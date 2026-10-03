package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"time"
)

func runIn(q queryRow, runID string) (StageRun, error) {
	var r StageRun
	var expiry int64
	var raw string
	e := q.QueryRow("SELECT id,task_id,role,attempt,generation,plan_revision,state,native_session_id,lease_owner,lease_until,startup_intent,launch_confirmed,target FROM stage_runs WHERE id=?", runID).Scan(&r.ID, &r.TaskID, &r.Role, &r.Attempt, &r.Generation, &r.PlanRevision, &r.State, &r.NativeSessionID, &r.Owner, &expiry, &r.StartupIntent, &r.LaunchConfirmed, &raw)
	if errors.Is(e, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if e != nil {
		return r, e
	}
	r.LeaseUntil = time.UnixMilli(expiry)
	if json.Unmarshal([]byte(raw), &r.Target) != nil {
		return r, ErrInvalid
	}
	return r, nil
}
func (s *Store) Run(runID string) (StageRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return StageRun{}, ErrClosed
	}
	return runIn(s.db, runID)
}

// CheckActive also durably fences an observed expiry. The owner is resolved
// from server state, never from a client-declared identity header.
func (s *Store) CheckActive(runID string, generation int64) (StageRun, error) {
	var result StageRun
	err := s.transaction(func(tx *sql.Tx) error {
		r, err := runIn(tx, runID)
		if err != nil {
			return err
		}
		result, err = s.fenced(tx, runID, generation, r.Owner)
		return err
	})
	return result, err
}
func (s *Store) StartIntent(in StartRequest) (StageRun, error) {
	if in.IdempotencyKey != "" || !opaque(in.Owner) || in.TTL <= 0 || in.TTL > time.Minute || in.ExpectedGeneration != nil && *in.ExpectedGeneration < 0 {
		return StageRun{}, ErrInvalid
	}
	var result StageRun
	e := s.transaction(func(tx *sql.Tx) error { return s.startIntentIn(tx, in, &result) })
	return result, e
}
func (s *Store) startIntentIn(tx *sql.Tx, in StartRequest, result *StageRun) error {
	t, e := taskIn(tx, in.TaskID)
	if e != nil {
		return e
	}
	if t.PlanRevision != in.PlanRevision || t.State != "ready" {
		return ErrConflict
	}
	if in.ExpectedGeneration != nil && *in.ExpectedGeneration != t.Generation {
		return ErrConflict
	}
	p, e := planIn(tx, t.ID, in.PlanRevision)
	if e != nil {
		return e
	}
	b, ok := p.Bindings[in.Role]
	if !ok {
		return ErrInvalid
	}
	raw, _ := json.Marshal(in.Target)
	admitted := false
	if b.Mode == stageplan.Locked && b.Target != nil {
		expected, _ := json.Marshal(b.Target)
		admitted = string(expected) == string(raw)
	} else if b.Mode == stageplan.Auto {
		for _, c := range b.Candidates {
			expected, _ := json.Marshal(c)
			if string(expected) == string(raw) {
				admitted = true
			}
		}
	}
	if !admitted {
		return ErrInvalid
	}
	var active int
	if e = tx.QueryRow("SELECT COUNT(*) FROM stage_runs WHERE task_id=? AND state IN ('starting','running','cancelling','unknown')", t.ID).Scan(&active); e != nil {
		return e
	}
	if active > 0 {
		return ErrConflict
	}
	var attempt int64
	if e = tx.QueryRow("SELECT COALESCE(MAX(attempt),0)+1 FROM stage_runs WHERE task_id=? AND role=?", t.ID, in.Role).Scan(&attempt); e != nil {
		return e
	}
	runID, e := id("run-")
	if e != nil {
		return e
	}
	gen := t.Generation + 1
	expiry := s.now().Add(in.TTL).UnixMilli()
	if _, e = tx.Exec("INSERT INTO stage_runs(id,task_id,role,attempt,generation,plan_revision,state,lease_owner,lease_until,startup_intent,target) VALUES(?,?,?,?,?,?,'starting',?,?,1,?)", runID, t.ID, in.Role, attempt, gen, in.PlanRevision, in.Owner, expiry, string(raw)); e != nil {
		return e
	}
	if _, e = tx.Exec("UPDATE tasks SET state='running',generation=? WHERE id=?", gen, t.ID); e != nil {
		return e
	}
	if e = event(tx, t.ID, "start_intent", runID, gen); e != nil {
		return e
	}
	*result, e = runIn(tx, runID)
	return e
}
func (s *Store) fenced(tx *sql.Tx, runID string, gen int64, owner string) (StageRun, error) {
	r, e := runIn(tx, runID)
	if e != nil {
		return r, e
	}
	t, e := taskIn(tx, r.TaskID)
	if e != nil {
		return r, e
	}
	if gen != r.Generation || gen != t.Generation || owner != r.Owner || (r.State != "starting" && r.State != "running" && r.State != "cancelling") {
		return r, ErrFenced
	}
	if !s.now().Before(r.LeaseUntil) {
		newGeneration := t.Generation + 1
		if _, e = tx.Exec("UPDATE tasks SET state='needs_review',generation=? WHERE id=?", newGeneration, r.TaskID); e != nil {
			return r, e
		}
		if _, e = tx.Exec("UPDATE stage_runs SET state='unknown',generation=?,lease_owner='',lease_until=0 WHERE id=?", newGeneration, runID); e != nil {
			return r, e
		}
		if e = event(tx, r.TaskID, "lease_expired_unknown", runID, newGeneration); e != nil {
			return r, e
		}
		return r, errExpiredFence
	}
	return r, nil
}
func (s *Store) ConfirmStarted(runID string, gen int64, owner, session string) error {
	if !opaque(session) {
		return ErrInvalid
	}
	return s.transaction(func(tx *sql.Tx) error {
		r, e := s.fenced(tx, runID, gen, owner)
		if e != nil {
			return e
		}
		if r.State != "starting" {
			return ErrFenced
		}
		if _, e = tx.Exec("UPDATE stage_runs SET state='running',native_session_id=?,launch_confirmed=1 WHERE id=?", session, runID); e != nil {
			return e
		}
		return event(tx, r.TaskID, "started", runID, gen)
	})
}
func (s *Store) RenewLease(runID string, gen int64, owner string, ttl time.Duration) error {
	if ttl <= 0 || ttl > time.Minute {
		return ErrInvalid
	}
	return s.transaction(func(tx *sql.Tx) error {
		if _, e := s.fenced(tx, runID, gen, owner); e != nil {
			return e
		}
		_, e := tx.Exec("UPDATE stage_runs SET lease_until=? WHERE id=?", s.now().Add(ttl).UnixMilli(), runID)
		return e
	})
}
func (s *Store) CancelIntentAtRevision(runID string, gen int64, owner string, revision int64) error {
	if revision < 1 {
		return ErrInvalid
	}
	return s.cancelIntent(runID, gen, owner, revision)
}
func (s *Store) CancelIntent(runID string, gen int64, owner string) error {
	return s.cancelIntent(runID, gen, owner, 0)
}
func (s *Store) cancelIntent(runID string, gen int64, owner string, revision int64) error {
	return s.transaction(func(tx *sql.Tx) error {
		r, e := s.fenced(tx, runID, gen, owner)
		if e != nil {
			return e
		}
		// The task revision may change without changing the active run's
		// frozen plan or generation. Check the HTTP precondition in this
		// transaction, including a repeated cancelling intent.
		if revision > 0 {
			t, e := taskIn(tx, r.TaskID)
			if e != nil {
				return e
			}
			if t.PlanRevision != revision {
				return ErrConflict
			}
		}
		if r.State == "cancelling" {
			return nil
		}
		if _, e = tx.Exec("UPDATE stage_runs SET state='cancelling' WHERE id=?", runID); e != nil {
			return e
		}
		return event(tx, r.TaskID, "cancel_intent", runID, gen)
	})
}
func (s *Store) Finish(runID string, gen int64, owner, outcome string) error {
	switch outcome {
	case "succeeded", "failed", "cancelled", "interrupted", "advisory_only":
	default:
		return ErrInvalid
	}
	return s.transaction(func(tx *sql.Tx) error {
		r, e := s.fenced(tx, runID, gen, owner)
		if e != nil {
			return e
		}
		if outcome == "succeeded" && r.State != "running" {
			return ErrFenced
		}
		if _, e = tx.Exec("UPDATE stage_runs SET state=?,lease_owner='',lease_until=0 WHERE id=?", outcome, runID); e != nil {
			return e
		}
		state := "ready"
		switch outcome {
		case "failed", "cancelled", "advisory_only":
			state = outcome
		case "interrupted":
			state = "needs_review"
		}
		task, e := taskIn(tx, r.TaskID)
		if e != nil {
			return e
		}
		// Protocol completion does not prove that the process stopped. A pause
		// remains pending until trusted reservation release; interrupted work
		// already requires reconciliation even when no handle was obtained.
		if task.State == "pausing" && state != "needs_review" {
			state = "pausing"
		}
		if _, e = tx.Exec("UPDATE tasks SET state=? WHERE id=?", state, r.TaskID); e != nil {
			return e
		}
		return event(tx, r.TaskID, "finished_"+outcome, runID, gen)
	})
}

// Every prior active intent becomes unknown at controller restart, even if its
// wall-clock lease remains valid. Database transactions do not prove child I/O.
func (s *Store) recover() error {
	return s.transaction(func(tx *sql.Tx) error {
		rows, e := tx.Query("SELECT id FROM stage_runs WHERE state IN ('starting','running','cancelling')")
		if e != nil {
			return e
		}
		var ids []string
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return e
			}
			ids = append(ids, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		for _, id := range ids {
			r, e := runIn(tx, id)
			if e != nil {
				return e
			}
			t, e := taskIn(tx, r.TaskID)
			if e != nil {
				return e
			}
			gen := t.Generation + 1
			if _, e = tx.Exec("UPDATE tasks SET state='needs_review',generation=? WHERE id=?", gen, r.TaskID); e != nil {
				return e
			}
			if _, e = tx.Exec("UPDATE stage_runs SET state='unknown',generation=?,lease_owner='',lease_until=0 WHERE id=?", gen, id); e != nil {
				return e
			}
			if e = event(tx, r.TaskID, "recovery_unknown", id, gen); e != nil {
				return e
			}
		}
		return nil
	})
}

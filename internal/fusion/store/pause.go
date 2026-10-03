package store

import (
	"database/sql"
	"errors"
)

// TaskVersion is the whole public task condition, not just its plan revision.
type TaskVersion struct {
	PlanRevision, Generation int64
	State                    string
}

// A pending Run still requires controller-owned cancellation and actual wait.
// This receipt never grants permission to invoke Native Resume or Start.
type TaskControlReceipt struct {
	Task    Task
	Run     *StageRun
	Changed bool
}

var ErrPauseReconcile = errors.New("task pause requires execution reconciliation")

func taskVersionIn(q queryRow, id string, expected TaskVersion) (Task, error) {
	if !opaque(id) || expected.PlanRevision < 1 || expected.Generation < 0 || expected.State == "" {
		return Task{}, ErrInvalid
	}
	t, e := taskIn(q, id)
	if e != nil {
		return t, e
	}
	if t.PlanRevision != expected.PlanRevision || t.Generation != expected.Generation || t.State != expected.State {
		return t, ErrConflict
	}
	return t, nil
}

func pendingTaskRunIn(q queryRow, id string) (*StageRun, error) {
	var runID string
	e := q.QueryRow("SELECT id FROM stage_runs WHERE task_id=? AND (state IN ('starting','running','cancelling','unknown') OR EXISTS (SELECT 1 FROM reservations WHERE run_id=stage_runs.id AND state='held')) ORDER BY generation DESC LIMIT 1", id).Scan(&runID)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	r, e := runIn(q, runID)
	return &r, e
}

// Protocol terminal state alone is insufficient. A legacy run without a
// reservation/stop proof also requires reconciliation, not inferred quiescence.
func taskQuiescentIn(q queryRow, id string) (bool, error) {
	var pending int
	e := q.QueryRow("SELECT COUNT(*) FROM stage_runs LEFT JOIN reservations ON reservations.run_id=stage_runs.id WHERE task_id=? AND (stage_runs.state NOT IN ('succeeded','failed','cancelled','interrupted','advisory_only') OR reservations.run_id IS NULL OR reservations.state!='released' OR reservations.stop_proof_hash='')", id).Scan(&pending)
	return pending == 0, e
}

// PauseTask atomically blocks new dispatch and new calls. Active runs keep
// their generation so the existing owner can finish; this method cannot stop
// an external process or release its reservation.
func (s *Store) PauseTask(id string, expected TaskVersion, owner string) (TaskControlReceipt, error) {
	var out TaskControlReceipt
	e := s.transaction(func(tx *sql.Tx) error {
		t, e := taskVersionIn(tx, id, expected)
		if e != nil {
			return e
		}
		out.Task = t
		switch t.State {
		case "paused":
			return nil
		case "needs_review":
			return ErrPauseReconcile
		case "ready", "running", "pausing":
		default:
			return ErrConflict
		}
		r, e := pendingTaskRunIn(tx, id)
		if e != nil {
			return e
		}
		if r == nil {
			if t.State != "ready" {
				return ErrPauseReconcile
			}
			quiet, e := taskQuiescentIn(tx, id)
			if e != nil {
				return e
			}
			if !quiet {
				return ErrPauseReconcile
			}
			if t.Generation == int64(1<<63-1) {
				return ErrInvalid
			}
			if _, e = tx.Exec("UPDATE tasks SET state='paused',generation=generation+1 WHERE id=?", id); e != nil {
				return e
			}
			if e = event(tx, id, "task_paused", "", t.Generation+1); e != nil {
				return e
			}
			out.Task, e = taskIn(tx, id)
			out.Changed = true
			return e
		}
		if r.Generation != t.Generation || r.State == "unknown" {
			return ErrPauseReconcile
		}
		if r.State == "starting" || r.State == "running" || r.State == "cancelling" {
			if !opaque(owner) {
				return ErrInvalid
			}
			if _, e = s.fenced(tx, r.ID, r.Generation, owner); e != nil {
				return e
			}
			if r.State != "cancelling" {
				if _, e = tx.Exec("UPDATE stage_runs SET state='cancelling' WHERE id=?", r.ID); e != nil {
					return e
				}
				if e = event(tx, id, "cancel_intent", r.ID, r.Generation); e != nil {
					return e
				}
			}
		}
		if t.State != "pausing" {
			if _, e = tx.Exec("UPDATE tasks SET state='pausing' WHERE id=?", id); e != nil {
				return e
			}
			if e = event(tx, id, "task_pause_requested", r.ID, r.Generation); e != nil {
				return e
			}
			out.Changed = true
		}
		out.Task, e = taskIn(tx, id)
		if e != nil {
			return e
		}
		current, e := runIn(tx, r.ID)
		out.Run = &current
		return e
	})
	if e != nil {
		return TaskControlReceipt{}, e
	}
	return out, nil
}

// ContinueTask unblocks a quiescent paused task. It does not start a stage,
// reset counters, adopt an unknown run or authorize replay of cancelled work.
func (s *Store) ContinueTask(id string, expected TaskVersion) (TaskControlReceipt, error) {
	var out TaskControlReceipt
	e := s.transaction(func(tx *sql.Tx) error {
		t, e := taskVersionIn(tx, id, expected)
		if e != nil {
			return e
		}
		out.Task = t
		if t.State == "ready" {
			return nil
		}
		if t.State == "pausing" || t.State == "needs_review" {
			return ErrPauseReconcile
		}
		if t.State != "paused" {
			return ErrConflict
		}
		quiet, e := taskQuiescentIn(tx, id)
		if e != nil {
			return e
		}
		if !quiet {
			return ErrPauseReconcile
		}
		if t.Generation == int64(1<<63-1) {
			return ErrInvalid
		}
		if _, e = tx.Exec("UPDATE tasks SET state='ready',generation=generation+1 WHERE id=?", id); e != nil {
			return e
		}
		if e = event(tx, id, "task_continued", "", t.Generation+1); e != nil {
			return e
		}
		out.Task, e = taskIn(tx, id)
		out.Changed = true
		return e
	})
	if e != nil {
		return TaskControlReceipt{}, e
	}
	return out, nil
}

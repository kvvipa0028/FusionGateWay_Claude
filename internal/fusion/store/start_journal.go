package store

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

// StartDraft freezes trusted metadata before an explicit UI start is sent.
// It contains no admission evidence and cannot authorize a Native launch.
type StartDraft struct {
	Key      string             `json:"key"`
	Identity StartIdentity      `json:"identity"`
	Task     Task               `json:"task"`
	Plan     stageplan.Snapshot `json:"plan"`
}
type StartJournal struct {
	Draft StartDraft `json:"draft"`
	State string     `json:"state"`
	RunID string     `json:"run_id"`
}
type StartJournalReceipt struct {
	Journal StartJournal
	Created bool
}

func canonicalStartDraft(in StartDraft) (StartDraft, []byte, error) {
	i := in.Identity
	if !opaque(in.Key) || !validStartIdentity(i) || i.Restore != nil || in.Task.ID != i.TaskID || !opaque(in.Task.ProjectID) || in.Task.State != "ready" || in.Task.PlanRevision != i.PlanRevision || in.Task.Generation != i.Generation || in.Plan.Revision != i.PlanRevision || stageplan.VerifySnapshot(in.Plan) != nil {
		return StartDraft{}, nil, ErrInvalid
	}
	if _, ok := in.Plan.Bindings[i.Role]; !ok {
		return StartDraft{}, nil, ErrInvalid
	}
	raw, e := json.Marshal(in)
	if e != nil {
		return StartDraft{}, nil, ErrInvalid
	}
	var frozen StartDraft
	if json.Unmarshal(raw, &frozen) != nil {
		return StartDraft{}, nil, ErrInvalid
	}
	return frozen, raw, nil
}

// loadStartJournal checks the immutable draft and current trusted history.
// It deliberately does not use lookupStartIn: that lookup applies this guard.
func loadStartJournal(q queryRow, key string) (StartJournal, error) {
	var j StartJournal
	var project, taskID, payload, raw, digest string
	var run sql.NullString
	e := q.QueryRow("SELECT project_id,task_id,identity_hash,draft_json,draft_hash,state,run_id FROM start_journals WHERE key=?", key).Scan(&project, &taskID, &payload, &raw, &digest, &j.State, &run)
	if errors.Is(e, sql.ErrNoRows) {
		return j, ErrNotFound
	}
	if e != nil {
		return j, e
	}
	if json.Unmarshal([]byte(raw), &j.Draft) != nil || hash([]byte(raw)) != digest {
		return StartJournal{}, ErrConflict
	}
	d, canonical, e := canonicalStartDraft(j.Draft)
	if e != nil || hash(canonical) != digest || d.Key != key || d.Task.ID != taskID || d.Task.ProjectID != project || startHash(d.Identity) != payload {
		return StartJournal{}, ErrConflict
	}
	current, e := taskIn(q, taskID)
	if e != nil {
		if errors.Is(e, ErrNotFound) {
			return StartJournal{}, ErrConflict
		}
		return StartJournal{}, e
	}
	plan, e := planIn(q, taskID, d.Identity.PlanRevision)
	if e != nil || plan.Hash != d.Plan.Hash || current.ProjectID != project || current.Goal != d.Task.Goal || current.PlanRevision < d.Task.PlanRevision || current.Generation < d.Task.Generation {
		return StartJournal{}, ErrConflict
	}
	j.Draft = d
	j.RunID = run.String
	switch j.State {
	case "prepared", "abandoned":
		if run.Valid {
			return StartJournal{}, ErrConflict
		}
		var mappings int
		if e := q.QueryRow("SELECT COUNT(*) FROM start_requests WHERE key=?", key).Scan(&mappings); e != nil {
			return StartJournal{}, e
		}
		if mappings != 0 {
			return StartJournal{}, ErrConflict
		}
	case "committed", "acknowledged":
		if !run.Valid || !opaque(j.RunID) {
			return StartJournal{}, ErrConflict
		}
		var actualHash, actualRun string
		if e := q.QueryRow("SELECT payload_hash,run_id FROM start_requests WHERE key=?", key).Scan(&actualHash, &actualRun); e != nil || actualHash != payload || actualRun != j.RunID {
			return StartJournal{}, ErrConflict
		}
	default:
		return StartJournal{}, ErrConflict
	}
	return j, nil
}

func checkStartJournal(q queryRow, key string, in StartIdentity) error {
	j, e := loadStartJournal(q, key)
	if errors.Is(e, ErrNotFound) {
		return nil
	}
	if e != nil {
		return e
	}
	if startHash(in) != startHash(j.Draft.Identity) || j.State == "abandoned" {
		return ErrConflict
	}
	return nil
}

// PrepareStart only saves an original request. Repeats return the same draft,
// including terminal history; a new request must match a live ready Task.
func (s *Store) PrepareStart(key string, in StartIdentity) (StartJournalReceipt, error) {
	if !opaque(key) || !validStartIdentity(in) || in.Restore != nil {
		return StartJournalReceipt{}, ErrInvalid
	}
	var result StartJournalReceipt
	e := s.transaction(func(tx *sql.Tx) error {
		old, e := loadStartJournal(tx, key)
		if e == nil {
			if startHash(in) != startHash(old.Draft.Identity) {
				return ErrConflict
			}
			result.Journal = old
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		task, e := taskIn(tx, in.TaskID)
		if e != nil {
			return e
		}
		if task.State != "ready" || task.PlanRevision != in.PlanRevision || task.Generation != in.Generation {
			return ErrConflict
		}
		plan, e := planIn(tx, in.TaskID, in.PlanRevision)
		if e != nil {
			return e
		}
		d, raw, e := canonicalStartDraft(StartDraft{Key: key, Identity: in, Task: task, Plan: plan})
		if e != nil {
			return e
		}
		var pending string
		e = tx.QueryRow("SELECT key FROM start_journals WHERE project_id=? AND state IN ('prepared','committed')", task.ProjectID).Scan(&pending)
		if e == nil {
			return ErrConflict
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		// Never infer the original UI draft from an older unjournalled run.
		if _, e = lookupStartIn(tx, key, in); !errors.Is(e, ErrNotFound) {
			if e == nil {
				return ErrConflict
			}
			return e
		}
		if _, e = tx.Exec("INSERT INTO start_journals VALUES(?,?,?,?,?,?,?,NULL)", key, task.ProjectID, task.ID, startHash(in), string(raw), hash(raw), "prepared"); e != nil {
			return e
		}
		result.Journal = StartJournal{Draft: d, State: "prepared"}
		result.Created = true
		return nil
	})
	return result, e
}

// ReadStartJournal returns validated original metadata for trusted consumers.
// The key selects history; callers must enforce project and original conditions.
func (s *Store) ReadStartJournal(key string) (StartJournal, error) {
	if !opaque(key) {
		return StartJournal{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return StartJournal{}, ErrClosed
	}
	return loadStartJournal(s.db, key)
}

func (s *Store) LookupStartJournal(key string, in StartIdentity) (StartJournal, error) {
	if !opaque(key) || !validStartIdentity(in) || in.Restore != nil {
		return StartJournal{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return StartJournal{}, ErrClosed
	}
	j, e := loadStartJournal(s.db, key)
	if e != nil {
		return j, e
	}
	if startHash(in) != startHash(j.Draft.Identity) {
		return StartJournal{}, ErrConflict
	}
	return j, nil
}

func (s *Store) PendingStart(project string) (StartJournal, error) {
	if !opaque(project) {
		return StartJournal{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return StartJournal{}, ErrClosed
	}
	var key string
	e := s.db.QueryRow("SELECT key FROM start_journals WHERE project_id=? AND state IN ('prepared','committed')", project).Scan(&key)
	if errors.Is(e, sql.ErrNoRows) {
		return StartJournal{}, ErrNotFound
	}
	if e != nil {
		return StartJournal{}, e
	}
	return loadStartJournal(s.db, key)
}

// ResolveStart acknowledges the exact committed run or seals a still-unwritten
// request. Neither operation cancels a run, changes a Task or refunds budget.
func (s *Store) ResolveStart(key string, in StartIdentity, action, runID string) (StartJournal, error) {
	if !opaque(key) || !validStartIdentity(in) || in.Restore != nil || action != "acknowledge" && action != "abandon" || action == "abandon" && runID != "" || action == "acknowledge" && !opaque(runID) {
		return StartJournal{}, ErrInvalid
	}
	var result StartJournal
	e := s.transaction(func(tx *sql.Tx) error {
		j, e := loadStartJournal(tx, key)
		if e != nil {
			return e
		}
		if startHash(in) != startHash(j.Draft.Identity) {
			return ErrConflict
		}
		target := "abandoned"
		if action == "acknowledge" {
			target = "acknowledged"
			if j.State != "committed" && j.State != target || j.RunID != runID {
				return ErrConflict
			}
			r, e := lookupStartIn(tx, key, in)
			if e != nil || r.ID != runID {
				return ErrConflict
			}
		} else if j.State != "prepared" && j.State != target {
			return ErrConflict
		}
		if j.State != target {
			if _, e = tx.Exec("UPDATE start_journals SET state=? WHERE key=?", target, key); e != nil {
				return e
			}
		}
		result, e = loadStartJournal(tx, key)
		return e
	})
	return result, e
}

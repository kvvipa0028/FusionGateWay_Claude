package store

import (
	"database/sql"
	"errors"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

// A durable intent consumes an attempt, even if launch or reconciliation fails.
// Plan revisions and model-call/rework budgets cannot reset this task/role cap.
const maxStageAttempts = 2

var ErrStageLimit = errors.New("stage attempt limit reached")
var errStageLimitFence = errors.New("commit exhausted stage fence")

func stageAttemptIn(q queryRow, taskID string, role stageplan.Role) (int64, error) {
	var attempt int64
	e := q.QueryRow("SELECT COALESCE(MAX(attempt),0) FROM stage_runs WHERE task_id=? AND role=?", taskID, role).Scan(&attempt)
	return attempt, e
}

// Call only after current task/role/target validation. A held reservation still
// requires process-stop reconciliation and must never be released by this gate.
func stageLimitIn(tx *sql.Tx, t Task, role stageplan.Role) error {
	attempt, e := stageAttemptIn(tx, t.ID, role)
	if e != nil || attempt < maxStageAttempts {
		return e
	}
	var held int
	if e = tx.QueryRow("SELECT COUNT(*) FROM reservations JOIN stage_runs ON stage_runs.id=reservations.run_id WHERE task_id=? AND reservations.state='held'", t.ID).Scan(&held); e != nil {
		return e
	}
	if held != 0 {
		return ErrConflict
	}
	if t.Generation == int64(1<<63-1) {
		return ErrInvalid
	}
	var last string
	if e = tx.QueryRow("SELECT id FROM stage_runs WHERE task_id=? AND role=? ORDER BY generation DESC LIMIT 1", t.ID, role).Scan(&last); e != nil {
		return e
	}
	if _, e = tx.Exec("UPDATE tasks SET state='needs_review',generation=generation+1 WHERE id=?", t.ID); e != nil {
		return e
	}
	if e = event(tx, t.ID, "stage_attempt_limit", last, t.Generation+1); e != nil {
		return e
	}
	return errStageLimitFence
}

// ValidateStageStartAuthorized applies the workflow and stage-attempt gates
// before target selection/Runtime preparation. current is a local nonblocking
// authority check and must not reenter Store. Revocation rolls back even the
// needs_review fence; stale requests cannot mutate a newer task version.
func (s *Store) ValidateStageStartAuthorized(in StartIdentity, current func() bool) error {
	if !validStartIdentity(in) {
		return ErrInvalid
	}
	exhausted := false
	e := s.workflowTransaction(current, func(tx *sql.Tx) error {
		t, e := taskVersionIn(tx, in.TaskID, TaskVersion{PlanRevision: in.PlanRevision, Generation: in.Generation, State: "ready"})
		if e != nil {
			return e
		}
		p, e := planIn(tx, t.ID, t.PlanRevision)
		if e != nil {
			return e
		}
		if _, ok := p.Bindings[in.Role]; !ok {
			return ErrInvalid
		}
		if e = workflowStartIn(tx, t, in.Role); e != nil {
			return e
		}
		e = stageLimitIn(tx, t, in.Role)
		if e == errStageLimitFence {
			exhausted = true
			return nil // workflowTransaction still rechecks authority before commit.
		}
		return e
	})
	if e == nil && exhausted {
		return ErrStageLimit
	}
	return e
}

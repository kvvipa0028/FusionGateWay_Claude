package store

import (
	"database/sql"
	"errors"
)

var ErrCancelReconcile = errors.New("task cancellation requires execution reconciliation")

// CancelTask stops task scheduling. Active runs retain their generation for
// their existing owner to finish; cancellation is not proof of process exit.
func (s *Store) CancelTask(id string, expected TaskVersion, owner string) (TaskControlReceipt, error) {
	var out TaskControlReceipt
	e := s.transaction(func(tx *sql.Tx) error {
		t, e := taskVersionIn(tx, id, expected)
		if e != nil {
			return e
		}
		switch t.State {
		case "needs_review":
			return ErrCancelReconcile
		case "ready", "paused", "running", "pausing", "cancelling", "cancelled", "failed", "advisory_only":
		default:
			return ErrConflict
		}
		r, e := pendingTaskRunIn(tx, id)
		if e != nil {
			return e
		}
		if r == nil {
			quiet, e := taskQuiescentIn(tx, id)
			if e != nil {
				return e
			}
			if !quiet || t.State == "running" || t.State == "pausing" || t.State == "cancelling" {
				return ErrCancelReconcile
			}
			out.Task = t
			if t.State == "cancelled" {
				return nil
			}
			if t.Generation == int64(1<<63-1) {
				return ErrInvalid
			}
			if _, e = tx.Exec("UPDATE tasks SET state='cancelled',generation=generation+1 WHERE id=?", id); e != nil {
				return e
			}
			if e = event(tx, id, "task_cancelled", "", t.Generation+1); e != nil {
				return e
			}
			out.Task, e = taskIn(tx, id)
			out.Changed = true
			return e
		}
		if r.Generation != t.Generation || r.State == "unknown" {
			return ErrCancelReconcile
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
		if t.State != "cancelling" {
			if _, e = tx.Exec("UPDATE tasks SET state='cancelling' WHERE id=?", id); e != nil {
				return e
			}
			if e = event(tx, id, "task_cancel_requested", r.ID, r.Generation); e != nil {
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

package store

import (
	"database/sql"
	"encoding/json"
	"reflect"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

// ValidateRevision has no persistent effects. RevisePlan repeats these checks
// in its write transaction to close a stage-start race after preview.
func (s *Store) ValidateRevision(taskID string, ifMatch int64, p stageplan.Snapshot) error {
	return s.transaction(func(tx *sql.Tx) error {
		_, e := revisionIn(tx, taskID, ifMatch, p)
		return e
	})
}

func revisionIn(tx *sql.Tx, taskID string, ifMatch int64, p stageplan.Snapshot) (Task, error) {
	if ifMatch < 1 || ifMatch == int64(1<<63-1) || p.Revision != ifMatch+1 || stageplan.VerifySnapshot(p) != nil {
		return Task{}, ErrInvalid
	}
	t, e := taskIn(tx, taskID)
	if e != nil {
		return Task{}, e
	}
	if t.PlanRevision != ifMatch || t.State == "completed" || t.State == "cancelled" {
		return Task{}, ErrConflict
	}
	old, e := planIn(tx, taskID, ifMatch)
	if e != nil {
		return Task{}, e
	}
	if old.SchemaVersion != p.SchemaVersion || !reflect.DeepEqual(old.RequiredRoles, p.RequiredRoles) || len(old.Bindings) != len(p.Bindings) {
		return Task{}, ErrConflict
	}
	for r := range old.Bindings {
		if _, ok := p.Bindings[r]; !ok {
			return Task{}, ErrConflict
		}
	}
	rows, e := tx.Query("SELECT DISTINCT role FROM stage_runs WHERE task_id=?", taskID)
	if e != nil {
		return Task{}, e
	}
	defer rows.Close()
	for rows.Next() {
		var r stageplan.Role
		if e = rows.Scan(&r); e != nil {
			return Task{}, e
		}
		a, e := json.Marshal(old.Bindings[r])
		if e != nil {
			return Task{}, ErrInvalid
		}
		b, e := json.Marshal(p.Bindings[r])
		if e != nil || string(a) != string(b) {
			return Task{}, ErrConflict
		}
	}
	if e = rows.Err(); e != nil {
		return Task{}, e
	}
	return t, nil
}

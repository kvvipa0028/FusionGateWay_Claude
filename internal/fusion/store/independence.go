package store

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

var ErrIndependence = errors.New("project model independence conflict")

func independenceTargetIn(q workflowQuery, t Task, p stageplan.Snapshot, role stageplan.Role, target stageplan.ExecutionTarget) error {
	for _, pair := range p.Independence {
		other := pair.Second
		if pair.Second == role {
			other = pair.First
		} else if pair.First != role {
			continue
		}
		binding, exists := p.Bindings[other]
		if !exists {
			continue
		}
		var targets []stageplan.ExecutionTarget
		if binding.Mode == stageplan.Locked && binding.Target != nil {
			targets = append(targets, *binding.Target)
		} else if binding.Mode == stageplan.Auto {
			targets = binding.Candidates
		}
		possible := false
		for _, candidate := range targets {
			if candidate.ResolvedModel != "" && candidate.ResolvedModel != target.ResolvedModel {
				possible = true
			}
		}
		if !possible {
			return ErrIndependence
		}
		rows, e := q.Query("SELECT target FROM stage_runs WHERE task_id=? AND role=?", t.ID, other)
		if e != nil {
			return e
		}
		for rows.Next() {
			var raw string
			var prior stageplan.ExecutionTarget
			if e = rows.Scan(&raw); e != nil {
				rows.Close()
				return e
			}
			if json.Unmarshal([]byte(raw), &prior) != nil || prior.ResolvedModel == "" {
				rows.Close()
				return ErrInvalid
			}
			if prior.ResolvedModel == target.ResolvedModel {
				rows.Close()
				return ErrIndependence
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
	}
	return nil
}

// ValidateStageTargetAuthorized never grants a model or records an intent.
// It rejects the concrete frozen selection before Runtime/credential preparation.
func (s *Store) ValidateStageTargetAuthorized(in StartIdentity, target stageplan.ExecutionTarget, current func() bool) error {
	if !validStartIdentity(in) {
		return ErrInvalid
	}
	return s.workflowTransaction(current, func(tx *sql.Tx) error {
		t, e := taskVersionIn(tx, in.TaskID, TaskVersion{PlanRevision: in.PlanRevision, Generation: in.Generation, State: "ready"})
		if e != nil {
			return e
		}
		p, e := planIn(tx, t.ID, t.PlanRevision)
		if e != nil {
			return e
		}
		b, ok := p.Bindings[in.Role]
		if !ok {
			return ErrInvalid
		}
		raw, _ := json.Marshal(target)
		allowed := false
		if b.Mode == stageplan.Locked && b.Target != nil {
			expected, _ := json.Marshal(b.Target)
			allowed = string(raw) == string(expected)
		}
		if b.Mode == stageplan.Auto {
			for _, candidate := range b.Candidates {
				expected, _ := json.Marshal(candidate)
				if string(raw) == string(expected) {
					allowed = true
				}
			}
		}
		if !allowed {
			return ErrInvalid
		}
		return independenceTargetIn(tx, t, p, in.Role, target)
	})
}

package store

import (
	"errors"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

// StageInput is assembled from the private Store, never an HTTP input or grant.
type StageInput struct {
	Workflow *WorkflowView
	Parent   *ArtifactRecord
}

func (s *Store) StageArtifactInput(taskID string, expected TaskVersion, role stageplan.Role) (StageInput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return StageInput{}, ErrClosed
	}
	if expected.State != "ready" {
		return StageInput{}, ErrConflict
	}
	t, err := taskVersionIn(s.db, taskID, expected)
	if err != nil {
		return StageInput{}, err
	}
	p, err := planIn(s.db, t.ID, t.PlanRevision)
	if err != nil {
		return StageInput{}, err
	}
	if _, ok := p.Bindings[role]; !ok {
		return StageInput{}, ErrConflict
	}
	v, err := workflowIn(s.db, t)
	if errors.Is(err, ErrNotFound) && len(p.RequiredRoles) == 1 && p.RequiredRoles[0] == role {
		return StageInput{}, nil
	}
	if err != nil || v.Blocker != "" || v.Next != role {
		return StageInput{}, ErrWorkflowGate
	}
	out := StageInput{Workflow: &v}
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM stage_runs WHERE task_id=?", t.ID).Scan(&count); err != nil {
		return StageInput{}, err
	}
	if count == 0 {
		return out, nil
	}
	var id string
	if err := s.db.QueryRow("SELECT id FROM stage_runs WHERE task_id=? AND generation<=? ORDER BY generation DESC LIMIT 1", t.ID, t.Generation).Scan(&id); err != nil {
		return StageInput{}, ErrWorkflowGate
	}
	a, err := artifactReleasedIn(s.db, id)
	if err != nil {
		return StageInput{}, err
	}
	out.Parent = &a
	return out, nil
}

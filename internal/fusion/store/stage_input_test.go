package store

import (
	"errors"
	"testing"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

func TestStageArtifactInputUsesExactCurrentWorkflowAndReleasedReceipt(t *testing.T) {
	s, _ := openFixture(t)
	task := workflowTask(t, s, "change")
	v := TaskVersion{PlanRevision: task.PlanRevision, Generation: task.Generation, State: task.State}
	in, err := s.StageArtifactInput(task.ID, v, stageplan.Design)
	if err != nil || in.Workflow == nil || in.Parent != nil || in.Workflow.Next != stageplan.Design {
		t.Fatal("first stage input", err)
	}
	if _, err := s.StageArtifactInput(task.ID, v, stageplan.Implementation); !errors.Is(err, ErrWorkflowGate) {
		t.Fatal("unapproved write input", err)
	}
	v.Generation++
	if _, err := s.StageArtifactInput(task.ID, v, stageplan.Design); !errors.Is(err, ErrConflict) {
		t.Fatal("stale task input", err)
	}
	// Existing workflow metadata alone is insufficient to prepare later code.
	s2, _ := openFixture(t)
	task, view := completedWorkflowDesign(t, s2)
	v = TaskVersion{PlanRevision: task.PlanRevision, Generation: task.Generation, State: task.State}
	if _, err := s2.ApproveWorkflowDesign(task.ID, v, view.Design.Snapshot.Hash, view.Design.Snapshot.AcceptanceHash); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.StageArtifactInput(task.ID, v, stageplan.Implementation); !errors.Is(err, ErrNotFound) {
		t.Fatal("missing real artifact accepted", err)
	}
}

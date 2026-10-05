package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/fusion/handoff"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/workflow"
	"github.com/yetone/magpie/internal/fusion/workspace"
)

func publishWorkflowArtifact(t *testing.T, s *Store, task Task, run StageRun, copy workspace.Snapshot, root, name string) (handoff.Bundle, ArtifactRecord) {
	t.Helper()
	if err := s.ConfirmStarted(run.ID, run.Generation, run.Owner, "artifact-workflow-session"); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(run.ID, run.Generation, run.Owner, "succeeded"); err != nil {
		t.Fatal(err)
	}
	a, err := workspace.Freeze(copy, root, name)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Plan(task.ID, run.PlanRevision)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := json.Marshal(run.Target)
	b := handoff.Binding{TaskID: task.ID, PlanRevision: run.PlanRevision, PlanHash: p.Hash, RunID: run.ID, Generation: run.Generation, Role: run.Role, TargetHash: hash(target)}
	h, err := handoff.Publish(a, b, task.Goal, handoff.Evidence{OutputHash: hash([]byte(name)), StopProofHash: hash([]byte("stop-" + run.ID))})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := h.Reference(root)
	if err != nil {
		t.Fatal(err)
	}
	return h, ArtifactRecord{Reference: ref, InputTreeHash: ref.BaseTreeHash}
}

func TestArtifactWorkflowRequiresExactReleasedParentAndInheritedCode(t *testing.T) {
	s, dbroot := openFixture(t)
	task := workflowTask(t, s, "change")
	source, root := t.TempDir(), t.TempDir()
	source, _ = filepath.EvalSymlinks(source)
	root, _ = filepath.EvalSymlinks(root)
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "code"), []byte("base"), 0600); err != nil {
		t.Fatal(err)
	}
	producer, err := workspace.Copy(source, root, "design")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(producer.Path, "decision"), []byte("design output"), 0600); err != nil {
		t.Fatal(err)
	}
	in, req := workflowStart(t, s, task, stageplan.Design, "artifact-design")
	started, err := s.StartReservedOnce(in, req)
	if err != nil {
		t.Fatal(err)
	}
	parent, record := publishWorkflowArtifact(t, s, task, started.Run, producer, root, "design-artifact")
	if err := s.RecordArtifact(record); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Artifact(started.Run.ID); !errors.Is(err, ErrArtifactPending) {
		t.Fatal("held parent became available", err)
	}
	if err := s.ReleaseReserved(started.Run.ID, started.Run.Generation, record.Reference.StopProofHash, true); err != nil {
		t.Fatal(err)
	}
	task, err = s.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	version := TaskVersion{PlanRevision: task.PlanRevision, Generation: task.Generation, State: task.State}
	design, err := s.SaveWorkflowDesign(task.ID, version, started.Run.ID, workflow.DesignDocument{Goal: task.Goal, Scope: []string{"code"}, Constraints: []string{"preserve source"}, Interfaces: []string{"typed API"}, Acceptance: []string{"real tests"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApproveWorkflowDesign(task.ID, version, design.Design.Snapshot.Hash, design.Design.Snapshot.AcceptanceHash); err != nil {
		t.Fatal(err)
	}
	in, req = workflowStart(t, s, task, stageplan.Implementation, "artifact-implementation")
	implementation, err := s.StartReservedOnce(in, req)
	if err != nil {
		t.Fatal(err)
	}
	childCopy, err := parent.Copy(record.Reference.Binding, root, "implementation")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(childCopy.Path, "code"), []byte("implemented code"), 0600); err != nil {
		t.Fatal(err)
	}
	_, child := publishWorkflowArtifact(t, s, task, implementation.Run, childCopy, root, "implementation-artifact")
	child.ParentRunID, child.InputTreeHash = started.Run.ID, record.Reference.TreeHash
	for _, mode := range []string{"missing_parent", "wrong_parent", "wrong_input", "fresh_original_copy"} {
		t.Run(mode, func(t *testing.T) {
			bad := child
			switch mode {
			case "missing_parent":
				bad.ParentRunID = ""
			case "wrong_parent":
				bad.ParentRunID = "foreign-run"
			case "wrong_input":
				bad.InputTreeHash = record.Reference.BaseTreeHash
			case "fresh_original_copy":
				fresh, err := workspace.Copy(source, root, "fresh-original")
				if err != nil {
					t.Fatal(err)
				}
				a, err := workspace.Freeze(fresh, root, "fresh-artifact")
				if err != nil {
					t.Fatal(err)
				}
				h, err := handoff.Publish(a, child.Reference.Binding, task.Goal, handoff.Evidence{OutputHash: hash([]byte("fresh")), StopProofHash: child.Reference.StopProofHash})
				if err != nil {
					t.Fatal(err)
				}
				bad.Reference, err = h.Reference(root)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := s.RecordArtifact(bad); !errors.Is(err, ErrConflict) {
				t.Fatal("incorrect parent accepted", err)
			}
		})
	}
	if err := s.RecordArtifact(child); err != nil {
		t.Fatal("exact parent rejected", err)
	}
	if err := s.ReleaseReserved(implementation.Run.ID, implementation.Run.Generation, child.Reference.StopProofHash, true); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dbroot)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	receipt, err := reopened.Artifact(implementation.Run.ID)
	if err != nil || receipt.ParentRunID != started.Run.ID {
		t.Fatal("parent lost across restart", err)
	}
	consumer, err := handoff.Restore(receipt.Reference, source, root)
	if err != nil {
		t.Fatal(err)
	}
	next, err := consumer.Copy(receipt.Reference.Binding, root, "testing")
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"code": "implemented code", "decision": "design output"} {
		got, err := os.ReadFile(filepath.Join(next.Path, name))
		if err != nil || string(got) != want {
			t.Fatal("ancestor code lost", name, err)
		}
	}
}

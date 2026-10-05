//go:build darwin

package store

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/evidence"
	"github.com/yetone/magpie/internal/fusion/handoff"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/workflow"
	"github.com/yetone/magpie/internal/fusion/workspace"
)

var reviewCapture = flag.String("fusion-review-capture", "", "capture synthetic released Native review receipt for offline schema checks")

func reviewedFixture(t *testing.T, s *Store) (ArtifactRecord, evidence.Spec, string, string) {
	t.Helper()
	task := workflowTask(t, s, "change")
	source, _ := filepath.EvalSymlinks(t.TempDir())
	root, _ := filepath.EvalSymlinks(t.TempDir())
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "tests.txt"), []byte("actual test input"), 0600); err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	exe, _ = filepath.EvalSymlinks(exe)
	bytes, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	spec := evidence.Spec{Tool: evidence.Tool{Executable: exe, SHA256: hash(bytes), Version: "stored-verifier 1", VersionArgs: []string{"-test.run=^TestStoredVerifierWorker$", "--", "version"}}, Args: []string{"-test.run=^TestStoredVerifierWorker$", "--", "pass"}, SuitePaths: []string{"tests.txt"}, Rules: evidence.Rules{MinTests: 1}, Timeout: 2 * time.Second}
	var parent handoff.Bundle
	var previous ArtifactRecord
	for _, role := range []stageplan.Role{stageplan.Design, stageplan.Implementation, stageplan.Testing, stageplan.Review} {
		task, err = s.Task(task.ID)
		if err != nil {
			t.Fatal(err)
		}
		in, req := workflowStart(t, s, task, role, "review-"+string(role))
		started, err := s.StartReservedOnce(in, req)
		if err != nil {
			t.Fatal(err)
		}
		var copy workspace.Snapshot
		if role == stageplan.Design {
			copy, err = workspace.Copy(source, root, string(role))
		} else {
			copy, err = parent.Copy(previous.Reference.Binding, root, string(role))
		}
		if err != nil {
			t.Fatal(err)
		}
		var record ArtifactRecord
		if role == stageplan.Testing {
			if err := s.ConfirmStarted(started.Run.ID, started.Run.Generation, started.Run.Owner, "synthetic-stopped-testing"); err != nil {
				t.Fatal(err)
			}
			if err := s.Finish(started.Run.ID, started.Run.Generation, started.Run.Owner, "succeeded"); err != nil {
				t.Fatal(err)
			}
			frozen, err := workspace.Freeze(copy, root, "testing-artifact")
			if err != nil {
				t.Fatal(err)
			}
			result, err := evidence.Run(context.Background(), frozen, root, spec)
			if err != nil {
				t.Fatal(err)
			}
			plan, _ := s.Plan(task.ID, task.PlanRevision)
			target, _ := json.Marshal(started.Run.Target)
			b := handoff.Binding{TaskID: task.ID, PlanRevision: task.PlanRevision, PlanHash: plan.Hash, RunID: started.Run.ID, Generation: started.Run.Generation, Role: role, TargetHash: hash(target)}
			parent, err = handoff.PublishVerified(frozen, b, task.Goal, handoff.Evidence{OutputHash: hash([]byte("synthetic model text")), StopProofHash: hash([]byte("stop-" + started.Run.ID))}, result, spec)
			if err != nil {
				t.Fatal(err)
			}
			record.Reference, err = parent.Reference(root)
			if err != nil {
				t.Fatal(err)
			}
			record.ParentRunID, record.InputTreeHash = previous.Reference.Binding.RunID, previous.Reference.TreeHash
			if err := s.RecordVerifiedArtifactAuthorized(record, result, frozen, spec, func() bool { return true }); err != nil {
				t.Fatal(err)
			}
		} else {
			parent, record = publishWorkflowArtifact(t, s, task, started.Run, copy, root, string(role)+"-artifact")
			if role != stageplan.Design {
				record.ParentRunID, record.InputTreeHash = previous.Reference.Binding.RunID, previous.Reference.TreeHash
			}
			if role == stageplan.Review {
				return record, spec, source, root
			}
			if err := s.RecordArtifact(record); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.ReleaseReserved(started.Run.ID, started.Run.Generation, record.Reference.StopProofHash, true); err != nil {
			t.Fatal(err)
		}
		if role == stageplan.Design {
			task, _ = s.Task(task.ID)
			v := TaskVersion{task.PlanRevision, task.Generation, task.State}
			d, err := s.SaveWorkflowDesign(task.ID, v, started.Run.ID, workflow.DesignDocument{Goal: task.Goal, Scope: []string{"tests.txt"}, Constraints: []string{"readonly review"}, Interfaces: []string{"typed opinions"}, Acceptance: []string{"real tests"}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.ApproveWorkflowDesign(task.ID, v, d.Design.Snapshot.Hash, d.Design.Snapshot.AcceptanceHash); err != nil {
				t.Fatal(err)
			}
		}
		previous = record
	}
	t.Fatal("missing review run")
	return ArtifactRecord{}, spec, source, root
}

func TestReviewReceiptKeepsHardModelAndHumanDecisionsSeparateAcrossRestart(t *testing.T) {
	s, dbroot := openFixture(t)
	a, spec, source, root := reviewedFixture(t, s)
	raw := `{"version":1,"verdict":"approve","findings":[]}`
	if err := s.RecordReviewedArtifactAuthorized(a, raw, func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ReviewedArtifact(a.Reference.Binding.RunID, source, root, spec); !errors.Is(err, ErrArtifactPending) {
		t.Fatal("held review disclosed decision", err)
	}
	if err := s.ReleaseReserved(a.Reference.Binding.RunID, a.Reference.Binding.Generation, a.Reference.StopProofHash, true); err != nil {
		t.Fatal(err)
	}
	got, hard, err := s.ReviewedArtifact(a.Reference.Binding.RunID, source, root, spec)
	if err != nil || hard.Status != evidence.Passed || got.Review == nil || !got.Review.Valid || got.Review.Document.Verdict != "approve" {
		t.Fatal("separate opinion and actual evidence", err, hard)
	}
	task, _ := s.Task(a.Reference.Binding.TaskID)
	if task.State != "ready" {
		t.Fatal("model opinion became human acceptance", task.State)
	}
	if *reviewCapture != "" {
		view, err := s.Workflow(task.ID)
		if err != nil {
			t.Fatal(err)
		}
		capture, err := json.MarshalIndent(map[string]any{"artifact": got, "hard_verdict": hard, "workflow": view}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(*reviewCapture, append(capture, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RecordArtifact(got); !errors.Is(err, ErrInvalid) {
		t.Fatal("public self-declared opinion accepted", err)
	}
	if err := s.RecordReviewedArtifactAuthorized(a, raw, func() bool { return false }); err == nil {
		t.Fatal("revoked producer registered review")
	}
	if err := s.RecordReviewedArtifactAuthorized(a, raw, func() bool { return true }); err != nil {
		t.Fatal("exact retry lost idempotence", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dbroot)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, v, err := s.ReviewedArtifact(a.Reference.Binding.RunID, source, root, spec); err != nil || v.Status != evidence.Passed {
		t.Fatal("restart lost receipt", err, v)
	}
	spec.Rules.MinTests++
	if _, v, err := s.ReviewedArtifact(a.Reference.Binding.RunID, source, root, spec); err != nil || v.Status != evidence.Superseded {
		t.Fatal("review reused obsolete hard evidence", err, v)
	}
}

func TestReviewRegistrationRejectsWrongParentTreeAndRevokedCommitWithoutSideEffects(t *testing.T) {
	s, _ := openFixture(t)
	a, _, _, _ := reviewedFixture(t, s)
	raw := `{"version":1,"verdict":"approve","findings":[]}`
	for _, mode := range []string{"foreign_parent", "changed_tree", "changed_input", "revoked_commit"} {
		t.Run(mode, func(t *testing.T) {
			bad := a
			calls := 0
			current := func() bool {
				calls++
				return mode != "revoked_commit" || calls < 3
			}
			switch mode {
			case "foreign_parent":
				bad.ParentRunID = "another-testing-run"
			case "changed_tree":
				bad.Reference.TreeHash = hash([]byte("different code"))
			case "changed_input":
				bad.InputTreeHash = hash([]byte("different input"))
			}
			before, _ := s.Events(a.Reference.Binding.TaskID, 0)
			if err := s.RecordReviewedArtifactAuthorized(bad, raw, current); err == nil {
				t.Fatal("invalid review producer committed")
			}
			after, _ := s.Events(a.Reference.Binding.TaskID, 0)
			if len(before) != len(after) {
				t.Fatal("invalid review wrote event")
			}
			if _, err := s.Artifact(a.Reference.Binding.RunID); !errors.Is(err, ErrNotFound) {
				t.Fatal("invalid review left receipt", err)
			}
		})
	}
}

func TestReviewBlockingOpinionRollsBackWithEventAndInvalidOpinionStopsWorkflow(t *testing.T) {
	for _, raw := range []string{`{"version":1,"verdict":"changes_required","findings":[{"id":"R1","severity":"blocking","summary":"fix in implementation"}]}`, `{"verdict":"accepted"}`} {
		t.Run(raw, func(t *testing.T) {
			s, _ := openFixture(t)
			a, _, _, _ := reviewedFixture(t, s)
			if _, err := s.db.Exec("CREATE TRIGGER fixture_review_fail BEFORE INSERT ON events WHEN NEW.kind='review_requires_review' BEGIN SELECT RAISE(ABORT,'fixture'); END"); err != nil {
				t.Fatal(err)
			}
			if err := s.RecordReviewedArtifactAuthorized(a, raw, func() bool { return true }); err == nil {
				t.Fatal("failed review event committed")
			}
			task, _ := s.Task(a.Reference.Binding.TaskID)
			if task.State != "ready" {
				t.Fatal("partial review state survived rollback", task.State)
			}
			if _, err := s.Artifact(a.Reference.Binding.RunID); !errors.Is(err, ErrNotFound) {
				t.Fatal("partial receipt survived rollback", err)
			}
			if _, err := s.db.Exec("DROP TRIGGER fixture_review_fail"); err != nil {
				t.Fatal(err)
			}
			if err := s.RecordReviewedArtifactAuthorized(a, raw, func() bool { return true }); err != nil {
				t.Fatal(err)
			}
			task, _ = s.Task(a.Reference.Binding.TaskID)
			if task.State != "needs_review" {
				t.Fatal("blocking/invalid opinion advanced workflow", task.State)
			}
			if err := s.RecordReviewedArtifactAuthorized(a, raw, func() bool { return true }); err != nil {
				t.Fatal("blocked exact retry changed receipt", err)
			}
			if _, err := s.Reservation(a.Reference.Binding.RunID); err != nil {
				t.Fatal("review registration released Native reservation", err)
			}
		})
	}
}

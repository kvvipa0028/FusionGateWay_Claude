//go:build darwin

package store

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"testing"

	"github.com/yetone/magpie/internal/fusion/evidence"
	"github.com/yetone/magpie/internal/fusion/handoff"
	"github.com/yetone/magpie/internal/fusion/stageplan"
)

var acceptanceCapture = flag.String("fusion-acceptance-capture", "", "capture synthetic Store decision with actual verifier for offline checks")

func acceptanceFixture(t *testing.T, s *Store) (ArtifactRecord, evidence.Spec, string, string) {
	t.Helper()
	review, spec, source, root := reviewedFixture(t, s)
	if err := s.RecordReviewedArtifactAuthorized(review, `{"version":1,"verdict":"approve","findings":[]}`, func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	if err := s.ReleaseReserved(review.Reference.Binding.RunID, review.Reference.Binding.Generation, review.Reference.StopProofHash, true); err != nil {
		t.Fatal(err)
	}
	parent, err := handoff.Restore(review.Reference, source, root)
	if err != nil {
		t.Fatal(err)
	}
	copy, err := parent.Copy(review.Reference.Binding, root, "acceptance")
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Task(review.Reference.Binding.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	in, req := workflowStart(t, s, task, stageplan.Acceptance, "acceptance-fixture")
	started, err := s.StartReservedOnce(in, req)
	if err != nil {
		t.Fatal(err)
	}
	_, record := publishWorkflowArtifact(t, s, task, started.Run, copy, root, "acceptance-artifact")
	record.ParentRunID, record.InputTreeHash = review.Reference.Binding.RunID, review.Reference.TreeHash
	return record, spec, source, root
}

func TestAcceptanceKeepsHardModelAndHumanSeparateAndSurvivesRestart(t *testing.T) {
	s, dbroot := openFixture(t)
	a, spec, source, root := acceptanceFixture(t, s)
	raw := `{"version":1,"verdict":"accepted","criteria":[{"index":0,"status":"met","reason":"actual pinned report and readonly review checked"}]}`
	if err := s.RecordAcceptanceArtifactAuthorized(a, raw, func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AcceptanceArtifact(a.Reference.Binding.RunID, source, root, spec); !errors.Is(err, ErrArtifactPending) {
		t.Fatal("held acceptance leaked opinion", err)
	}
	if err := s.ReleaseReserved(a.Reference.Binding.RunID, a.Reference.Binding.Generation, a.Reference.StopProofHash, true); err != nil {
		t.Fatal(err)
	}
	got, hard, err := s.AcceptanceArtifact(a.Reference.Binding.RunID, source, root, spec)
	if err != nil || hard.Status != evidence.Passed || got.Acceptance == nil || !got.Acceptance.Valid || got.Acceptance.Document.Verdict != "accepted" {
		t.Fatal("separate actual hard check/model decision", err, hard)
	}
	task, _ := s.Task(a.Reference.Binding.TaskID)
	if task.State != "advisory_only" {
		t.Fatal("model acceptance became human completion", task.State)
	}
	if *acceptanceCapture != "" {
		capture, err := json.MarshalIndent(map[string]any{"artifact": got, "hard_verdict": hard, "task": task}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(*acceptanceCapture, append(capture, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RecordArtifact(got); !errors.Is(err, ErrInvalid) {
		t.Fatal("public self-declared acceptance accepted", err)
	}
	before, _ := s.Events(task.ID, 0)
	if err := s.RecordAcceptanceArtifactAuthorized(a, raw, func() bool { return true }); err != nil {
		t.Fatal("exact retry changed decision", err)
	}
	after, _ := s.Events(task.ID, 0)
	if len(before) != len(after) {
		t.Fatal("exact retry duplicated decision event")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dbroot)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, v, err := s.AcceptanceArtifact(a.Reference.Binding.RunID, source, root, spec); err != nil || v.Status != evidence.Passed {
		t.Fatal("restart lost decision", err, v)
	}
	spec.Rules.MinTests++
	if _, v, err := s.AcceptanceArtifact(a.Reference.Binding.RunID, source, root, spec); err != nil || v.Status != evidence.Superseded {
		t.Fatal("model decision reused superseded hard check", err, v)
	}
}

func TestAcceptanceEventFailureRollsBackAndRejectionNeverCompletesTask(t *testing.T) {
	for _, tc := range []struct {
		name, raw, state string
	}{
		{"accepted", `{"version":1,"verdict":"accepted","criteria":[{"index":0,"status":"met","reason":"checked"}]}`, "advisory_only"},
		{"rejected", `{"version":1,"verdict":"rejected","criteria":[{"index":0,"status":"not_met","reason":"defect"}]}`, "needs_review"},
		{"unverified", `{"version":1,"verdict":"unverified","criteria":[{"index":0,"status":"unverified","reason":"no HIL evidence"}]}`, "needs_review"},
		{"missing_criteria", `{"version":1,"verdict":"accepted","criteria":[]}`, "needs_review"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := tc.raw
			s, _ := openFixture(t)
			a, _, _, _ := acceptanceFixture(t, s)
			if _, err := s.db.Exec("CREATE TRIGGER fixture_accept_fail BEFORE INSERT ON events WHEN NEW.kind IN ('acceptance_requires_review','acceptance_awaiting_human') BEGIN SELECT RAISE(ABORT,'fixture'); END"); err != nil {
				t.Fatal(err)
			}
			if err := s.RecordAcceptanceArtifactAuthorized(a, raw, func() bool { return true }); err == nil {
				t.Fatal("failed decision event committed")
			}
			task, _ := s.Task(a.Reference.Binding.TaskID)
			if task.State != "ready" {
				t.Fatal("partial task state survived", task.State)
			}
			if _, err := s.Artifact(a.Reference.Binding.RunID); !errors.Is(err, ErrNotFound) {
				t.Fatal("partial decision survived", err)
			}
			if _, err := s.db.Exec("DROP TRIGGER fixture_accept_fail"); err != nil {
				t.Fatal(err)
			}
			if err := s.RecordAcceptanceArtifactAuthorized(a, raw, func() bool { return true }); err != nil {
				t.Fatal(err)
			}
			want := tc.state
			task, _ = s.Task(a.Reference.Binding.TaskID)
			if task.State != want {
				t.Fatal("model decision wrongly completed/advanced task", task.State, want)
			}
			if err := s.RecordAcceptanceArtifactAuthorized(a, raw, func() bool { return true }); err != nil {
				t.Fatal("terminal exact retry changed decision", err)
			}
			if _, err := s.Reservation(a.Reference.Binding.RunID); err != nil {
				t.Fatal("decision registration released reservation", err)
			}
		})
	}
}

func TestAcceptanceInvalidProducerAndRevokedCommitLeaveNoDecision(t *testing.T) {
	s, _ := openFixture(t)
	a, _, _, _ := acceptanceFixture(t, s)
	raw := `{"version":1,"verdict":"accepted","criteria":[{"index":0,"status":"met","reason":"checked"}]}`
	for _, mode := range []string{"wrong_parent", "changed_tree", "wrong_input", "revoked_commit", "wrong_role"} {
		t.Run(mode, func(t *testing.T) {
			bad := a
			calls := 0
			current := func() bool { calls++; return mode != "revoked_commit" || calls < 3 }
			switch mode {
			case "wrong_parent":
				bad.ParentRunID = "another-review-run"
			case "changed_tree":
				bad.Reference.TreeHash = hash([]byte("different code"))
			case "wrong_input":
				bad.InputTreeHash = hash([]byte("different input"))
			case "wrong_role":
				bad.Reference.Binding.Role = stageplan.Review
			}
			before, _ := s.Events(a.Reference.Binding.TaskID, 0)
			if err := s.RecordAcceptanceArtifactAuthorized(bad, raw, current); err == nil {
				t.Fatal("invalid authority/binding registered decision")
			}
			after, _ := s.Events(a.Reference.Binding.TaskID, 0)
			if len(before) != len(after) {
				t.Fatal("invalid producer wrote events")
			}
			if _, err := s.Artifact(a.Reference.Binding.RunID); !errors.Is(err, ErrNotFound) {
				t.Fatal("invalid decision persisted", err)
			}
		})
	}
}

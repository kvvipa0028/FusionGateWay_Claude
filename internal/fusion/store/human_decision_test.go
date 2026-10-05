//go:build darwin

package store

import (
	"encoding/json"
	"errors"
	"flag"
	"github.com/yetone/magpie/internal/fusion/evidence"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func humanFixture(t *testing.T, raw string) (*Store, string, ArtifactRecord, evidence.Spec, string, string) {
	t.Helper()
	s, dbroot := openFixture(t)
	a, spec, source, root := acceptanceFixture(t, s)
	if err := s.RecordAcceptanceArtifactAuthorized(a, raw, func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	if err := s.ReleaseReserved(a.Reference.Binding.RunID, a.Reference.Binding.Generation, a.Reference.StopProofHash, true); err != nil {
		t.Fatal(err)
	}
	return s, dbroot, a, spec, source, root
}

var humanDecisionCapture = flag.String("fusion-human-decision-capture", "", "capture actual verifier with synthetic Native human fixture")

const humanAcceptedFixture = `{"version":1,"verdict":"accepted","criteria":[{"index":0,"status":"met","reason":"checked"}]}`

func TestHumanDecisionRequiresOwnedCurrentEvidenceAndPreservesNativeBudget(t *testing.T) {
	s, dbroot, a, spec, source, root := humanFixture(t, humanAcceptedFixture)
	tsk, _ := s.Task(a.Reference.Binding.TaskID)
	beforeBudget, _ := s.Budget(tsk.ID)
	beforeRun, _ := s.Run(a.Reference.Binding.RunID)
	expected := TaskVersion{tsk.PlanRevision, tsk.Generation, tsk.State}
	if _, err := s.DecideHumanAcceptance(tsk.ID, expected, FinalEvidence{}, "accept", "checked delivery", func() bool { return true }); err == nil {
		t.Fatal("caller JSON/zero value granted human completion")
	}
	permit, err := s.PrepareFinalEvidence(a.Reference.Binding.RunID, source, root, spec)
	if err != nil {
		t.Fatal(err)
	}
	display := permit.Report()
	display.Hard.Status = evidence.Failed
	display.Model.Verdict = "rejected" // presentation copy cannot change authority.
	decision, err := s.DecideHumanAcceptance(tsk.ID, expected, permit, "accept", "checked delivery", func() bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	after, _ := s.Task(tsk.ID)
	if after.State != "completed" || decision.Action != "accept" || decision.Authority != "management" || decision.RunID != a.Reference.Binding.RunID {
		t.Fatal("explicit human accept lost", after, decision)
	}
	run, _ := s.Run(beforeRun.ID)
	budget, _ := s.Budget(tsk.ID)
	if !reflect.DeepEqual(run, beforeRun) || budget != beforeBudget {
		t.Fatal("human decision changed actual Native or task budget")
	}
	events, _ := s.Events(tsk.ID, 0)
	current := TaskVersion{after.PlanRevision, after.Generation, after.State}
	again, err := s.DecideHumanAcceptance(tsk.ID, current, permit, "accept", "checked delivery", func() bool { return true })
	if err != nil || again != decision {
		t.Fatal("exact fresh-condition retry", err)
	}
	repeated, _ := s.Events(tsk.ID, 0)
	if len(events) != len(repeated) {
		t.Fatal("retry duplicated decision event")
	}
	if _, err := s.DecideHumanAcceptance(tsk.ID, current, permit, "return", "changed mind", func() bool { return true }); err == nil {
		t.Fatal("immutable human decision replaced")
	}
	for _, query := range []string{"UPDATE human_acceptance_decisions SET decision_json='{}'", "DELETE FROM human_acceptance_decisions"} {
		if _, err := s.db.Exec(query); err == nil {
			t.Fatal("immutable operator fact modified")
		}
	}
	if *humanDecisionCapture != "" {
		raw, err := json.MarshalIndent(map[string]any{"report": permit.Report(), "decision": decision, "task": after}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(*humanDecisionCapture, append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dbroot)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.HumanDecision(tsk.ID)
	if err != nil || got != decision {
		t.Fatal("restart lost actual human receipt", got, err)
	}
	if _, err := s.DecideHumanAcceptance(tsk.ID, current, permit, "accept", "checked delivery", func() bool { return true }); err == nil {
		t.Fatal("foreign/closed Store token trusted")
	}
}
func TestHumanAcceptCannotOverrideModelUnknownOrCurrentStandard(t *testing.T) {
	for _, mode := range []string{"unverified", "changed_standard"} {
		t.Run(mode, func(t *testing.T) {
			raw := humanAcceptedFixture
			if mode == "unverified" {
				raw = `{"version":1,"verdict":"unverified","criteria":[{"index":0,"status":"unverified","reason":"HIL absent"}]}`
			}
			s, _, a, spec, source, root := humanFixture(t, raw)
			if mode == "changed_standard" {
				spec.Rules.MinTests++
			}
			tsk, _ := s.Task(a.Reference.Binding.TaskID)
			permit, err := s.PrepareFinalEvidence(a.Reference.Binding.RunID, source, root, spec)
			if err != nil {
				t.Fatal(err)
			}
			expected := TaskVersion{tsk.PlanRevision, tsk.Generation, tsk.State}
			if _, err := s.DecideHumanAcceptance(tsk.ID, expected, permit, "accept", "override evidence", func() bool { return true }); err == nil {
				t.Fatal("human action bypassed hard/model gate")
			}
			if _, err := s.HumanDecision(tsk.ID); !errors.Is(err, ErrNotFound) {
				t.Fatal("rejected accept left receipt", err)
			}
			d, err := s.DecideHumanAcceptance(tsk.ID, expected, permit, "return", "need real evidence", func() bool { return true })
			if err != nil || d.Action != "return" {
				t.Fatal(err)
			}
			after, _ := s.Task(tsk.ID)
			if after.State != "needs_review" {
				t.Fatal("return auto-completed or started rework", after)
			}
		})
	}
}
func TestHumanDecisionRechecksFilesVersionAuthorityAndRollsBackEvent(t *testing.T) {
	for _, mode := range []string{"stale_task", "revoked_commit", "event_failure", "changed_header", "late_header", "source_drift", "invalid_action", "empty_reason"} {
		t.Run(mode, func(t *testing.T) {
			s, _, a, spec, source, root := humanFixture(t, humanAcceptedFixture)
			tsk, _ := s.Task(a.Reference.Binding.TaskID)
			permit, err := s.PrepareFinalEvidence(a.Reference.Binding.RunID, source, root, spec)
			if err != nil {
				t.Fatal(err)
			}
			expected := TaskVersion{tsk.PlanRevision, tsk.Generation, tsk.State}
			action, reason := "accept", "checked delivery"
			current := func() bool { return true }
			calls := 0
			switch mode {
			case "stale_task":
				expected.Generation--
			case "revoked_commit":
				current = func() bool { calls++; return calls < 2 }
			case "event_failure":
				_, err = s.db.Exec("CREATE TRIGGER fail_human BEFORE INSERT ON events WHEN NEW.kind='human_accepted' BEGIN SELECT RAISE(ABORT,'fixture'); END")
				if err != nil {
					t.Fatal(err)
				}
			case "changed_header":
				replaceArtifactHeader(t, a.Reference.Path)
			case "late_header":
				current = func() bool {
					calls++
					if calls == 2 {
						replaceArtifactHeader(t, a.Reference.Path)
					}
					return true
				}
			case "source_drift":
				if err := os.WriteFile(filepath.Join(source, "source-added.txt"), []byte("new original source"), 0600); err != nil {
					t.Fatal(err)
				}
			case "invalid_action":
				action = "completed"
			case "empty_reason":
				reason = " "
			}
			before, _ := s.Events(tsk.ID, 0)
			if _, err := s.DecideHumanAcceptance(tsk.ID, expected, permit, action, reason, current); err == nil {
				t.Fatal("invalid decision acknowledged")
			}
			after, _ := s.Task(tsk.ID)
			if after != tsk {
				t.Fatal("failed decision changed task", after)
			}
			events, _ := s.Events(tsk.ID, 0)
			if len(events) != len(before) {
				t.Fatal("failed decision left event")
			}
			if _, err := s.HumanDecision(tsk.ID); !errors.Is(err, ErrNotFound) {
				t.Fatal("failed decision left receipt", err)
			}
		})
	}
}

func replaceArtifactHeader(t *testing.T, root string) {
	t.Helper()
	p := filepath.Join(root, "handoff.json")
	if err := os.Chmod(p, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("changed after human evidence read"), 0400); err != nil {
		t.Fatal(err)
	}
}

func TestHumanConcurrentFreshConditionProducesOneDecision(t *testing.T) {
	s, _, a, spec, source, root := humanFixture(t, humanAcceptedFixture)
	task, _ := s.Task(a.Reference.Binding.TaskID)
	p, err := s.PrepareFinalEvidence(a.Reference.Binding.RunID, source, root, spec)
	if err != nil {
		t.Fatal(err)
	}
	expected := TaskVersion{task.PlanRevision, task.Generation, task.State}
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := s.DecideHumanAcceptance(task.ID, expected, p, "accept", "operator explicitly reviewed", func() bool { return true })
			results <- err
		}()
	}
	success, conflict := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("lost whole TaskVersion CAS", success, conflict)
	}
	events, _ := s.Events(task.ID, 0)
	count := 0
	for _, event := range events {
		if event.Kind == "human_accepted" {
			count++
		}
	}
	if count != 1 {
		t.Fatal("duplicate human authority event", count)
	}
}

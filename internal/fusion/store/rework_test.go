//go:build darwin

package store

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/yetone/magpie/internal/fusion/evidence"
)

func TestReworkRejectsChangedHardStandardAndNonRepairOpinions(t *testing.T) {
	for _, mode := range []string{"changed_standard", "approve", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := openFixture(t)
			a, spec, source, root := reviewedFixture(t, s)
			raw := `{"version":1,"verdict":"changes_required","findings":[{"id":"R1","severity":"blocking","summary":"repair current source"}]}`
			if mode == "approve" {
				raw = `{"version":1,"verdict":"approve","findings":[]}`
			} else if mode == "invalid" {
				raw = "unstructured model opinion"
			}
			if e := s.RecordReviewedArtifactAuthorized(a, raw, func() bool { return true }); e != nil {
				t.Fatal(e)
			}
			if e := s.ReleaseReserved(a.Reference.Binding.RunID, a.Reference.Binding.Generation, a.Reference.StopProofHash, true); e != nil {
				t.Fatal(e)
			}
			if mode == "changed_standard" {
				spec.Rules.MinTests = 2
				_, hard, e := s.ReviewedArtifact(a.Reference.Binding.RunID, source, root, spec)
				if e != nil || hard.Status != evidence.Superseded {
					t.Fatal("changed standard was not independently detected", e, hard.Status)
				}
			}
			if _, e := s.PrepareReworkEvidence(a.Reference.Binding.RunID, source, root, spec); !errors.Is(e, ErrWorkflowGate) {
				t.Fatal("non-current hard proof or non-repair opinion admitted repair", e)
			}
			budget, _ := s.Budget(a.Reference.Binding.TaskID)
			if budget.UsedReworks != 0 {
				t.Fatal("proof preparation spent a round")
			}
		})
	}
}

func reworkFixture(t *testing.T, s *Store) ReworkEvidence {
	t.Helper()
	a, spec, source, root := reviewedFixture(t, s)
	if e := s.RecordReviewedArtifactAuthorized(a, `{"version":1,"verdict":"changes_required","findings":[{"id":"R1","severity":"blocking","summary":"repair exact current source"}]}`, func() bool { return true }); e != nil {
		t.Fatal(e)
	}
	if e := s.ReleaseReserved(a.Reference.Binding.RunID, a.Reference.Binding.Generation, a.Reference.StopProofHash, true); e != nil {
		t.Fatal(e)
	}
	p, e := s.PrepareReworkEvidence(a.Reference.Binding.RunID, source, root, spec)
	if e != nil {
		t.Fatal(e)
	}
	return p
}

func TestReworkCurrentFilesStaleConditionsAndSharedBudgetBlockRound(t *testing.T) {
	for _, mode := range []string{"header", "stale", "zero", "used"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := openFixture(t)
			p := reworkFixture(t, s)
			task, _ := s.Task(p.artifact.Reference.Binding.TaskID)
			v := TaskVersion{PlanRevision: task.PlanRevision, Generation: task.Generation, State: task.State}
			switch mode {
			case "header":
				path := filepath.Join(p.artifact.Reference.Path, "handoff.json")
				if e := os.Chmod(path, 0600); e != nil {
					t.Fatal(e)
				}
				if e := os.WriteFile(path, []byte("changed header"), 0400); e != nil {
					t.Fatal(e)
				}
			case "stale":
				v.Generation--
			case "zero":
				if _, e := s.db.Exec("UPDATE task_budgets SET max_reworks=0 WHERE task_id=?", task.ID); e != nil {
					t.Fatal(e)
				}
			case "used":
				if e := s.ReserveRework(task.ID, task.Generation); e != nil {
					t.Fatal(e)
				}
			}
			budget, _ := s.Budget(task.ID)
			if _, e := s.BeginReworkAuthorized(task.ID, v, p, func() bool { return true }); e == nil {
				t.Fatal("invalid repair admitted", mode)
			}
			current, _ := s.Task(task.ID)
			after, _ := s.Budget(task.ID)
			if current != task || after != budget {
				t.Fatal("blocked repair changed state/budget")
			}
			if _, e := s.Rework(task.ID); !errors.Is(e, ErrNotFound) {
				t.Fatal("blocked repair persisted round", e)
			}
		})
	}
}

func TestReworkConcurrentBeginRestartAndImmutableReceipt(t *testing.T) {
	s, root := openFixture(t)
	p := reworkFixture(t, s)
	task, _ := s.Task(p.artifact.Reference.Binding.TaskID)
	v := TaskVersion{PlanRevision: task.PlanRevision, Generation: task.Generation, State: task.State}
	var wg sync.WaitGroup
	ch := make(chan error, 8)
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.BeginReworkAuthorized(task.ID, v, p, func() bool { return true })
			ch <- e
		}()
	}
	wg.Wait()
	close(ch)
	wins := 0
	for e := range ch {
		if e == nil {
			wins++
		} else if !errors.Is(e, ErrConflict) {
			t.Fatal(e)
		}
	}
	if wins != 1 {
		t.Fatal("multiple repair rounds", wins)
	}
	before, e := s.Rework(task.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, sql := range []string{"UPDATE workflow_reworks SET record_hash='changed'", "DELETE FROM workflow_reworks"} {
		if _, e := s.db.Exec(sql); e == nil {
			t.Fatal("repair history mutable")
		}
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	after, e := s.Rework(task.ID)
	view, ve := s.Workflow(task.ID)
	if e != nil || ve != nil || before != after || view.Next != "implementation" || view.Blocker != "" {
		t.Fatal("restart lost finite round", e, ve)
	}
	if _, e = s.BeginReworkAuthorized(task.ID, v, p, func() bool { return true }); e == nil {
		t.Fatal("old Store evidence survived reopen")
	}
}

func TestReworkOwnedEvidenceOpensOnlyOneRoundWithExactReviewParent(t *testing.T) {
	s, _ := openFixture(t)
	p := reworkFixture(t, s)
	task, _ := s.Task(p.artifact.Reference.Binding.TaskID)
	version := TaskVersion{PlanRevision: task.PlanRevision, Generation: task.Generation, State: task.State}
	before, _ := s.Budget(task.ID)
	r, e := s.BeginReworkAuthorized(task.ID, version, p, func() bool { return true })
	if e != nil || r.ReviewRunID != p.artifact.Reference.Binding.RunID {
		t.Fatal("owned review did not open repair", e)
	}
	task, _ = s.Task(task.ID)
	if task.State != "ready" || task.Generation != 4 {
		t.Fatal("repair reset or invented stage generation", task)
	}
	v, e := s.Workflow(task.ID)
	if e != nil || v.Next != "implementation" || v.Blocker != "" || len(v.Definition.RequiredRoles) != 5 {
		t.Fatal("finite repair did not retain original definition", v, e)
	}
	in, e := s.StageArtifactInput(task.ID, TaskVersion{PlanRevision: task.PlanRevision, Generation: task.Generation, State: task.State}, "implementation")
	if e != nil || in.Parent == nil || !reflect.DeepEqual(*in.Parent, p.artifact) {
		t.Fatal("repair did not receive review problems and exact parent", e)
	}
	after, _ := s.Budget(task.ID)
	if after.UsedReworks != 1 || before.UsedCalls != after.UsedCalls {
		t.Fatal("repair reset/spent model budget", after)
	}
	if _, e = s.BeginReworkAuthorized(task.ID, version, p, func() bool { return true }); e == nil {
		t.Fatal("same opinion opened another round")
	}
}

func TestReworkOwnershipAuthorityAndEventFailureCannotInventRound(t *testing.T) {
	s, _ := openFixture(t)
	p := reworkFixture(t, s)
	task, _ := s.Task(p.artifact.Reference.Binding.TaskID)
	v := TaskVersion{PlanRevision: task.PlanRevision, Generation: task.Generation, State: task.State}
	before, _ := s.Events(task.ID, 0)
	for _, proof := range []ReworkEvidence{{}, {artifact: p.artifact}} {
		if _, e := s.BeginReworkAuthorized(task.ID, v, proof, func() bool { return true }); e == nil {
			t.Fatal("presentation metadata granted repair")
		}
	}
	calls := 0
	if _, e := s.BeginReworkAuthorized(task.ID, v, p, func() bool { calls++; return calls == 1 }); !errors.Is(e, ErrWorkflowAuthority) {
		t.Fatal("revoked repair persisted", e)
	}
	if _, e := s.db.Exec(`CREATE TRIGGER reject_rework BEFORE INSERT ON events WHEN NEW.kind='rework_started' BEGIN SELECT RAISE(ABORT,'fixture'); END`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.BeginReworkAuthorized(task.ID, v, p, func() bool { return true }); e == nil {
		t.Fatal("failed audit event admitted repair")
	}
	after, _ := s.Events(task.ID, 0)
	current, _ := s.Task(task.ID)
	budget, _ := s.Budget(task.ID)
	if !reflect.DeepEqual(before, after) || current != task || budget.UsedReworks != 0 {
		t.Fatal("failed repair left partial state")
	}
	if _, e := s.Rework(task.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("failed round left receipt", e)
	}
}

package store

import (
	"errors"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/yetone/magpie/internal/fusion/workflow"
)

func TestWorkflowAuthorityIsRecheckedInsideWriteAndRollsBackEveryMetadataDecision(t *testing.T) {
	for _, operation := range []string{"attach", "design", "approve"} {
		t.Run(operation, func(t *testing.T) {
			s, _ := openFixture(t)
			var task Task
			var saved WorkflowView
			var runID string
			doc := workflow.DesignDocument{Goal: "fixture goal", Scope: []string{"src"}, Constraints: []string{"preserve API"}, Interfaces: []string{"typed API"}, Acceptance: []string{"tests pass"}}
			if operation == "attach" {
				var e error
				task, e = s.Create("authorized-attach", CreateRequest{ProjectID: "fixture-project", Goal: doc.Goal, Plan: plan(t, 1)})
				if e != nil {
					t.Fatal(e)
				}
			} else if operation == "design" {
				task = workflowTask(t, s, "change")
				in, req := workflowStart(t, s, task, "design", "authorized-design")
				r, e := s.StartReservedOnce(in, req)
				if e != nil {
					t.Fatal(e)
				}
				finishWorkflowRun(t, s, r.Run)
				runID = r.Run.ID
				task, e = s.Task(task.ID)
				if e != nil {
					t.Fatal(e)
				}
			} else {
				task, saved = completedWorkflowDesign(t, s)
			}
			version := TaskVersion{task.PlanRevision, task.Generation, task.State}
			call := func(current func() bool) (WorkflowView, error) {
				switch operation {
				case "attach":
					return s.AttachWorkflowAuthorized(task.ID, version, "investigate", current)
				case "design":
					return s.SaveWorkflowDesignAuthorized(task.ID, version, runID, doc, current)
				default:
					return s.ApproveWorkflowDesignAuthorized(task.ID, version, saved.Design.Snapshot.Hash, saved.Design.Snapshot.AcceptanceHash, current)
				}
			}
			before, e := s.Events(task.ID, 0)
			if e != nil {
				t.Fatal(e)
			}
			for _, current := range []func() bool{nil, func() bool { return false }} {
				if out, e := call(current); !errors.Is(e, ErrWorkflowAuthority) || !reflect.DeepEqual(out, WorkflowView{}) {
					t.Fatal("absent authority exposed or wrote decision", e)
				}
			}
			calls := 0
			if out, e := call(func() bool { calls++; return calls == 1 }); !errors.Is(e, ErrWorkflowAuthority) || !reflect.DeepEqual(out, WorkflowView{}) {
				t.Fatal("authority loss after write was committed", e)
			}
			if calls != 2 {
				t.Fatal("authority not checked before/after transaction")
			}
			after, e := s.Events(task.ID, 0)
			if e != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("revoked decision persisted events", e)
			}
			v, e := s.Workflow(task.ID)
			if operation == "attach" {
				if !errors.Is(e, ErrNotFound) {
					t.Fatal("revoked attach persisted", e)
				}
			} else if e != nil || operation == "design" && v.Design != nil || operation == "approve" && v.Approval != nil {
				t.Fatal("revoked design/approval persisted", e)
			}
			if _, e = call(func() bool { return true }); e != nil {
				t.Fatal("authorized retry failed", e)
			}
		})
	}
}

func TestWorkflowAuthorityLossDuringStoreMutexWaitCannotAttach(t *testing.T) {
	s, _ := openFixture(t)
	task, e := s.Create("waiting-attach", CreateRequest{ProjectID: "fixture-project", Goal: "fixture goal", Plan: plan(t, 1)})
	if e != nil {
		t.Fatal(e)
	}
	var current atomic.Bool
	current.Store(true)
	s.mu.Lock()
	entered := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(entered)
		_, e := s.AttachWorkflowAuthorized(task.ID, TaskVersion{1, 0, "ready"}, "investigate", current.Load)
		done <- e
	}()
	<-entered
	current.Store(false)
	s.mu.Unlock()
	if e = <-done; !errors.Is(e, ErrWorkflowAuthority) {
		t.Fatal("revoked wait attached", e)
	}
	if _, e = s.Workflow(task.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("revoked wait changed Store", e)
	}
}

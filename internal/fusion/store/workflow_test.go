package store

import (
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/workflow"
)

func workflowTask(t *testing.T, s *Store, kind workflow.Kind) Task {
	t.Helper()
	d, e := workflow.DefinitionFor(kind)
	if e != nil {
		t.Fatal(e)
	}
	var p stageplan.Snapshot
	layer := stageplan.Layer{Roles: map[stageplan.Role]stageplan.Binding{}}
	// Recompile through the real trusted compiler, with one synthetic route.
	route := stageplan.Route{ID: "fixture-route", Revision: 1, Model: "fixture-model-a", Account: "fixture-account-a", Workspace: "fixture-workspace", CredentialIdentity: "fixture-credential-a", RuntimeVersion: "fixture-runtime-v1", BillingPath: "fixture-subscription", BillingKnown: true, Admitted: true, Efforts: []string{"medium"}, Capabilities: []string{"text"}, LockEnforcement: stageplan.ControlledCalls}
	for _, role := range d.RequiredRoles {
		layer.Roles[role] = stageplan.Binding{Mode: stageplan.Locked, Route: &routeRefForWorkflow, Model: route.Model, Effort: &stageplan.EffortSelection{Mode: stageplan.EffortExplicit, Value: "medium"}}
	}
	p, e = stageplan.Compile(1, d.RequiredRoles, layer, stageplan.Layer{}, stageplan.Layer{}, []stageplan.Route{route})
	if e != nil {
		t.Fatal(e)
	}
	task, e := s.Create("fixture-workflow", CreateRequest{ProjectID: "fixture-project", Goal: "fixture goal", Plan: p, Budget: &Budget{MaxCalls: 20, MaxReworks: 1}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.AttachWorkflow(task.ID, TaskVersion{1, 0, "ready"}, kind); e != nil {
		t.Fatal(e)
	}
	return task
}

var routeRefForWorkflow = stageplan.RouteRef{ID: "fixture-route", Revision: 1}

func TestWorkflowAttachAndDesignEventFailureRollBackWithoutPartialAuthorization(t *testing.T) {
	s, _ := openFixture(t)
	task, e := s.Create("attachment-rollback", CreateRequest{ProjectID: "fixture-project", Goal: "fixture goal", Plan: plan(t, 1), Budget: &Budget{MaxCalls: 3}})
	if e != nil {
		t.Fatal(e)
	}
	before, e := s.Events(task.ID, 0)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.AttachWorkflow(task.ID, TaskVersion{1, 0, "ready"}, "change"); !errors.Is(e, ErrConflict) {
		t.Fatal("wrong role set accepted", e)
	}
	if _, e = s.db.Exec("CREATE TRIGGER fixture_attach_fail BEFORE INSERT ON events WHEN NEW.kind='workflow_attached' BEGIN SELECT RAISE(ABORT,'fixture'); END"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.AttachWorkflow(task.ID, TaskVersion{1, 0, "ready"}, "investigate"); e == nil {
		t.Fatal("failed event acknowledged")
	}
	if _, e = s.Workflow(task.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("partial workflow survived", e)
	}
	if _, e = s.db.Exec("DROP TRIGGER fixture_attach_fail"); e != nil {
		t.Fatal(e)
	}
	v, e := s.AttachWorkflow(task.ID, TaskVersion{1, 0, "ready"}, "investigate")
	if e != nil {
		t.Fatal(e)
	}
	if replay, e := s.AttachWorkflow(task.ID, TaskVersion{1, 0, "ready"}, "investigate"); e != nil || !reflect.DeepEqual(replay, v) {
		t.Fatal("attachment not idempotent", e)
	}
	if _, e = s.AttachWorkflow(task.ID, TaskVersion{1, 0, "ready"}, "review"); !errors.Is(e, ErrConflict) {
		t.Fatal("definition replaced", e)
	}
	after, e := s.Events(task.ID, 0)
	if e != nil || len(after) != len(before)+1 {
		t.Fatal("failed/replayed attachment wrote events", e)
	}
	in, req := workflowStart(t, s, task, stageplan.Design, "rollback-design")
	r, e := s.StartReservedOnce(in, req)
	if e != nil {
		t.Fatal(e)
	}
	finishWorkflowRun(t, s, r.Run)
	doc := workflow.DesignDocument{Goal: task.Goal, Scope: []string{"src"}, Constraints: []string{"preserve API"}, Interfaces: []string{"typed API"}, Acceptance: []string{"tests pass"}}
	if _, e = s.db.Exec("CREATE TRIGGER fixture_design_fail BEFORE INSERT ON events WHEN NEW.kind='design_frozen' BEGIN SELECT RAISE(ABORT,'fixture'); END"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveWorkflowDesign(task.ID, TaskVersion{1, 1, "ready"}, r.Run.ID, doc); e == nil {
		t.Fatal("failed design event acknowledged")
	}
	if v, e = s.Workflow(task.ID); e != nil || v.Design != nil || v.Blocker != "design_required" {
		t.Fatal("partial design survived", e)
	}
	if _, e = s.db.Exec("DROP TRIGGER fixture_design_fail"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveWorkflowDesign(task.ID, TaskVersion{1, 1, "ready"}, r.Run.ID, doc); e != nil {
		t.Fatal("design retry failed", e)
	}
}

func TestWorkflowFailedDesignNeverPermitsApprovalOrAnotherStage(t *testing.T) {
	s, _ := openFixture(t)
	task := workflowTask(t, s, "change")
	in, req := workflowStart(t, s, task, stageplan.Design, "failed-design")
	r, e := s.StartReservedOnce(in, req)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConfirmStarted(r.Run.ID, r.Run.Generation, r.Run.Owner, "fixture-failed-session"); e != nil {
		t.Fatal(e)
	}
	if e = s.Finish(r.Run.ID, r.Run.Generation, r.Run.Owner, "failed"); e != nil {
		t.Fatal(e)
	}
	if e = s.ReleaseReserved(r.Run.ID, r.Run.Generation, hash([]byte("fixture-stop")), true); e != nil {
		t.Fatal(e)
	}
	v, e := s.Workflow(task.ID)
	if e != nil || v.Blocker != "stage_failed" || v.Next != "" {
		t.Fatal("failed design advanced", e)
	}
	task, e = s.Task(task.ID)
	if e != nil {
		t.Fatal(e)
	}
	if task.State != "failed" {
		t.Fatal("failed Runtime did not keep failed Task")
	}
	for _, role := range []stageplan.Role{stageplan.Design, stageplan.Implementation} {
		in, req = workflowStart(t, s, task, role, "retry-"+string(role))
		// Existing Task readiness rejects terminal failures before workflow gating.
		if _, e = s.StartReservedOnce(in, req); !errors.Is(e, ErrConflict) {
			t.Fatal("failed design was replayed", e)
		}
	}
	var count int
	if e = s.db.QueryRow("SELECT COUNT(*) FROM stage_runs WHERE task_id=?", task.ID).Scan(&count); e != nil || count != 1 {
		t.Fatal("failure retry created a new run", e)
	}
	if _, e = s.ApproveWorkflowDesign(task.ID, TaskVersion{1, task.Generation, task.State}, "fake-design", "fake-acceptance"); !errors.Is(e, ErrConflict) {
		t.Fatal("model output approved failed design", e)
	}
}

func TestWorkflowDesignCanBeFrozenAfterIdlePauseContinueWithCurrentCondition(t *testing.T) {
	s, _ := openFixture(t)
	task := workflowTask(t, s, "change")
	in, req := workflowStart(t, s, task, stageplan.Design, "design-before-pause")
	r, e := s.StartReservedOnce(in, req)
	if e != nil {
		t.Fatal(e)
	}
	finishWorkflowRun(t, s, r.Run)
	paused, e := s.PauseTask(task.ID, TaskVersion{1, 1, "ready"}, "")
	if e != nil || paused.Task.State != "paused" {
		t.Fatal(e)
	}
	continued, e := s.ContinueTask(task.ID, TaskVersion{1, paused.Task.Generation, "paused"})
	if e != nil {
		t.Fatal(e)
	}
	doc := workflow.DesignDocument{Goal: task.Goal, Scope: []string{"src"}, Constraints: []string{"preserve API"}, Interfaces: []string{"typed API"}, Acceptance: []string{"tests pass"}}
	if _, e = s.SaveWorkflowDesign(task.ID, TaskVersion{1, 1, "ready"}, r.Run.ID, doc); !errors.Is(e, ErrConflict) {
		t.Fatal("stale condition accepted", e)
	}
	v, e := s.SaveWorkflowDesign(task.ID, TaskVersion{1, continued.Task.Generation, "ready"}, r.Run.ID, doc)
	if e != nil || v.Design.Generation != r.Run.Generation || v.Blocker != "approval_required" {
		t.Fatal("idle pause lost completed original design", e)
	}
}

func workflowStart(t *testing.T, s *Store, task Task, role stageplan.Role, key string) (StartRequest, ReservationRequest) {
	t.Helper()
	p, e := s.Plan(task.ID, task.PlanRevision)
	if e != nil {
		t.Fatal(e)
	}
	gen := task.Generation
	return StartRequest{TaskID: task.ID, Role: role, PlanRevision: task.PlanRevision, Owner: "fixture-worker", TTL: time.Minute, Target: *p.Bindings[role].Target, IdempotencyKey: key, ExpectedGeneration: &gen}, ReservationRequest{PoolKey: "fixture-flow-pool", GlobalLimit: 2, AdmissionHash: hash([]byte("fixture-proof"))}
}
func TestWorkflowRequiresFrozenHumanDesignApprovalBeforeAnyImplementationIntent(t *testing.T) {
	s, root := openFixture(t)
	task := workflowTask(t, s, "change")
	in, req := workflowStart(t, s, task, stageplan.Implementation, "early-write")
	if _, e := s.StartReservedOnce(in, req); !errors.Is(e, ErrWorkflowGate) {
		t.Fatal("implementation bypassed design", e)
	}
	current, _ := s.Task(task.ID)
	if current.Generation != 0 || current.State != "ready" {
		t.Fatal("denied start wrote intent")
	}
	in, req = workflowStart(t, s, task, stageplan.Design, "design-run")
	got, e := s.StartReservedOnce(in, req)
	if e != nil || !got.Created {
		t.Fatal(e)
	}
	r := got.Run
	if e = s.ConfirmStarted(r.ID, r.Generation, r.Owner, "fixture-design-session"); e != nil {
		t.Fatal(e)
	}
	if e = s.Finish(r.ID, r.Generation, r.Owner, "succeeded"); e != nil {
		t.Fatal(e)
	}
	doc := workflow.DesignDocument{Goal: task.Goal, Scope: []string{"src"}, Constraints: []string{"preserve API"}, Interfaces: []string{"typed API"}, Acceptance: []string{"real tests pass"}}
	version := TaskVersion{1, 1, "ready"}
	if _, e = s.SaveWorkflowDesign(task.ID, version, r.ID, doc); !errors.Is(e, ErrWorkflowGate) {
		t.Fatal("missing stop/release accepted", e)
	}
	if e = s.ReleaseReserved(r.ID, r.Generation, hash([]byte("fixture-stop")), true); e != nil {
		t.Fatal(e)
	}
	view, e := s.SaveWorkflowDesign(task.ID, version, r.ID, doc)
	if e != nil || view.Blocker != "approval_required" {
		t.Fatal(e, view)
	}
	current, _ = s.Task(task.ID)
	in, req = workflowStart(t, s, current, stageplan.Implementation, "still-unapproved")
	if _, e = s.StartReservedOnce(in, req); !errors.Is(e, ErrWorkflowGate) {
		t.Fatal("model text granted write", e)
	}
	if _, e = s.ApproveWorkflowDesign(task.ID, version, view.Design.Snapshot.Hash, "wrong-criteria"); !errors.Is(e, ErrConflict) {
		t.Fatal("wrong acceptance approval accepted", e)
	}
	approved, e := s.ApproveWorkflowDesign(task.ID, version, view.Design.Snapshot.Hash, view.Design.Snapshot.AcceptanceHash)
	if e != nil || approved.Next != stageplan.Implementation || approved.Blocker != "" {
		t.Fatal(e, approved)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	stored, e := s.Workflow(task.ID)
	if e != nil || !reflect.DeepEqual(stored, approved) {
		t.Fatal("frozen approval did not persist", e)
	}
	got, e = s.StartReservedOnce(in, req)
	if e != nil || !got.Created || got.Run.Role != stageplan.Implementation {
		t.Fatal("approved stage refused", e)
	}
}

func completedWorkflowDesign(t *testing.T, s *Store) (Task, WorkflowView) {
	t.Helper()
	task := workflowTask(t, s, "change")
	in, req := workflowStart(t, s, task, stageplan.Design, "fixture-design")
	got, e := s.StartReservedOnce(in, req)
	if e != nil {
		t.Fatal(e)
	}
	r := got.Run
	finishWorkflowRun(t, s, r)
	task, e = s.Task(task.ID)
	if e != nil {
		t.Fatal(e)
	}
	doc := workflow.DesignDocument{Goal: task.Goal, Scope: []string{"src"}, Constraints: []string{"preserve API"}, Interfaces: []string{"typed API"}, Acceptance: []string{"real tests pass"}}
	v, e := s.SaveWorkflowDesign(task.ID, TaskVersion{task.PlanRevision, task.Generation, task.State}, r.ID, doc)
	if e != nil {
		t.Fatal(e)
	}
	return task, v
}
func finishWorkflowRun(t *testing.T, s *Store, r StageRun) {
	t.Helper()
	if e := s.ConfirmStarted(r.ID, r.Generation, r.Owner, "fixture-flow-session"); e != nil {
		t.Fatal(e)
	}
	if e := s.Finish(r.ID, r.Generation, r.Owner, "succeeded"); e != nil {
		t.Fatal(e)
	}
	if e := s.ReleaseReserved(r.ID, r.Generation, hash([]byte("fixture-stop")), true); e != nil {
		t.Fatal(e)
	}
}
func TestWorkflowBlocksAllStartEntrypointsAndPreservesBudgetAndEvents(t *testing.T) {
	s, _ := openFixture(t)
	task, v := completedWorkflowDesign(t, s)
	before, _ := s.Events(task.ID, 0)
	budget, _ := s.Budget(task.ID)
	for _, entry := range []string{"once", "reserved", "intent"} {
		in, req := workflowStart(t, s, task, stageplan.Implementation, "unapproved-"+entry)
		var e error
		switch entry {
		case "once":
			_, e = s.StartReservedOnce(in, req)
		case "reserved":
			in.IdempotencyKey = ""
			_, e = s.StartReserved(in, req)
		case "intent":
			in.IdempotencyKey = ""
			_, e = s.StartIntent(in)
		}
		if !errors.Is(e, ErrWorkflowGate) {
			t.Fatal("bypass", entry, e)
		}
	}
	current, _ := s.Task(task.ID)
	after, _ := s.Events(task.ID, 0)
	currentBudget, _ := s.Budget(task.ID)
	view, e := s.Workflow(task.ID)
	if e != nil || !reflect.DeepEqual(view, v) || !reflect.DeepEqual(task, current) || !reflect.DeepEqual(budget, currentBudget) || !reflect.DeepEqual(before, after) {
		t.Fatal("denied intent changed durable state", e)
	}
}
func TestWorkflowFiniteFiveRolesCannotSkipRepeatOrDeclareWholeTaskAcceptance(t *testing.T) {
	s, _ := openFixture(t)
	task, v := completedWorkflowDesign(t, s)
	if _, e := s.ApproveWorkflowDesign(task.ID, TaskVersion{1, 1, "ready"}, v.Design.Snapshot.Hash, v.Design.Snapshot.AcceptanceHash); e != nil {
		t.Fatal(e)
	}
	for _, role := range []stageplan.Role{stageplan.Implementation, stageplan.Testing, stageplan.Review, stageplan.Acceptance} {
		in, req := workflowStart(t, s, task, stageplan.Design, "repeat-design")
		if _, e := s.StartReservedOnce(in, req); !errors.Is(e, ErrWorkflowGate) {
			t.Fatal("already finished design reran", e)
		}
		if role != stageplan.Acceptance {
			in, req = workflowStart(t, s, task, stageplan.Acceptance, "skip-acceptance")
			if _, e := s.StartReservedOnce(in, req); !errors.Is(e, ErrWorkflowGate) {
				t.Fatal("phase skipped", e)
			}
		}
		in, req = workflowStart(t, s, task, role, "stage-"+string(role))
		got, e := s.StartReservedOnce(in, req)
		if e != nil || !got.Created || got.Run.Attempt != 1 || got.Run.Role != role {
			t.Fatal(e)
		}
		finishWorkflowRun(t, s, got.Run)
		task, e = s.Task(task.ID)
		if e != nil {
			t.Fatal(e)
		}
		// Same original intent remains a read receipt, not another stage launch.
		old, e := s.StartReservedOnce(in, req)
		if e != nil || old.Created || old.Run.ID != got.Run.ID {
			t.Fatal("replayed finished stage", e)
		}
	}
	view, e := s.Workflow(task.ID)
	if e != nil || view.Next != "" || view.Blocker != "workflow_complete" || task.Generation != 5 || task.State != "ready" {
		t.Fatal("phase result became whole-task acceptance", e, view)
	}
	in, req := workflowStart(t, s, task, stageplan.Implementation, "extra-stage")
	if _, e = s.StartReservedOnce(in, req); !errors.Is(e, ErrWorkflowGate) {
		t.Fatal("unbounded loop", e)
	}
	b, e := s.Budget(task.ID)
	if e != nil || b.UsedCalls != 0 || b.UsedReworks != 0 || b.MaxCalls != 20 {
		t.Fatal("phase renamed budget", e)
	}
}
func TestWorkflowInvestigateAndStandaloneReviewNeverStartUnusedRoles(t *testing.T) {
	for _, kind := range []workflow.Kind{"investigate", "review"} {
		t.Run(string(kind), func(t *testing.T) {
			s, _ := openFixture(t)
			task := workflowTask(t, s, kind)
			role := stageplan.Design
			if kind == "review" {
				role = stageplan.Review
			}
			in, req := workflowStart(t, s, task, role, "only-stage")
			got, e := s.StartReservedOnce(in, req)
			if e != nil {
				t.Fatal(e)
			}
			finishWorkflowRun(t, s, got.Run)
			task, e = s.Task(task.ID)
			if e != nil {
				t.Fatal(e)
			}
			v, e := s.Workflow(task.ID)
			if e != nil {
				t.Fatal(e)
			}
			if kind == "investigate" {
				if v.Blocker != "design_required" {
					t.Fatal("missing planning output ignored")
				}
				v, e = s.SaveWorkflowDesign(task.ID, TaskVersion{1, 1, "ready"}, got.Run.ID, workflow.DesignDocument{Goal: task.Goal, Scope: []string{"."}, Constraints: []string{"read only"}, Interfaces: []string{"existing APIs"}, Acceptance: []string{"design reviewed"}})
				if e != nil {
					t.Fatal(e)
				}
			}
			if v.Blocker != "workflow_complete" || v.Next != "" || v.Approval != nil {
				t.Fatal("unused write approval/role required")
			}
			in.PlanRevision = task.PlanRevision
			*in.ExpectedGeneration = task.Generation
			in.IdempotencyKey = "extra-role"
			in.Role = stageplan.Implementation
			if _, e = s.StartReservedOnce(in, req); !errors.Is(e, ErrWorkflowGate) {
				t.Fatal("unnecessary implementation started", e)
			}
		})
	}
}
func TestWorkflowFrozenDesignRejectsReplacementStaleConditionAndMutableRead(t *testing.T) {
	s, _ := openFixture(t)
	task, v := completedWorkflowDesign(t, s)
	original := v.Design.Snapshot.Document
	original.Interfaces = []string{"new unapproved interface"}
	if _, e := s.SaveWorkflowDesign(task.ID, TaskVersion{1, 1, "ready"}, v.Design.RunID, original); !errors.Is(e, ErrConflict) {
		t.Fatal("design overwritten", e)
	}
	if _, e := s.ApproveWorkflowDesign(task.ID, TaskVersion{1, 0, "ready"}, v.Design.Snapshot.Hash, v.Design.Snapshot.AcceptanceHash); !errors.Is(e, ErrConflict) {
		t.Fatal("stale approval accepted", e)
	}
	v.Design.Snapshot.Document.Scope[0] = "outside"
	actual, e := s.Workflow(task.ID)
	if e != nil || actual.Design.Snapshot.Document.Scope[0] != "src" {
		t.Fatal("mutable read changed scope", e)
	}
}
func TestWorkflowNewPlanRequiresNewApprovalAndKeepsOldDecision(t *testing.T) {
	s, _ := openFixture(t)
	task, v := completedWorkflowDesign(t, s)
	approved, e := s.ApproveWorkflowDesign(task.ID, TaskVersion{1, 1, "ready"}, v.Design.Snapshot.Hash, v.Design.Snapshot.AcceptanceHash)
	if e != nil {
		t.Fatal(e)
	}
	// A new compiler revision with unchanged started role still invalidates the
	// old plan-bound approval; unstarted model edits follow the same path.
	layer := stageplan.Layer{Roles: map[stageplan.Role]stageplan.Binding{}}
	route := stageplan.Route{ID: "fixture-route", Revision: 1, Model: "fixture-model-a", Account: "fixture-account-a", Workspace: "fixture-workspace", CredentialIdentity: "fixture-credential-a", RuntimeVersion: "fixture-runtime-v1", BillingPath: "fixture-subscription", BillingKnown: true, Admitted: true, Efforts: []string{"medium"}, Capabilities: []string{"text"}, LockEnforcement: stageplan.ControlledCalls}
	for _, role := range v.Definition.RequiredRoles {
		layer.Roles[role] = stageplan.Binding{Mode: stageplan.Locked, Route: &routeRefForWorkflow, Model: route.Model, Effort: &stageplan.EffortSelection{Mode: stageplan.EffortExplicit, Value: "medium"}}
	}
	p, e := stageplan.Compile(2, v.Definition.RequiredRoles, layer, stageplan.Layer{}, stageplan.Layer{}, []stageplan.Route{route})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.RevisePlan(task.ID, 1, p); e != nil {
		t.Fatal(e)
	}
	task, e = s.Task(task.ID)
	if e != nil {
		t.Fatal(e)
	}
	current, e := s.Workflow(task.ID)
	if e != nil || current.Approval != nil || current.Blocker != "approval_required" {
		t.Fatal("new plan reused old approval", e)
	}
	in, req := workflowStart(t, s, task, stageplan.Implementation, "new-plan-unapproved")
	if _, e = s.StartReservedOnce(in, req); !errors.Is(e, ErrWorkflowGate) {
		t.Fatal(e)
	}
	current, e = s.ApproveWorkflowDesign(task.ID, TaskVersion{2, 1, "ready"}, v.Design.Snapshot.Hash, v.Design.Snapshot.AcceptanceHash)
	if e != nil || current.Approval.PlanHash != p.Hash || current.Approval.PlanHash == approved.Approval.PlanHash {
		t.Fatal(e)
	}
	var n int
	if e = s.db.QueryRow("SELECT COUNT(*) FROM workflow_approvals WHERE task_id=?", task.ID).Scan(&n); e != nil || n != 2 {
		t.Fatal("historical decision rewritten", e)
	}
	if _, e = s.StartReservedOnce(in, req); e != nil {
		t.Fatal("reapproved new plan blocked", e)
	}
}
func TestWorkflowApprovalFailureRollsBackAndConcurrentRetriesHaveOneDecision(t *testing.T) {
	s, _ := openFixture(t)
	task, v := completedWorkflowDesign(t, s)
	before, _ := s.Events(task.ID, 0)
	if _, e := s.db.Exec("CREATE TRIGGER fixture_design_event_fail BEFORE INSERT ON events WHEN NEW.kind='design_approved' BEGIN SELECT RAISE(ABORT,'fixture'); END"); e != nil {
		t.Fatal(e)
	}
	approve := func() (WorkflowView, error) {
		return s.ApproveWorkflowDesign(task.ID, TaskVersion{1, 1, "ready"}, v.Design.Snapshot.Hash, v.Design.Snapshot.AcceptanceHash)
	}
	if _, e := approve(); e == nil {
		t.Fatal("failed transaction accepted")
	}
	after, _ := s.Events(task.ID, 0)
	actual, e := s.Workflow(task.ID)
	if e != nil || !reflect.DeepEqual(actual, v) || !reflect.DeepEqual(before, after) {
		t.Fatal("partial approval survived", e)
	}
	if _, e = s.db.Exec("DROP TRIGGER fixture_design_event_fail"); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for n := 0; n < 12; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := approve(); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	events, e := s.Events(task.ID, 0)
	if e != nil {
		t.Fatal(e)
	}
	count := 0
	for _, event := range events {
		if event.Kind == "design_approved" {
			count++
		}
	}
	if count != 1 {
		t.Fatal("duplicate decisions", count)
	}
	var rows int
	if e = s.db.QueryRow("SELECT COUNT(*) FROM workflow_approvals").Scan(&rows); e != nil || rows != 1 {
		t.Fatal("duplicate approval rows", e)
	}
}
func TestWorkflowAttachmentCannotRewritePreparedOrStartedManualRequest(t *testing.T) {
	s, _ := openFixture(t)
	in, req := onceFixture(t, s)
	ident := identityOf(in)
	if _, e := s.PrepareStart(in.IdempotencyKey, ident); e != nil {
		t.Fatal(e)
	}
	if _, e := s.AttachWorkflow(in.TaskID, TaskVersion{1, 0, "ready"}, "investigate"); !errors.Is(e, ErrConflict) {
		t.Fatal("pending original request contract changed", e)
	}
	got, e := s.StartReservedOnce(in, req)
	if e != nil || !got.Created {
		t.Fatal("manual request was broken", e)
	}
	finishWorkflowRun(t, s, got.Run)
	if _, e = s.AttachWorkflow(in.TaskID, TaskVersion{1, 1, "ready"}, "investigate"); !errors.Is(e, ErrConflict) {
		t.Fatal("old runs silently reclassified", e)
	}
	if _, e = s.Workflow(in.TaskID); !errors.Is(e, ErrNotFound) {
		t.Fatal("legacy workflow guessed", e)
	}
}
func TestWorkflowDamagedFrozenDesignCannotAuthorizeNewIntent(t *testing.T) {
	s, _ := openFixture(t)
	task, v := completedWorkflowDesign(t, s)
	if _, e := s.db.Exec("UPDATE workflow_designs SET design_json='{}'"); e == nil {
		t.Fatal("immutable design could change")
	}
	if _, e := s.db.Exec("DROP TRIGGER immutable_workflow_designs"); e != nil {
		t.Fatal(e)
	}
	v.Design.Snapshot.Document.Acceptance = []string{"model says passed"}
	raw, e := json.Marshal(v.Design)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec("UPDATE workflow_designs SET design_json=?,design_hash=? WHERE task_id=?", string(raw), hash(raw), task.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Workflow(task.ID); !errors.Is(e, ErrWorkflowGate) {
		t.Fatal("rewritten checksum hid changed criteria", e)
	}
	in, req := workflowStart(t, s, task, stageplan.Implementation, "damaged-design")
	if _, e = s.StartReservedOnce(in, req); !errors.Is(e, ErrWorkflowGate) {
		t.Fatal("corrupted contract started", e)
	}
}

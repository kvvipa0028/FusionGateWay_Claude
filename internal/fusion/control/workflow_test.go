package control

import (
	"context"
	"errors"
	"testing"

	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
	"github.com/yetone/magpie/internal/fusion/workflow"
)

func TestControllerWorkflowDeniesUnapprovedStageBeforeResolverAndStartsApprovedBinding(t *testing.T) {
	f := newControlFixture(t)
	original, e := f.st.Task(f.in.TaskID)
	if e != nil {
		t.Fatal(e)
	}
	p, e := f.st.Plan(original.ID, 1)
	if e != nil {
		t.Fatal(e)
	}
	inspect, e := f.config.Scheduler.Inspect(context.Background(), original, stageplan.Design, *p.Bindings[stageplan.Design].Target)
	if e != nil {
		t.Fatal(e)
	}
	d, e := workflow.DefinitionFor("change")
	if e != nil {
		t.Fatal(e)
	}
	layer := stageplan.Layer{Roles: map[stageplan.Role]stageplan.Binding{}}
	for _, role := range d.RequiredRoles {
		layer.Roles[role] = stageplan.Binding{Mode: stageplan.Locked, Route: &stageplan.RouteRef{ID: inspect.Route.ID, Revision: inspect.Route.Revision}, Model: inspect.Route.Model, Effort: &stageplan.EffortSelection{Mode: stageplan.EffortExplicit, Value: "high"}}
	}
	p, e = stageplan.Compile(1, d.RequiredRoles, layer, stageplan.Layer{}, stageplan.Layer{}, []stageplan.Route{inspect.Route})
	if e != nil {
		t.Fatal(e)
	}
	task, e := f.st.Create("controller-workflow", store.CreateRequest{ProjectID: original.ProjectID, Goal: original.Goal, Plan: p, Budget: &store.Budget{MaxCalls: 3}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.st.AttachWorkflow(task.ID, store.TaskVersion{PlanRevision: 1, Generation: 0, State: "ready"}, "change"); e != nil {
		t.Fatal(e)
	}
	c := f.controller(t)
	in := store.StartIdentity{TaskID: task.ID, Role: stageplan.Implementation, PlanRevision: 1, Generation: 0}
	if _, e = c.Start(context.Background(), "denied-before-design", in); !errors.Is(e, store.ErrWorkflowGate) {
		t.Fatal(e)
	}
	if f.resolved.Load() != 0 || f.started.Load() != 0 {
		t.Fatal("unapproved workflow reached resolver or Runtime")
	}
	in.Role = stageplan.Design
	run, e := c.Start(context.Background(), "workflow-design", in)
	if e != nil || !run.Created {
		t.Fatal(e)
	}
	close(f.finish)
	completion := waitControl(t, c, run.Run.ID)
	if !completion.StoppedVerified || !completion.Released || completion.State != "succeeded" {
		t.Fatal("design was not really waited/released by Controller")
	}
	doc := workflow.DesignDocument{Goal: task.Goal, Scope: []string{"src"}, Constraints: []string{"preserve API"}, Interfaces: []string{"typed API"}, Acceptance: []string{"real tests pass"}}
	view, e := f.st.SaveWorkflowDesign(task.ID, store.TaskVersion{PlanRevision: 1, Generation: 1, State: "ready"}, run.Run.ID, doc)
	if e != nil {
		t.Fatal(e)
	}
	in.Role = stageplan.Implementation
	in.Generation = 1
	if _, e = c.Start(context.Background(), "denied-before-approval", in); !errors.Is(e, store.ErrWorkflowGate) {
		t.Fatal(e)
	}
	if f.resolved.Load() != 1 || f.started.Load() != 1 {
		t.Fatal("unapproved implementation reached backend")
	}
	if _, e = f.st.ApproveWorkflowDesign(task.ID, store.TaskVersion{PlanRevision: 1, Generation: 1, State: "ready"}, view.Design.Snapshot.Hash, view.Design.Snapshot.AcceptanceHash); e != nil {
		t.Fatal(e)
	}
	run, e = c.Start(context.Background(), "approved-implementation", in)
	if e != nil || !run.Created || run.Run.Role != stageplan.Implementation || run.Run.Target.ResolvedModel != "glm-5.3" {
		t.Fatal("frozen binding changed", e)
	}
	completion = waitControl(t, c, run.Run.ID)
	if !completion.Released || completion.State != "succeeded" {
		t.Fatal("approved execution did not complete")
	}
	if f.resolved.Load() != 2 || f.started.Load() != 2 {
		t.Fatal("duplicate dispatch")
	}
}

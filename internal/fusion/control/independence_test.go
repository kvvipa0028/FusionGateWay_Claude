package control

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

func TestControllerProjectIndependenceBeforeRuntimeAndInspection(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "default_same_model", true: "required_distinct_model"}[enabled], func(t *testing.T) {
			f := newControlFixture(t)
			route := stageplan.Route{ID: "a", Revision: 1, Model: "shared-model", Account: "account", Workspace: "workspace", CredentialIdentity: "credential", RuntimeVersion: "fixture-version", BillingPath: "coding_plan", BillingKnown: true, Admitted: true, NoEffort: true, LockEnforcement: stageplan.ControlledCalls}
			other := route
			other.ID = "b"
			other.Model = "other-model"
			eff := &stageplan.EffortSelection{Mode: stageplan.EffortNone}
			layer := stageplan.Layer{Roles: map[stageplan.Role]stageplan.Binding{stageplan.Design: {Mode: stageplan.Locked, Route: &stageplan.RouteRef{ID: "a", Revision: 1}, Model: route.Model, Effort: eff}, stageplan.Acceptance: {Mode: stageplan.Auto, Candidates: []stageplan.Candidate{{Route: stageplan.RouteRef{ID: "a", Revision: 1}, Model: route.Model, Effort: eff}, {Route: stageplan.RouteRef{ID: "b", Revision: 1}, Model: other.Model, Effort: eff}}}}}
			var pairs []stageplan.RolePair
			if enabled {
				pairs = []stageplan.RolePair{{First: stageplan.Design, Second: stageplan.Acceptance}}
			}
			p, e := stageplan.CompileWithIndependence(1, []stageplan.Role{stageplan.Design, stageplan.Acceptance}, layer, stageplan.Layer{}, stageplan.Layer{}, []stageplan.Route{route, other}, pairs)
			if e != nil {
				t.Fatal(e)
			}
			task, e := f.st.Create("independent-control", store.CreateRequest{ProjectID: "fixture-project", Goal: "fixture", Plan: p})
			if e != nil {
				t.Fatal(e)
			}
			in := store.StartIdentity{TaskID: task.ID, Role: stageplan.Acceptance, PlanRevision: 1, Generation: 0}
			resolves, inspections := 0, 0
			f.config.SelectAuto = func(context.Context, store.Task, stageplan.Role, stageplan.FrozenBinding) (stageplan.ExecutionTarget, error) {
				return p.Bindings[stageplan.Acceptance].Candidates[0], nil
			}
			f.config.Resolve = func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (Launch, error) {
				resolves++
				return Launch{}, ErrUnsupported
			}
			inspect := f.config.Scheduler.Inspect
			f.config.Scheduler.Inspect = func(ctx context.Context, task store.Task, role stageplan.Role, target stageplan.ExecutionTarget) (policy.Inspection, error) {
				inspections++
				return inspect(ctx, task, role, target)
			}
			c := f.controller(t)
			_, e = c.Start(context.Background(), "independence-start", in)
			if enabled && (!errors.Is(e, store.ErrIndependence) || resolves != 0) || !enabled && (resolves != 1 || !errors.Is(e, ErrUnsupported)) || inspections != 0 {
				t.Fatal("policy changed default or ran preflight", e, resolves, inspections)
			}
			if enabled {
				gen := int64(0)
				_, e = f.config.Scheduler.PrepareOnce(context.Background(), store.StartRequest{TaskID: task.ID, Role: in.Role, PlanRevision: 1, ExpectedGeneration: &gen, Owner: "fixture-owner", TTL: time.Minute, Target: p.Bindings[in.Role].Candidates[0], IdempotencyKey: "scheduler-independence"})
				if !errors.Is(e, store.ErrIndependence) || inspections != 0 {
					t.Fatal("scheduler inspected conflict", e, inspections)
				}
			}
			events, _ := f.st.Events(task.ID, 0)
			if len(events) != 1 {
				t.Fatal("preflight wrote an intent")
			}
		})
	}
}

package store

import (
	"errors"
	"testing"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

func changedDesign(t *testing.T) stageplan.Snapshot {
	t.Helper()
	p := plan(t, 2)
	target := *p.Bindings[stageplan.Design].Target
	// Compile a different, otherwise valid route rather than forging a hash.
	e := "medium"
	r := stageplan.Route{ID: "fixture-route-b", Revision: 1, Model: "fixture-model-b", Account: target.Account, Workspace: target.Workspace, CredentialIdentity: target.CredentialIdentity, RuntimeVersion: target.RuntimeVersion, BillingPath: target.BillingPath, BillingKnown: true, Admitted: true, Efforts: []string{e}, DefaultEffort: &e, LockEnforcement: stageplan.ControlledCalls}
	b := stageplan.Binding{Mode: stageplan.Locked, Route: &stageplan.RouteRef{ID: r.ID, Revision: 1}, Model: r.Model, Effort: &stageplan.EffortSelection{Mode: stageplan.EffortDefault}}
	p, err := stageplan.Compile(2, []stageplan.Role{stageplan.Design}, stageplan.Layer{Roles: map[stageplan.Role]stageplan.Binding{stageplan.Design: b}}, stageplan.Layer{}, stageplan.Layer{}, []stageplan.Route{r})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestOrdinaryRevisionCannotRewriteAlreadyFinishedRole(t *testing.T) {
	s, _ := openFixture(t)
	task := create(t, s)
	run := start(t, s, task)
	if e := s.ConfirmStarted(run.ID, run.Generation, run.Owner, "fixture-session"); e != nil {
		t.Fatal(e)
	}
	if e := s.Finish(run.ID, run.Generation, run.Owner, "succeeded"); e != nil {
		t.Fatal(e)
	}
	if e := s.RevisePlan(task.ID, 1, changedDesign(t)); !errors.Is(e, ErrConflict) {
		t.Fatal("completed stage binding rewritten", e)
	}
	got, e := s.Task(task.ID)
	if e != nil || got.PlanRevision != 1 {
		t.Fatal("rejected revision persisted")
	}
}
func TestRevisionValidationHasNoEventsOrSnapshotEffects(t *testing.T) {
	s, _ := openFixture(t)
	task := create(t, s)
	before, e := s.Events(task.ID, 0)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ValidateRevision(task.ID, 1, plan(t, 2)); e != nil {
		t.Fatal(e)
	}
	after, e := s.Events(task.ID, 0)
	if e != nil || len(after) != len(before) {
		t.Fatal("preview validation wrote events")
	}
	if _, e = s.Plan(task.ID, 2); !errors.Is(e, ErrNotFound) {
		t.Fatal("preview inserted snapshot", e)
	}
}

func TestRevisionEventFailureRollsBackSnapshotAndTaskVersion(t *testing.T) {
	s, _ := openFixture(t)
	task := create(t, s)
	if _, e := s.db.Exec(`CREATE TRIGGER reject_revision_event BEFORE INSERT ON events WHEN NEW.kind='plan_revised' BEGIN SELECT RAISE(ABORT,'fixture revision failure'); END`); e != nil {
		t.Fatal(e)
	}
	if e := s.RevisePlan(task.ID, 1, plan(t, 2)); e == nil {
		t.Fatal("event failure hidden")
	}
	got, e := s.Task(task.ID)
	if e != nil || got.PlanRevision != 1 {
		t.Fatal("revision failure changed task")
	}
	if _, e = s.Plan(task.ID, 2); !errors.Is(e, ErrNotFound) {
		t.Fatal("revision failure inserted snapshot", e)
	}
	events, e := s.Events(task.ID, 0)
	if e != nil || len(events) != 1 {
		t.Fatal("revision failure wrote event")
	}
}

func TestOrdinaryRevisionCannotReplaceRequiredRoles(t *testing.T) {
	s, _ := openFixture(t)
	task := create(t, s)
	target := *plan(t, 1).Bindings[stageplan.Design].Target
	r := stageplan.Route{ID: target.Route.ID, Revision: target.Route.Revision, Model: target.ResolvedModel, Account: target.Account, Workspace: target.Workspace, CredentialIdentity: target.CredentialIdentity, RuntimeVersion: target.RuntimeVersion, BillingPath: target.BillingPath, BillingKnown: true, Admitted: true, NoEffort: true, LockEnforcement: stageplan.ControlledCalls}
	b := stageplan.Binding{Mode: stageplan.Locked, Route: &target.Route, Model: r.Model, Effort: &stageplan.EffortSelection{Mode: stageplan.EffortNone}}
	p, e := stageplan.Compile(2, []stageplan.Role{stageplan.Review}, stageplan.Layer{Roles: map[stageplan.Role]stageplan.Binding{stageplan.Review: b}}, stageplan.Layer{}, stageplan.Layer{}, []stageplan.Route{r})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.RevisePlan(task.ID, 1, p); !errors.Is(e, ErrConflict) {
		t.Fatal("required role changed", e)
	}
}

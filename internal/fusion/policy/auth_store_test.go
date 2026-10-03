package policy

import (
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStageScopeUsesPersistedRunAndRejectsCompletedGeneration(t *testing.T) {
	root := t.TempDir()
	os.Chmod(root, 0700)
	root, _ = filepath.EvalSymlinks(root)
	s, e := store.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	route := stageplan.Route{ID: "fixture-route", Revision: 1, Model: "fixture-model-a", Account: "fixture-account-a", Workspace: "fixture-workspace", CredentialIdentity: "fixture-credential-a", RuntimeVersion: "fixture-runtime-v1", BillingPath: "fixture-subscription", BillingKnown: true, Admitted: true, NoEffort: true, Capabilities: []string{"text"}, LockEnforcement: stageplan.ControlledCalls}
	snap, e := stageplan.Compile(1, []stageplan.Role{stageplan.Design}, stageplan.Layer{Roles: map[stageplan.Role]stageplan.Binding{stageplan.Design: {Mode: stageplan.Locked, Route: &stageplan.RouteRef{ID: route.ID, Revision: 1}, Model: route.Model, Effort: &stageplan.EffortSelection{Mode: stageplan.EffortNone}}}}, stageplan.Layer{}, stageplan.Layer{}, []stageplan.Route{route})
	if e != nil {
		t.Fatal(e)
	}
	task, e := s.Create("fixture-key", store.CreateRequest{ProjectID: "fixture-project", Goal: "fixture", Plan: snap})
	if e != nil {
		t.Fatal(e)
	}
	run, e := s.StartIntent(store.StartRequest{TaskID: task.ID, Role: stageplan.Design, PlanRevision: 1, Owner: "fixture-worker", TTL: time.Minute, Target: *snap.Bindings[stageplan.Design].Target})
	if e != nil {
		t.Fatal(e)
	}
	c := Claims{TaskID: task.ID, RunID: run.ID, Role: run.Role, Attempt: run.Attempt, PlanRevision: run.PlanRevision, Generation: run.Generation, ProjectID: task.ProjectID, Audience: ModelAudience}
	m := NewManager("fixture-management-secret", StoreValidator(s), nil)
	if _, e = m.Issue(c, time.Minute); e == nil {
		t.Fatal("model credential issued before startup confirmation")
	}
	events := c
	events.Audience = EventsAudience
	if _, e = m.Issue(events, time.Minute); e != nil {
		t.Fatal(e)
	}
	if e = s.ConfirmStarted(run.ID, run.Generation, run.Owner, "fixture-session"); e != nil {
		t.Fatal(e)
	}
	raw, e := m.Issue(c, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	for _, edit := range []func(*Claims){func(c *Claims) { c.ProjectID = "fixture-other" }, func(c *Claims) { c.TaskID = "fixture-other" }, func(c *Claims) { c.Generation++ }, func(c *Claims) { c.PlanRevision++ }, func(c *Claims) { c.Role = stageplan.Acceptance }, func(c *Claims) { c.Attempt++ }} {
		bad := c
		edit(&bad)
		if _, e = m.Issue(bad, time.Minute); e == nil {
			t.Fatal("scope unrelated to server run issued")
		}
	}
	if e = s.Finish(run.ID, run.Generation, run.Owner, "succeeded"); e != nil {
		t.Fatal(e)
	}
	if _, e = m.AuthenticateStage(raw, c); e == nil {
		t.Fatal("completed run accepted new request")
	}
	s.Close()
	if _, e = m.Issue(c, time.Minute); e == nil {
		t.Fatal("closed controller issued credentials")
	}
}

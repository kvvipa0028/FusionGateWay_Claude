package stageplan

import (
	"encoding/json"
	"testing"
)

func revisionFixture(t *testing.T) (Snapshot, Layer, []Route) {
	t.Helper()
	e := "medium"
	r := Route{ID: "fixture-a", Revision: 1, Model: "fixture-model-a", Account: "fixture-account", Workspace: "fixture-workspace", CredentialIdentity: "fixture-credential", RuntimeVersion: "fixture-runtime", BillingPath: "fixture-subscription", BillingKnown: true, Admitted: true, Efforts: []string{e}, DefaultEffort: &e, LockEnforcement: ControlledCalls}
	b := Binding{Mode: Locked, Route: &RouteRef{ID: r.ID, Revision: 1}, Model: r.Model, Effort: &EffortSelection{Mode: EffortDefault}}
	global := Layer{Roles: map[Role]Binding{Design: b, Review: b}}
	routes := []Route{r}
	r.ID = "fixture-b"
	r.Model = "fixture-model-b"
	routes = append(routes, r)
	p, err := Compile(1, []Role{Design, Review}, global, Layer{}, Layer{}, routes)
	if err != nil {
		t.Fatal(err)
	}
	return p, global, routes
}
func TestCompileRevisionChangesOnlyExplicitRolesAndCopiesSnapshot(t *testing.T) {
	old, g, r := revisionFixture(t)
	raw, _ := json.Marshal(old)
	changes := Layer{Roles: map[Role]Binding{Review: {Mode: Locked, Route: &RouteRef{ID: r[1].ID, Revision: 1}, Model: r[1].Model, Effort: &EffortSelection{Mode: EffortDefault}}}}
	next, e := CompileRevision(old, changes, g, Layer{}, r)
	if e != nil {
		t.Fatal(e)
	}
	if next.Revision != 2 || next.Bindings[Review].Target.ResolvedModel != "fixture-model-b" || next.Bindings[Design].Target.ResolvedModel != "fixture-model-a" || VerifySnapshot(next) != nil {
		t.Fatal("revision did not preserve unrelated roles")
	}
	next.Bindings[Design].Target.ResolvedModel = "fixture-mutated"
	after, _ := json.Marshal(old)
	if string(raw) != string(after) {
		t.Fatal("revision shares old pointers")
	}
}
func TestRevisionDoesNotReResolveUnchangedRetiredRoute(t *testing.T) {
	old, g, r := revisionFixture(t)
	r[0].Admitted = false
	changes := Layer{Roles: map[Role]Binding{Review: {Mode: Locked, Route: &RouteRef{ID: r[1].ID, Revision: 1}, Model: r[1].Model, Effort: &EffortSelection{Mode: EffortDefault}}}}
	next, e := CompileRevision(old, changes, g, Layer{}, r)
	if e != nil || next.Bindings[Design].Target.Route.ID != "fixture-a" {
		t.Fatal("unchanged historical binding reinterpreted", e)
	}
	// Preserving metadata grants no live execution admission for the old route.
	if next.Bindings[Review].Target.Route.ID != "fixture-b" {
		t.Fatal("changed role bypassed live registry")
	}
}
func TestRevisionInheritUsesCurrentDefaultsOnlyForSelectedRole(t *testing.T) {
	old, g, r := revisionFixture(t)
	b := g.Roles[Review]
	b.Model = r[1].Model
	b.Route = &RouteRef{ID: r[1].ID, Revision: 1}
	g.Roles[Review] = b
	next, e := CompileRevision(old, Layer{Roles: map[Role]Binding{Review: {Mode: Inherit}}}, g, Layer{}, r)
	if e != nil || next.Bindings[Review].Target.ResolvedModel != "fixture-model-b" || next.Bindings[Design].Target.ResolvedModel != "fixture-model-a" {
		t.Fatal("inherit crossed unaffected role", e)
	}
}
func TestRevisionCannotAddDropOrIgnoreRequiredStages(t *testing.T) {
	old, g, r := revisionFixture(t)
	for _, changes := range []Layer{{}, {Roles: map[Role]Binding{Testing: {Mode: Inherit}}}} {
		if _, e := CompileRevision(old, changes, g, Layer{}, r); e == nil {
			t.Fatal("empty or unrelated stage revision accepted")
		}
	}
	old.Hash = "fixture-invalid"
	if _, e := CompileRevision(old, Layer{Roles: map[Role]Binding{Review: {Mode: Inherit}}}, g, Layer{}, r); e == nil {
		t.Fatal("corrupt old snapshot accepted")
	}
}

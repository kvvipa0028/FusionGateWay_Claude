package stageplan

import (
	"encoding/json"
	"strings"
	"testing"
)

func fixtureRoute(id, model string, rev int64) Route {
	def := "medium"
	return Route{ID: id, Revision: rev, Model: model, Account: "fixture-account-" + id, Workspace: "fixture-workspace", CredentialIdentity: "fixture-credential-" + id, RuntimeVersion: "fixture-runtime-v1", PluginVersion: nil, BillingPath: "fixture-subscription", BillingKnown: true, Admitted: true, Efforts: []string{"low", "medium", "high"}, DefaultEffort: &def, Capabilities: []string{"text", "tools", "read", "write"}, LockEnforcement: ControlledCalls}
}
func locked(id, model string, rev int64) Binding {
	return Binding{Mode: Locked, Route: &RouteRef{ID: id, Revision: rev}, Model: model, Effort: &EffortSelection{Mode: EffortExplicit, Value: "high"}}
}
func compileOne(t *testing.T, b Binding, r Route) Snapshot {
	t.Helper()
	s, e := Compile(1, []Role{Design}, Layer{Roles: map[Role]Binding{Design: b}}, Layer{}, Layer{}, []Route{r})
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestWholeBindingPrecedenceAndGroupExpansion(t *testing.T) {
	a, b, c := fixtureRoute("a", "fixture-model-a", 1), fixtureRoute("b", "fixture-model-b", 2), fixtureRoute("c", "fixture-model-c", 3)
	global := Layer{Groups: map[Group]Binding{DesignPlanning: locked("a", a.Model, 1), ImplementationTesting: locked("a", a.Model, 1), ReviewAcceptance: locked("a", a.Model, 1)}}
	project := Layer{Groups: map[Group]Binding{ImplementationTesting: locked("b", b.Model, 2)}, Roles: map[Role]Binding{Testing: locked("c", c.Model, 3)}}
	task := Layer{Roles: map[Role]Binding{Implementation: locked("c", c.Model, 3), Testing: {Mode: Inherit}}}
	s, e := Compile(7, AllRoles(), global, project, task, []Route{a, b, c})
	if e != nil {
		t.Fatal(e)
	}
	for role, want := range map[Role]string{Design: "a", Implementation: "c", Testing: "c", Review: "a", Acceptance: "a"} {
		got := s.Bindings[role]
		if got.Target.Route.ID != want || got.Target.Account != "fixture-account-"+want {
			t.Fatalf("%s mixed binding: %+v", role, got)
		}
	}
	if s.Bindings[Testing].Source != "project" || s.Bindings[Implementation].Source != "task" {
		t.Fatal("wrong provenance")
	}
}
func TestTaskBindingDoesNotBorrowModelOrEffort(t *testing.T) {
	r := fixtureRoute("a", "fixture-model-a", 1)
	task := locked("a", r.Model, 1)
	task.Effort = nil
	_, e := Compile(1, []Role{Design}, Layer{Roles: map[Role]Binding{Design: locked("a", r.Model, 1)}}, Layer{}, Layer{Roles: map[Role]Binding{Design: task}}, []Route{r})
	if e == nil {
		t.Fatal("task borrowed global effort")
	}
	task = locked("a", "", 1)
	_, e = Compile(1, []Role{Design}, Layer{Roles: map[Role]Binding{Design: locked("a", r.Model, 1)}}, Layer{}, Layer{Roles: map[Role]Binding{Design: task}}, []Route{r})
	if e == nil {
		t.Fatal("task borrowed global model")
	}
}
func TestExactRevisionModelIdentityAndAdmission(t *testing.T) {
	r := fixtureRoute("a", "fixture-model-a", 1)
	for _, edit := range []func(*Route){func(r *Route) { r.Revision = 2 }, func(r *Route) { r.Model = "fixture-model-b" }, func(r *Route) { r.Account = "" }, func(r *Route) { r.Workspace = "unknown" }, func(r *Route) { r.CredentialIdentity = "" }, func(r *Route) { r.RuntimeVersion = "" }, func(r *Route) { r.BillingKnown = false }, func(r *Route) { r.BillingPath = "unknown" }, func(r *Route) { r.Admitted = false }} {
		copy := r
		edit(&copy)
		if _, e := Compile(1, []Role{Design}, Layer{Roles: map[Role]Binding{Design: locked("a", r.Model, 1)}}, Layer{}, Layer{}, []Route{copy}); e == nil {
			t.Fatalf("invalid route admitted: %+v", copy)
		}
	}
}
func TestMissingAndUnresolvedRolesAreRejected(t *testing.T) {
	for _, required := range [][]Role{{}, {Design}, {Role("sixth")}, {Design, Design}} {
		_, e := Compile(1, required, Layer{Roles: map[Role]Binding{Design: {Mode: Inherit}}}, Layer{}, Layer{}, nil)
		if e == nil {
			t.Fatalf("missing role contract accepted: %v", required)
		}
	}
}
func TestUnknownRoleGroupAndIllegalBindings(t *testing.T) {
	r := fixtureRoute("a", "fixture-model-a", 1)
	cases := []Layer{{Roles: map[Role]Binding{Role("sixth"): locked("a", r.Model, 1)}}, {Groups: map[Group]Binding{Group("other"): locked("a", r.Model, 1)}}, {Roles: map[Role]Binding{Design: {Mode: Inherit, Model: r.Model}}}, {Roles: map[Role]Binding{Design: {Mode: Locked, Route: &RouteRef{ID: "a", Revision: 1}, Model: r.Model, Effort: &EffortSelection{Mode: EffortExplicit, Value: "high"}, Candidates: []Candidate{{Route: RouteRef{ID: "a", Revision: 1}, Model: r.Model}}}}}}
	for _, layer := range cases {
		if _, e := Compile(1, []Role{Design}, layer, Layer{}, Layer{}, []Route{r}); e == nil {
			t.Fatal("invalid binding accepted")
		}
	}
}
func TestEffortNullUnknownDefaultAndNone(t *testing.T) {
	r := fixtureRoute("a", "fixture-model-a", 1)
	for _, effort := range []*EffortSelection{nil, {Mode: EffortExplicit, Value: "ultra"}, {Mode: EffortSelectionMode("unknown")}, {Mode: EffortNone}, {Mode: EffortDefault, Value: "high"}} {
		b := locked("a", r.Model, 1)
		b.Effort = effort
		if _, e := Compile(1, []Role{Design}, Layer{Roles: map[Role]Binding{Design: b}}, Layer{}, Layer{}, []Route{r}); e == nil {
			t.Fatalf("effort accepted: %+v", effort)
		}
	}
	b := locked("a", r.Model, 1)
	b.Effort = &EffortSelection{Mode: EffortDefault}
	s := compileOne(t, b, r)
	if s.Bindings[Design].Target.Effort.Value == nil || *s.Bindings[Design].Target.Effort.Value != "medium" || s.Bindings[Design].Target.Effort.RequestedMode != EffortDefault {
		t.Fatal("verified default not frozen")
	}
	r.DefaultEffort = nil
	if _, e := Compile(1, []Role{Design}, Layer{Roles: map[Role]Binding{Design: b}}, Layer{}, Layer{}, []Route{r}); e == nil {
		t.Fatal("undisclosed default silently guessed")
	}
	r.NoEffort = true
	r.Efforts = nil
	b.Effort = &EffortSelection{Mode: EffortNone}
	s = compileOne(t, b, r)
	if s.Bindings[Design].Target.Effort.Value != nil {
		t.Fatal("none mapped to effort")
	}
}
func TestLockCoverageRequiresExplicitPrimaryOnlyAcceptance(t *testing.T) {
	r := fixtureRoute("a", "fixture-model-a", 1)
	r.LockEnforcement = PrimaryOnly
	b := locked("a", r.Model, 1)
	if _, e := Compile(1, []Role{Design}, Layer{Roles: map[Role]Binding{Design: b}}, Layer{}, Layer{}, []Route{r}); e == nil {
		t.Fatal("primary-only silently accepted")
	}
	b.AcceptPrimaryOnly = true
	compileOne(t, b, r)
	r.LockEnforcement = Unverified
	if _, e := Compile(1, []Role{Design}, Layer{Roles: map[Role]Binding{Design: b}}, Layer{}, Layer{}, []Route{r}); e == nil {
		t.Fatal("unverified coverage accepted")
	}
}
func TestCapabilitiesAndDuplicateRouteRevisions(t *testing.T) {
	r := fixtureRoute("a", "fixture-model-a", 1)
	b := locked("a", r.Model, 1)
	b.RequiredCapabilities = []string{"image"}
	if _, e := Compile(1, []Role{Design}, Layer{Roles: map[Role]Binding{Design: b}}, Layer{}, Layer{}, []Route{r}); e == nil {
		t.Fatal("missing capability accepted")
	}
	b.RequiredCapabilities = nil
	if _, e := Compile(1, []Role{Design}, Layer{Roles: map[Role]Binding{Design: b}}, Layer{}, Layer{}, []Route{r, r}); e == nil {
		t.Fatal("ambiguous route registry accepted")
	}
}
func TestAutoApprovedCandidatesAreFrozenWithoutEarlySelection(t *testing.T) {
	a, b := fixtureRoute("a", "fixture-model-a", 1), fixtureRoute("b", "fixture-model-b", 2)
	binding := Binding{Mode: Auto, Candidates: []Candidate{{Route: RouteRef{ID: "a", Revision: 1}, Model: a.Model, Effort: &EffortSelection{Mode: EffortExplicit, Value: "high"}}, {Route: RouteRef{ID: "b", Revision: 2}, Model: b.Model, Effort: &EffortSelection{Mode: EffortDefault}}}}
	s := compileOneAuto(t, binding, []Route{a, b})
	got := s.Bindings[Design]
	if got.Target != nil || len(got.Candidates) != 2 || got.Candidates[1].Effort.Value == nil || *got.Candidates[1].Effort.Value != "medium" {
		t.Fatal("auto not frozen correctly")
	}
	binding.Candidates[0].Model = "fixture-changed"
	a.Capabilities[0] = "changed"
	if got.Candidates[0].ResolvedModel != "fixture-model-a" || got.Candidates[0].Capabilities[0] != "text" {
		t.Fatal("snapshot aliases inputs")
	}
}
func compileOneAuto(t *testing.T, b Binding, r []Route) Snapshot {
	t.Helper()
	s, e := Compile(1, []Role{Design}, Layer{Roles: map[Role]Binding{Design: b}}, Layer{}, Layer{}, r)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestAutoNeedsApprovedUniqueCandidates(t *testing.T) {
	r := fixtureRoute("a", "fixture-model-a", 1)
	c := Candidate{Route: RouteRef{ID: "a", Revision: 1}, Model: r.Model, Effort: &EffortSelection{Mode: EffortExplicit, Value: "high"}}
	for _, b := range []Binding{{Mode: Auto}, {Mode: Auto, Candidates: []Candidate{c, c}}, {Mode: Auto, Model: r.Model, Candidates: []Candidate{c}}} {
		if _, e := Compile(1, []Role{Design}, Layer{Roles: map[Role]Binding{Design: b}}, Layer{}, Layer{}, []Route{r}); e == nil {
			t.Fatal("illegal candidate set accepted")
		}
	}
}
func TestSnapshotHashAndReportedModelUnknown(t *testing.T) {
	r := fixtureRoute("a", "fixture-model-a", 1)
	b := locked("a", r.Model, 1)
	s := compileOne(t, b, r)
	if s.Bindings[Design].Target.UpstreamReportedModel != nil || len(s.Hash) != 64 || VerifySnapshot(s) != nil {
		t.Fatal("unknown/report/hash contract broken")
	}
	r.Account = "fixture-new-account"
	other := compileOne(t, b, r)
	if s.Hash == other.Hash {
		t.Fatal("identity change not hashed")
	}
	s.Bindings[Design].Target.Account = "fixture-mutated"
	if VerifySnapshot(s) == nil {
		t.Fatal("modified snapshot verified")
	}
}
func TestParsePlanRejectsUnknownDuplicateAndTrailingFields(t *testing.T) {
	for _, input := range []string{`{"revision":1,"required_roles":["design"],"global":{},"extra":true}`, `{"revision":1,"revision":2,"required_roles":["design"],"global":{}}`, `{"revision":1,"required_roles":["design"],"global":{}}{}`, `null`, `{"revision":1,"required_roles":["design"],"global":{"roles":{"design":{"mode":"locked","route":{"id":"a","revision":1},"model":"fixture-model-a","effort":null}}}}`} {
		if _, e := ParsePlan([]byte(input)); e == nil {
			t.Fatalf("bad JSON/config accepted: %s", input)
		}
	}
	b := locked("a", "fixture-model-a", 1)
	in := Input{SchemaVersion: 1, Revision: 1, RequiredRoles: []Role{Design}, Global: Layer{Roles: map[Role]Binding{Design: b}}}
	raw, _ := json.Marshal(in)
	got, e := ParsePlan(raw)
	if e != nil || got.Global.Roles[Design].Model != b.Model {
		t.Fatalf("valid plan rejected: %v", e)
	}
	if _, e := ParsePlan([]byte(strings.Repeat(" ", MaxPlanBytes+1))); e == nil {
		t.Fatal("oversized config accepted")
	}
}

func TestParseRejectsNullLayersAndNullScalarSettings(t *testing.T) {
	for _, input := range []string{
		`{"schema_version":1,"revision":1,"required_roles":["design"],"global":null}`,
		`{"schema_version":1,"revision":1,"required_roles":["design"],"global":{"roles":null}}`,
		`{"schema_version":1,"revision":1,"required_roles":["design"],"global":{"roles":{"design":{"mode":"inherit","accept_primary_only":null}}}}`,
		`{"schema_version":1,"revision":1,"required_roles":["design"],"global":{"roles":{"design":{"mode":"inherit","required_capabilities":null}}}}`,
	} {
		if _, e := ParsePlan([]byte(input)); e == nil {
			t.Fatal("null field silently defaulted")
		}
	}
}
func TestRouteCapabilitiesAreKnownAndUnique(t *testing.T) {
	for _, caps := range [][]string{{"text", "text"}, {""}, {"unknown"}} {
		r := fixtureRoute("a", "fixture-model-a", 1)
		r.Capabilities = caps
		b := locked("a", r.Model, 1)
		if _, e := Compile(1, []Role{Design}, Layer{Roles: map[Role]Binding{Design: b}}, Layer{}, Layer{}, []Route{r}); e == nil {
			t.Fatal("invalid capabilities frozen")
		}
	}
}

package stageplan

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestProjectIndependenceDefaultAndCanonicalFrozenPairs(t *testing.T) {
	a, b := fixtureRoute("a", "model-a", 1), fixtureRoute("b", "model-b", 1)
	l := Layer{Roles: map[Role]Binding{Design: locked("a", a.Model, 1), Acceptance: locked("a", a.Model, 1)}}
	old, e := Compile(1, []Role{Design, Acceptance}, l, Layer{}, Layer{}, []Route{a})
	if e != nil {
		t.Fatal(e)
	}
	plain, e := CompileWithIndependence(1, []Role{Design, Acceptance}, l, Layer{}, Layer{}, []Route{a}, nil)
	if e != nil || !reflect.DeepEqual(old, plain) {
		t.Fatal("default changed", e)
	}
	if _, e = CompileWithIndependence(1, []Role{Design, Acceptance}, l, Layer{}, Layer{}, []Route{a}, []RolePair{{Design, Acceptance}}); e == nil {
		t.Fatal("same model passed hard policy")
	}
	l.Roles[Acceptance] = locked("b", b.Model, 1)
	pairs := []RolePair{{Acceptance, Design}}
	s, e := CompileWithIndependence(1, []Role{Design, Acceptance}, l, Layer{}, Layer{}, []Route{a, b}, pairs)
	if e != nil || VerifySnapshot(s) != nil || !reflect.DeepEqual(s.Independence, []RolePair{{Design, Acceptance}}) {
		t.Fatal("policy not canonical frozen", e)
	}
	pairs[0].First = Review
	if s.Independence[0].First != Design || s.Hash == plain.Hash {
		t.Fatal("policy not copied/hash bound")
	}
	raw, _ := json.Marshal(s)
	var restored Snapshot
	if json.Unmarshal(raw, &restored) != nil || VerifySnapshot(restored) != nil {
		t.Fatal("policy did not roundtrip")
	}
	changed := Layer{Roles: map[Role]Binding{Acceptance: locked("a", a.Model, 1)}}
	if _, e = CompileRevision(s, changed, l, Layer{}, []Route{a, b}); e == nil {
		t.Fatal("revision defeated policy")
	}
}

func TestProjectIndependenceRejectsInvalidPairsAndKeepsViableAutoCandidates(t *testing.T) {
	for _, pairs := range [][]RolePair{{{Design, Design}}, {{Design, "unknown"}}, {{Design, Acceptance}, {Acceptance, Design}}} {
		if _, e := CanonicalIndependence(pairs); e == nil {
			t.Fatal("invalid pairs accepted")
		}
	}
	a, b := fixtureRoute("a", "model-a", 1), fixtureRoute("b", "model-b", 1)
	ca := Candidate{Route: RouteRef{"a", 1}, Model: a.Model, Effort: &EffortSelection{Mode: EffortDefault}}
	cb := Candidate{Route: RouteRef{"b", 1}, Model: b.Model, Effort: &EffortSelection{Mode: EffortDefault}}
	l := Layer{Roles: map[Role]Binding{Design: {Mode: Auto, Candidates: []Candidate{ca, cb}}, Acceptance: locked("a", a.Model, 1)}}
	s, e := CompileWithIndependence(1, []Role{Design, Acceptance}, l, Layer{}, Layer{}, []Route{a, b}, []RolePair{{Design, Acceptance}})
	if e != nil || len(s.Bindings[Design].Candidates) != 2 {
		t.Fatal("viable approved candidates changed", e)
	}
	l.Roles[Design] = Binding{Mode: Auto, Candidates: []Candidate{ca}}
	if _, e = CompileWithIndependence(1, []Role{Design, Acceptance}, l, Layer{}, Layer{}, []Route{a, b}, []RolePair{{Design, Acceptance}}); e == nil {
		t.Fatal("impossible auto pair accepted")
	}
	// An unrelated single-role task retains the frozen project rule, but
	// does not invent the missing role or another model.
	s, e = CompileWithIndependence(1, []Role{Design}, l, Layer{}, Layer{}, []Route{a}, []RolePair{{Design, Acceptance}})
	if e != nil || len(s.Bindings) != 1 || len(s.Independence) != 1 {
		t.Fatal("single role changed", e)
	}
}

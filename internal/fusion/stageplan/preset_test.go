package stageplan

import "testing"

func TestPresetExpansionKeepsIndependentRoleOverridesAndCopies(t *testing.T) {
	b := Binding{Mode: Locked, Route: &RouteRef{ID: "fixture-route", Revision: 1}, Model: "fixture-model", Effort: &EffortSelection{Mode: EffortExplicit, Value: "high"}}
	in := Layer{Groups: map[Group]Binding{ImplementationTesting: b}, Roles: map[Role]Binding{Testing: {Mode: Inherit}}}
	got, e := ExpandLayer(in)
	if e != nil || len(got.Groups) != 0 || got.Roles[Testing].Mode != Inherit || got.Roles[Implementation].Model != b.Model {
		t.Fatal("group/role precedence changed", e)
	}
	got.Roles[Implementation].Effort.Value = "medium"
	if in.Groups[ImplementationTesting].Effort.Value != "high" {
		t.Fatal("expansion mutates input")
	}
	got, e = ExpandLayer(Layer{Groups: map[Group]Binding{ImplementationTesting: b}})
	if e != nil {
		t.Fatal(e)
	}
	got.Roles[Testing].Effort.Value = "medium"
	if got.Roles[Implementation].Effort.Value != "high" {
		t.Fatal("expanded roles share mutable pointers")
	}
}
func TestPresetApplicationReplacesWholeBindingsIncludingInherit(t *testing.T) {
	b := Binding{Mode: Locked, Route: &RouteRef{ID: "fixture-route", Revision: 1}, Model: "fixture-model", Effort: &EffortSelection{Mode: EffortExplicit, Value: "high"}}
	p := Layer{Groups: map[Group]Binding{ImplementationTesting: b, ReviewAcceptance: b}}
	task := Layer{Groups: map[Group]Binding{ImplementationTesting: {Mode: Inherit}}, Roles: map[Role]Binding{Testing: b}}
	got, e := ApplyPreset(p, task)
	if e != nil || got.Roles[Implementation].Mode != Inherit || got.Roles[Implementation].Route != nil || got.Roles[Testing].Mode != Locked || got.Roles[Acceptance].Effort.Value != "high" {
		t.Fatal("preset application mixed fields or ignored explicit inherit", e)
	}
	got.Roles[Testing].Effort.Value = "medium"
	if task.Roles[Testing].Effort.Value != "high" || p.Groups[ImplementationTesting].Effort.Value != "high" {
		t.Fatal("application mutates callers")
	}
}

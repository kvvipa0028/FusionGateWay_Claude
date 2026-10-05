package workflow

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

func designFixture() DesignDocument {
	return DesignDocument{Goal: "fixture goal", Scope: []string{"src", "tests"}, Constraints: []string{"preserve API"}, Interfaces: []string{"input bytes -> output bytes"}, Acceptance: []string{"existing tests pass", "new boundary test executes"}}
}

func TestDefinitionsKeepOnlyNecessaryRolesAndRejectUnversionedChanges(t *testing.T) {
	for _, tc := range []struct {
		kind  Kind
		roles []stageplan.Role
	}{
		{"investigate", []stageplan.Role{"design"}}, {"review", []stageplan.Role{"review"}},
		{"change", []stageplan.Role{"design", "implementation", "testing", "review", "acceptance"}},
		{"bugfix", []stageplan.Role{"design", "implementation", "testing", "review", "acceptance"}},
	} {
		d, e := DefinitionFor(tc.kind)
		if e != nil || !reflect.DeepEqual(d.RequiredRoles, tc.roles) || VerifyDefinition(d) != nil {
			t.Fatal(tc.kind, d, e)
		}
		d.RequiredRoles[0] = stageplan.Acceptance
		if VerifyDefinition(d) == nil {
			t.Fatal("mutated stage sequence accepted")
		}
		fresh, e := DefinitionFor(tc.kind)
		if e != nil || !reflect.DeepEqual(fresh.RequiredRoles, tc.roles) {
			t.Fatal("caller mutated shared definition")
		}
	}
	for _, k := range []Kind{"", "continue", "pstack", "merge", "deploy", "flash", "change-v2"} {
		if _, e := DefinitionFor(k); !errors.Is(e, ErrInvalid) {
			t.Fatal("unapproved workflow accepted", k, e)
		}
	}
}

func TestFrozenDesignKeepsIndependentAcceptanceHashAndRejectsChangedFields(t *testing.T) {
	doc := designFixture()
	frozen, e := FreezeDesign(doc)
	if e != nil || VerifyDesign(frozen) != nil || len(frozen.Hash) != 64 || len(frozen.AcceptanceHash) != 64 {
		t.Fatal(e)
	}
	doc.Scope[0] = "foreign"
	if frozen.Document.Scope[0] != "src" {
		t.Fatal("mutable input changed frozen approval scope")
	}
	changed := designFixture()
	changed.Interfaces[0] = "different interface"
	v, e := FreezeDesign(changed)
	if e != nil || v.Hash == frozen.Hash || v.AcceptanceHash != frozen.AcceptanceHash {
		t.Fatal("design/criteria identities conflated", e)
	}
	changed = designFixture()
	changed.Acceptance[0] = "ignore tests"
	v, e = FreezeDesign(changed)
	if e != nil || v.Hash == frozen.Hash || v.AcceptanceHash == frozen.AcceptanceHash {
		t.Fatal("new criteria retained old approval identity", e)
	}
	frozen.Document.Constraints[0] = "ignore old constraints"
	if VerifyDesign(frozen) == nil {
		t.Fatal("modified frozen design accepted")
	}
}

func TestDesignRejectsMissingContractAndEscapingWriteScope(t *testing.T) {
	for _, mutate := range []func(*DesignDocument){
		func(d *DesignDocument) { d.Goal = "" }, func(d *DesignDocument) { d.Scope = nil },
		func(d *DesignDocument) { d.Constraints = nil }, func(d *DesignDocument) { d.Interfaces = nil }, func(d *DesignDocument) { d.Acceptance = nil },
		func(d *DesignDocument) { d.Acceptance = []string{" "} }, func(d *DesignDocument) { d.Goal = string([]byte{0xff}) },
		func(d *DesignDocument) { d.Scope = []string{"../outside"} }, func(d *DesignDocument) { d.Scope = []string{"/outside"} },
		func(d *DesignDocument) { d.Scope = []string{"src/../outside"} }, func(d *DesignDocument) { d.Scope = []string{`C:\outside`} },
		func(d *DesignDocument) { d.Scope = []string{".git/config"} }, func(d *DesignDocument) { d.Scope = []string{"src", "src"} },
		func(d *DesignDocument) { d.Scope = []string{"src/\x01hidden"} },
		func(d *DesignDocument) { d.Goal = strings.Repeat("x", 65537) },
	} {
		d := designFixture()
		mutate(&d)
		if _, e := FreezeDesign(d); !errors.Is(e, ErrInvalid) {
			t.Fatal("invalid design accepted", e)
		}
	}
	d := designFixture()
	d.Scope = []string{"."}
	if _, e := FreezeDesign(d); e != nil {
		t.Fatal("explicit whole-folder scope rejected", e)
	}
}

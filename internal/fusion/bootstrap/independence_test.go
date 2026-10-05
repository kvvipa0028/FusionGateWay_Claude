//go:build darwin || linux

package bootstrap

import (
	"testing"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

func TestProjectRegistrationIndependenceIsCanonicalCopiedAndRejectsInvalidRules(t *testing.T) {
	for _, bad := range []bool{false, true} {
		path, d := sourceFixture(t)
		d.Projects[0].Independence = []stageplan.RolePair{{First: stageplan.Acceptance, Second: stageplan.Design}}
		if bad {
			d.Projects[0].Independence[0].Second = stageplan.Acceptance
		}
		writeSource(t, path, d)
		s, e := Load(path)
		if bad {
			if e == nil {
				t.Fatal("invalid private rule loaded")
			}
			continue
		}
		if e != nil {
			t.Fatal(e)
		}
		p, _ := s.Project("fixture-project")
		if p.Configuration.Independence[0].First != stageplan.Design {
			t.Fatal("not canonical")
		}
		p.Configuration.Independence[0].First = stageplan.Review
		again, _ := s.Project("fixture-project")
		if again.Configuration.Independence[0].First != stageplan.Design {
			t.Fatal("mutable registration rule")
		}
	}
}

package stageplan

import (
	"reflect"
	"sort"
)

// RolePair is a project-owned hard model constraint, never a task override.
// Session independence is required separately, even without a model constraint.
type RolePair struct {
	First  Role `json:"first"`
	Second Role `json:"second"`
}

func CanonicalIndependence(pairs []RolePair) ([]RolePair, error) {
	if len(pairs) > 10 {
		return nil, invalid("independence_invalid")
	}
	order := map[Role]int{}
	for i, r := range AllRoles() {
		order[r] = i
	}
	var out []RolePair
	seen := map[RolePair]bool{}
	for _, pair := range pairs {
		a, ok := order[pair.First]
		b, valid := order[pair.Second]
		if !ok || !valid || a == b {
			return nil, invalid("independence_invalid")
		}
		if a > b {
			pair.First, pair.Second = pair.Second, pair.First
		}
		if seen[pair] {
			return nil, invalid("independence_invalid")
		}
		seen[pair] = true
		out = append(out, pair)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].First == out[j].First {
			return order[out[i].Second] < order[out[j].Second]
		}
		return order[out[i].First] < order[out[j].First]
	})
	return out, nil
}

func bindingModels(b FrozenBinding) []string {
	if b.Mode == Locked && b.Target != nil {
		return []string{b.Target.ResolvedModel}
	}
	var out []string
	if b.Mode == Auto {
		for _, target := range b.Candidates {
			out = append(out, target.ResolvedModel)
		}
	}
	return out
}

func checkIndependence(s Snapshot) error {
	pairs, e := CanonicalIndependence(s.Independence)
	if e != nil || !reflect.DeepEqual(pairs, s.Independence) {
		return invalid("independence_invalid")
	}
	for _, pair := range pairs {
		a, aok := s.Bindings[pair.First]
		b, bok := s.Bindings[pair.Second]
		if !aok || !bok {
			continue
		}
		possible := false
		for _, first := range bindingModels(a) {
			for _, second := range bindingModels(b) {
				if first != "" && second != "" && first != second {
					possible = true
				}
			}
		}
		if !possible {
			return invalid("independence_conflict")
		}
	}
	return nil
}

// CompileWithIndependence preserves all approved candidates. Concrete choices
// are checked again against historical intents by the Store before launch.
func CompileWithIndependence(revision int64, required []Role, global, project, task Layer, routes []Route, pairs []RolePair) (Snapshot, error) {
	s, e := Compile(revision, required, global, project, task, routes)
	if e != nil {
		return Snapshot{}, e
	}
	s.Independence, e = CanonicalIndependence(pairs)
	if e != nil {
		return Snapshot{}, e
	}
	if e = checkIndependence(s); e != nil {
		return Snapshot{}, e
	}
	s.Hash, e = snapshotHash(s)
	return s, e
}

package stageplan

import "encoding/json"

// CompileRevision resolves only explicitly selected roles against current
// defaults and routes. Preserved historical bindings grant no live admission.
// The store separately enforces that selected bindings have not started.
func CompileRevision(current Snapshot, changes Layer, global, project Layer, routes []Route) (Snapshot, error) {
	if current.SchemaVersion != 1 || current.Revision < 1 || current.Revision == int64(1<<63-1) || VerifySnapshot(current) != nil {
		return Snapshot{}, invalid("revision_invalid")
	}
	need := map[Role]bool{}
	for _, r := range current.RequiredRoles {
		if !knownRole(r) || need[r] {
			return Snapshot{}, invalid("required_roles_invalid")
		}
		need[r] = true
	}
	if len(need) == 0 || len(current.Bindings) != len(need) {
		return Snapshot{}, invalid("required_roles_invalid")
	}
	for r := range need {
		if _, ok := current.Bindings[r]; !ok {
			return Snapshot{}, invalid("required_role_unresolved")
		}
	}
	expanded, e := expand(changes)
	if e != nil {
		return Snapshot{}, e
	}
	if len(expanded) == 0 {
		return Snapshot{}, invalid("revision_changes_missing")
	}
	var selected []Role
	for r := range expanded {
		if !need[r] {
			return Snapshot{}, invalid("revision_role_not_required")
		}
		selected = append(selected, r)
	}
	updated, e := Compile(current.Revision+1, selected, global, project, changes, routes)
	if e != nil {
		return Snapshot{}, e
	}
	raw, e := json.Marshal(current)
	if e != nil {
		return Snapshot{}, e
	}
	var next Snapshot
	if e = json.Unmarshal(raw, &next); e != nil {
		return Snapshot{}, e
	}
	next.Revision = updated.Revision
	for r, b := range updated.Bindings {
		next.Bindings[r] = b
	}
	next.Hash, e = snapshotHash(next)
	return next, e
}

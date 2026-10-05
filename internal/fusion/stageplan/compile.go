package stageplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

var ErrInvalidPlan = errors.New("invalid stage plan")

func invalid(reason string) error { return fmt.Errorf("%w: %s", ErrInvalidPlan, reason) }
func knownRole(r Role) bool {
	for _, v := range AllRoles() {
		if r == v {
			return true
		}
	}
	return false
}
func knownValue(s string) bool {
	if s == "" || len(s) > 256 || s == "unknown" || s == "undisclosed" || s == "default" {
		return false
	}
	return !strings.ContainsFunc(s, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) })
}
func validRef(r RouteRef) bool { return knownValue(r.ID) && r.Revision > 0 }
func validateEffort(e *EffortSelection) error {
	if e == nil {
		return invalid("effort_unspecified")
	}
	switch e.Mode {
	case EffortExplicit:
		if !knownValue(e.Value) {
			return invalid("effort_value_missing")
		}
	case EffortDefault, EffortNone:
		if e.Value != "" {
			return invalid("effort_value_incompatible")
		}
	default:
		return invalid("effort_mode_unknown")
	}
	return nil
}
func validateBinding(b Binding) error {
	seen := map[string]bool{}
	for _, c := range b.RequiredCapabilities {
		if !knownValue(c) || seen[c] {
			return invalid("capability_invalid")
		}
		seen[c] = true
	}
	switch b.Mode {
	case Inherit:
		if b.Route != nil || b.Model != "" || b.Effort != nil || len(b.Candidates) > 0 || len(b.RequiredCapabilities) > 0 || b.AcceptPrimaryOnly {
			return invalid("inherit_contains_binding")
		}
	case Locked:
		if b.Route == nil || !validRef(*b.Route) || !knownValue(b.Model) || len(b.Candidates) > 0 {
			return invalid("locked_incomplete_or_mixed")
		}
		return validateEffort(b.Effort)
	case Auto:
		if b.Route != nil || b.Model != "" || b.Effort != nil || len(b.Candidates) == 0 {
			return invalid("auto_incomplete_or_mixed")
		}
		seen := map[RouteRef]bool{}
		for _, c := range b.Candidates {
			if !validRef(c.Route) || !knownValue(c.Model) || seen[c.Route] {
				return invalid("candidate_invalid_or_duplicate")
			}
			seen[c.Route] = true
			if e := validateEffort(c.Effort); e != nil {
				return e
			}
		}
	default:
		return invalid("binding_mode_unknown")
	}
	return nil
}
func expand(l Layer) (map[Role]Binding, error) {
	out := map[Role]Binding{}
	for g, b := range l.Groups {
		if e := validateBinding(b); e != nil {
			return nil, e
		}
		var roles []Role
		switch g {
		case DesignPlanning:
			roles = []Role{Design}
		case ImplementationTesting:
			roles = []Role{Implementation, Testing}
		case ReviewAcceptance:
			roles = []Role{Review, Acceptance}
		default:
			return nil, invalid("group_unknown")
		}
		for _, r := range roles {
			out[r] = b
		}
	}
	for r, b := range l.Roles {
		if !knownRole(r) {
			return nil, invalid("role_unknown")
		}
		if e := validateBinding(b); e != nil {
			return nil, e
		}
		out[r] = b
	}
	return out, nil
}
func freeze(c Candidate, b Binding, registry map[RouteRef]Route) (ExecutionTarget, error) {
	r, ok := registry[c.Route]
	if !ok || !r.Admitted {
		return ExecutionTarget{}, invalid("route_revision_not_admitted")
	}
	for _, v := range []string{r.Model, r.Account, r.Workspace, r.CredentialIdentity, r.RuntimeVersion, r.BillingPath} {
		if !knownValue(v) {
			return ExecutionTarget{}, invalid("route_identity_or_version_unknown")
		}
	}
	if !r.BillingKnown || r.Model != c.Model {
		return ExecutionTarget{}, invalid("billing_or_model_unverified")
	}
	if r.PluginVersion != nil && !knownValue(*r.PluginVersion) {
		return ExecutionTarget{}, invalid("plugin_version_unknown")
	}
	if r.LockEnforcement != ControlledCalls && !(r.LockEnforcement == PrimaryOnly && b.AcceptPrimaryOnly) {
		return ExecutionTarget{}, invalid("call_lock_scope_insufficient")
	}
	caps := map[string]bool{}
	for _, v := range r.Capabilities {
		if !knownValue(v) || caps[v] {
			return ExecutionTarget{}, invalid("route_capabilities_invalid")
		}
		caps[v] = true
	}
	for _, need := range b.RequiredCapabilities {
		if !contains(r.Capabilities, need) {
			return ExecutionTarget{}, invalid("required_capability_missing")
		}
	}
	efforts := map[string]bool{}
	for _, v := range r.Efforts {
		if !knownValue(v) || efforts[v] {
			return ExecutionTarget{}, invalid("route_effort_catalog_invalid")
		}
		efforts[v] = true
	}
	if r.NoEffort && (len(r.Efforts) > 0 || r.DefaultEffort != nil) {
		return ExecutionTarget{}, invalid("no_effort_catalog_incompatible")
	}
	effort := FrozenEffort{RequestedMode: c.Effort.Mode}
	switch c.Effort.Mode {
	case EffortNone:
		if !r.NoEffort {
			return ExecutionTarget{}, invalid("none_not_admitted")
		}
	case EffortDefault:
		if r.DefaultEffort == nil || !efforts[*r.DefaultEffort] {
			return ExecutionTarget{}, invalid("default_effort_undisclosed")
		}
		v := *r.DefaultEffort
		effort.Value = &v
	case EffortExplicit:
		if !efforts[c.Effort.Value] {
			return ExecutionTarget{}, invalid("effort_unsupported")
		}
		v := c.Effort.Value
		effort.Value = &v
	}
	var plugin *string
	if r.PluginVersion != nil {
		v := *r.PluginVersion
		plugin = &v
	}
	return ExecutionTarget{Route: c.Route, RequestedModel: c.Model, ResolvedModel: r.Model, UpstreamReportedModel: nil, Account: r.Account, Workspace: r.Workspace, CredentialIdentity: r.CredentialIdentity, Effort: effort, BillingPath: r.BillingPath, RuntimeVersion: r.RuntimeVersion, PluginVersion: plugin, Capabilities: append([]string(nil), r.Capabilities...), LockEnforcement: r.LockEnforcement}, nil
}
func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// Compile replaces entire bindings across layers. The returned snapshot retains
// no references to input slices, pointers, or registry revisions.
func Compile(revision int64, required []Role, global, project, task Layer, routes []Route) (Snapshot, error) {
	if revision < 1 || len(required) == 0 {
		return Snapshot{}, invalid("revision_or_required_roles_missing")
	}
	need := map[Role]bool{}
	for _, r := range required {
		if !knownRole(r) || need[r] {
			return Snapshot{}, invalid("required_roles_invalid")
		}
		need[r] = true
	}
	registry := map[RouteRef]Route{}
	for _, r := range routes {
		ref := RouteRef{r.ID, r.Revision}
		if !validRef(ref) {
			return Snapshot{}, invalid("registry_revision_invalid")
		}
		if _, exists := registry[ref]; exists {
			return Snapshot{}, invalid("registry_revision_duplicate")
		}
		registry[ref] = r
	}
	merged := map[Role]Binding{}
	source := map[Role]string{}
	for _, layer := range []struct {
		name  string
		value Layer
	}{{"global", global}, {"project", project}, {"task", task}} {
		expanded, e := expand(layer.value)
		if e != nil {
			return Snapshot{}, e
		}
		for role, b := range expanded {
			if b.Mode != Inherit {
				merged[role] = b
				source[role] = layer.name
			}
		}
	}
	s := Snapshot{SchemaVersion: 1, Revision: revision, Bindings: map[Role]FrozenBinding{}}
	for _, role := range AllRoles() {
		if !need[role] {
			continue
		}
		b, ok := merged[role]
		if !ok {
			return Snapshot{}, invalid("required_role_unresolved")
		}
		s.RequiredRoles = append(s.RequiredRoles, role)
		f := FrozenBinding{Mode: b.Mode, Source: source[role], RequiredCapabilities: append([]string(nil), b.RequiredCapabilities...), AcceptPrimaryOnly: b.AcceptPrimaryOnly}
		if b.Mode == Locked {
			target, e := freeze(Candidate{Route: *b.Route, Model: b.Model, Effort: b.Effort}, b, registry)
			if e != nil {
				return Snapshot{}, e
			}
			f.Target = &target
		} else {
			for _, c := range b.Candidates {
				target, e := freeze(c, b, registry)
				if e != nil {
					return Snapshot{}, e
				}
				f.Candidates = append(f.Candidates, target)
			}
		}
		s.Bindings[role] = f
	}
	h, e := snapshotHash(s)
	if e != nil {
		return Snapshot{}, e
	}
	s.Hash = h
	return s, nil
}
func snapshotHash(s Snapshot) (string, error) {
	s.Hash = ""
	b, e := json.Marshal(s)
	if e != nil {
		return "", e
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

// VerifySnapshot checks integrity, not issuer authenticity or current admission.
func VerifySnapshot(s Snapshot) error {
	if e := checkIndependence(s); e != nil {
		return e
	}
	h, e := snapshotHash(s)
	if e != nil || len(s.Hash) != 64 || h != s.Hash {
		return invalid("snapshot_hash_mismatch")
	}
	return nil
}

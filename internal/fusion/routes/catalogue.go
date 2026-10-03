// Package routes separates official native candidates from execution admission.
// No candidate in the built-in catalogue has live generation or quota rights.
package routes

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

var (
	ErrNotAdmitted = errors.New("route not admitted for this purpose")
	ErrImmutable   = errors.New("route revision is immutable")
)

type Permission struct {
	Status     string  `json:"status"`
	Reason     string  `json:"reason"`
	ReportHash *string `json:"report_hash"`
}
type Candidate struct {
	ID                  string     `json:"id"`
	Revision            int64      `json:"revision"`
	Provider            string     `json:"provider"`
	Kind                string     `json:"kind"`
	Runtime             string     `json:"runtime"`
	Region              string     `json:"region"`
	AuthMethod          string     `json:"auth_method"`
	AuthenticationOwner string     `json:"authentication_owner"`
	Billing             string     `json:"billing"`
	AllowedPurposes     []string   `json:"allowed_purposes"`
	Generation          Permission `json:"generation"`
	Quota               Permission `json:"quota"`
}
type Evidence struct {
	RouteHash                                                                                                                  string
	Purpose                                                                                                                    string
	Route                                                                                                                      stageplan.RouteRef
	RuntimeVersion, Model, Account, Workspace, CredentialIdentity, Region, Billing, TransportID, ReportHash                    string
	PublisherVerified, IdentityVerified, BillingVerified, ModelVerified, EffortVerified, AllCallsControlled, WorkspaceEnforced bool
}
type Registry struct {
	mu         sync.Mutex
	candidates map[stageplan.RouteRef]Candidate
	routes     map[stageplan.RouteRef]stageplan.Route
	proofs     map[stageplan.RouteRef]Evidence
	verify     func(Evidence) bool
}

func unknown() Permission {
	return Permission{Status: "unverified", Reason: "native route admission evidence missing"}
}
func Builtins() []Candidate {
	return []Candidate{
		{ID: "codex-chatgpt", Revision: 1, Provider: "openai", Kind: "official_runtime", Runtime: "codex", Region: "unverified", AuthMethod: "oauth", AuthenticationOwner: "local_user", Billing: "subscription", AllowedPurposes: []string{"engineering", "diagnostic", "quota"}, Generation: unknown(), Quota: unknown()},
		{ID: "grok-subscription", Revision: 1, Provider: "xai", Kind: "official_runtime", Runtime: "grok", Region: "unverified", AuthMethod: "oauth", AuthenticationOwner: "local_user", Billing: "subscription", AllowedPurposes: []string{"engineering", "diagnostic", "quota"}, Generation: unknown(), Quota: unknown()},
		{ID: "glm-cn-claude", Revision: 1, Provider: "bigmodel", Kind: "official_runtime", Runtime: "claude", Region: "CN", AuthMethod: "api_key", AuthenticationOwner: "local_user", Billing: "coding_plan", AllowedPurposes: []string{"engineering", "diagnostic", "quota"}, Generation: unknown(), Quota: unknown()},
	}
}

// verify belongs to the trusted native Adapter/evidence collector and must
// verify a persisted report's contents, not only accept a caller's SHA string.
// nil disables all admission. Neither Evidence nor this callback is a public API.
func NewRegistry(verify func(Evidence) bool) *Registry {
	r := &Registry{candidates: map[stageplan.RouteRef]Candidate{}, routes: map[stageplan.RouteRef]stageplan.Route{}, proofs: map[stageplan.RouteRef]Evidence{}, verify: verify}
	for _, c := range Builtins() {
		r.candidates[stageplan.RouteRef{ID: c.ID, Revision: c.Revision}] = c
	}
	return r
}
func clone[T any](v T) T { raw, _ := json.Marshal(v); var out T; json.Unmarshal(raw, &out); return out }
func equal(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
func value(s string) bool {
	return s != "" && len(s) <= 256 && !strings.ContainsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

// Register freezes a new exact candidate revision after account-region setup.
// It never imports authentication or accepts pre-admitted permission fields.
func (r *Registry) Register(c Candidate) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !value(c.ID) || c.Revision < 1 || !value(c.Region) || c.Kind != "official_runtime" || c.AuthenticationOwner != "local_user" || c.Generation.Status != "unverified" || c.Quota.Status != "unverified" || c.Generation.ReportHash != nil || c.Quota.ReportHash != nil {
		return ErrNotAdmitted
	}
	matched := false
	for _, base := range Builtins() {
		if c.Provider == base.Provider && c.Runtime == base.Runtime && c.AuthMethod == base.AuthMethod && c.Billing == base.Billing && equal(c.AllowedPurposes, base.AllowedPurposes) && (base.Region == "unverified" || c.Region == base.Region) {
			matched = true
		}
	}
	if !matched {
		return ErrNotAdmitted
	}
	ref := stageplan.RouteRef{ID: c.ID, Revision: c.Revision}
	if old, ok := r.candidates[ref]; ok {
		if equal(old, c) {
			return nil
		}
		return ErrImmutable
	}
	r.candidates[ref] = clone(c)
	return nil
}
func (r *Registry) Candidate(ref stageplan.RouteRef) (Candidate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.candidates[ref]
	if !ok {
		return Candidate{}, ErrNotAdmitted
	}
	return clone(c), nil
}
func (r *Registry) Candidates() []Candidate {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Candidate, 0, len(r.candidates))
	for _, c := range r.candidates {
		out = append(out, clone(c))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID == out[j].ID {
			return out[i].Revision < out[j].Revision
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Admit grants generation only. Quota rights remain independent. Only an exact
// immutable model/account/Runtime/transport proof can promote a native route.
func (r *Registry) Admit(route stageplan.Route, e Evidence) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	ref := stageplan.RouteRef{ID: route.ID, Revision: route.Revision}
	c, ok := r.candidates[ref]
	hash, hashErr := hex.DecodeString(e.ReportHash)
	manifest, _ := json.Marshal(route)
	manifestHash := sha256.Sum256(manifest)
	if !ok || c.Generation.Status == "blocked" || e.RouteHash != hex.EncodeToString(manifestHash[:]) || c.Region == "unverified" || e.Region != c.Region || e.Route != ref || e.Purpose != "generation" || e.Billing != c.Billing || e.RuntimeVersion != route.RuntimeVersion || e.Model != route.Model || e.Account != route.Account || e.Workspace != route.Workspace || e.CredentialIdentity != route.CredentialIdentity || !value(e.TransportID) || hashErr != nil || len(hash) != 32 || !e.PublisherVerified || !e.IdentityVerified || !e.BillingVerified || !e.ModelVerified || !e.EffortVerified || !e.AllCallsControlled || !e.WorkspaceEnforced || !route.BillingKnown || !route.Admitted || route.PluginVersion != nil || route.LockEnforcement != stageplan.ControlledCalls || r.verify == nil || !r.verify(e) {
		return ErrNotAdmitted
	}
	if old, exists := r.routes[ref]; exists {
		if equal(old, route) && r.proofs[ref] == e {
			return nil
		}
		return ErrImmutable
	}
	// Reuse the compiler's exact identity/model/capability/effort validation.
	effort := &stageplan.EffortSelection{Mode: stageplan.EffortDefault}
	if route.NoEffort {
		effort.Mode = stageplan.EffortNone
	}
	b := stageplan.Binding{Mode: stageplan.Locked, Route: &ref, Model: route.Model, Effort: effort}
	if _, err := stageplan.Compile(1, []stageplan.Role{stageplan.Design}, stageplan.Layer{Roles: map[stageplan.Role]stageplan.Binding{stageplan.Design: b}}, stageplan.Layer{}, stageplan.Layer{}, []stageplan.Route{route}); err != nil {
		return ErrNotAdmitted
	}
	r.routes[ref] = clone(route)
	r.proofs[ref] = e
	h := e.ReportHash
	c.Generation = Permission{Status: "admitted", Reason: "version-bound native proof verified", ReportHash: &h}
	r.candidates[ref] = c
	return nil
}
func (r *Registry) Resolve(ref stageplan.RouteRef) (stageplan.Route, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.routes[ref]
	if !ok || r.candidates[ref].Generation.Status != "admitted" || r.verify == nil || !r.verify(r.proofs[ref]) {
		return stageplan.Route{}, ErrNotAdmitted
	}
	return clone(v), nil
}
func (r *Registry) Revoke(ref stageplan.RouteRef) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.routes, ref)
	delete(r.proofs, ref)
	if c, ok := r.candidates[ref]; ok {
		c.Generation = Permission{Status: "blocked", Reason: "route revoked"}
		r.candidates[ref] = c
	}
}

package routes

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"testing"
)

func routeHash(r stageplan.Route) string {
	raw, _ := json.Marshal(r)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

func TestThreeOfficialCandidatesRemainSeparatelyUnverified(t *testing.T) {
	r := NewRegistry(nil)
	if len(r.Candidates()) != 3 {
		t.Fatal("three native candidates required")
	}
	for _, c := range r.Candidates() {
		if c.Kind != "official_runtime" || c.AuthenticationOwner != "local_user" || c.Generation.Status != "unverified" || c.Quota.Status != "unverified" {
			t.Fatalf("unsupported capability claimed: %+v", c)
		}
		if _, e := r.Resolve(stageplan.RouteRef{ID: c.ID, Revision: c.Revision}); !errors.Is(e, ErrNotAdmitted) {
			t.Fatalf("candidate executable: %v", e)
		}
	}
	glm, _ := r.Candidate(stageplan.RouteRef{ID: "glm-cn-claude", Revision: 1})
	if glm.Region != "CN" || glm.Billing != "coding_plan" || glm.AuthMethod != "api_key" || glm.Runtime != "claude" {
		t.Fatal("wrong selected GLM host/plan")
	}
}
func TestCandidateReturnedCopiesCannotGrantAdmission(t *testing.T) {
	r := NewRegistry(nil)
	all := r.Candidates()
	all[0].Generation.Status = "admitted"
	all[0].AllowedPurposes[0] = "arbitrary-app"
	c, _ := r.Candidate(stageplan.RouteRef{ID: all[0].ID, Revision: 1})
	if c.Generation.Status != "unverified" || c.AllowedPurposes[0] == "arbitrary-app" {
		t.Fatal("registry alias")
	}
	if _, e := r.Candidate(stageplan.RouteRef{ID: c.ID, Revision: 2}); e == nil {
		t.Fatal("latest revision fallback")
	}
}
func proofRoute(c Candidate) stageplan.Route {
	return stageplan.Route{ID: c.ID, Revision: c.Revision, Model: "fixture-model", Account: "fixture-account", Workspace: "fixture-workspace", CredentialIdentity: "fixture-identity", RuntimeVersion: "fixture-v1", BillingPath: "fixture-subscription", BillingKnown: true, Admitted: true, NoEffort: true, Capabilities: []string{"text"}, LockEnforcement: stageplan.ControlledCalls}
}
func TestAdmissionRequiresTrustedVersionBoundEvidence(t *testing.T) {
	trust := true
	r := NewRegistry(func(e Evidence) bool {
		return trust && e.ReportHash == "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	})
	ref := stageplan.RouteRef{ID: "codex-chatgpt", Revision: 2}
	c, _ := r.Candidate(stageplan.RouteRef{ID: ref.ID, Revision: 1})
	c.Revision = 2
	c.Region = "fixture-region"
	if e := r.Register(c); e != nil {
		t.Fatal(e)
	}
	route := proofRoute(c)
	evidence := Evidence{RouteHash: routeHash(route), Region: "fixture-region", Purpose: "generation", Route: ref, RuntimeVersion: route.RuntimeVersion, Model: route.Model, Account: route.Account, Workspace: route.Workspace, CredentialIdentity: route.CredentialIdentity, Billing: "subscription", TransportID: "fixture-transport-v1", ReportHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PublisherVerified: true, IdentityVerified: true, BillingVerified: true, ModelVerified: true, EffortVerified: true, AllCallsControlled: true, WorkspaceEnforced: true}
	for _, name := range []string{"none", "publisher", "billing", "account", "model", "version", "all_calls", "sandbox", "hash", "purpose", "region", "manifest"} {
		bad := evidence
		badRoute := route
		switch name {
		case "none":
			bad = Evidence{}
		case "publisher":
			bad.PublisherVerified = false
		case "billing":
			bad.BillingVerified = false
		case "account":
			bad.Account = "fixture-other"
		case "model":
			bad.Model = "fixture-other"
		case "version":
			bad.RuntimeVersion = "fixture-other"
		case "all_calls":
			bad.AllCallsControlled = false
		case "sandbox":
			bad.WorkspaceEnforced = false
		case "hash":
			bad.ReportHash = "not-proof"
		case "purpose":
			bad.Purpose = "quota"
		case "manifest":
			badRoute.BillingPath = "fixture-cash"
		case "region":
			bad.Region = "CN"
		}
		if e := r.Admit(badRoute, bad); e == nil {
			t.Fatalf("%s admitted", name)
		}
	}
	if e := r.Admit(route, evidence); e != nil {
		t.Fatal(e)
	}
	got, e := r.Resolve(ref)
	if e != nil || got.Model != route.Model || !got.Admitted {
		t.Fatal(e)
	}
	quota, _ := r.Candidate(ref)
	if quota.Quota.Status != "unverified" {
		t.Fatal("generation fabricated quota admission")
	}
	trust = false
	if _, e := r.Resolve(ref); e == nil {
		t.Error("invalidated proof still executable")
	}
	trust = true
	r.Revoke(ref)
	if e := r.Admit(route, evidence); e == nil {
		t.Error("revoked revision re-admitted")
	}
	route.Model = "fixture-other"
	if e := r.Admit(route, evidence); e == nil {
		t.Fatal("immutable route replaced")
	}
}
func TestUnverifiedBillingOrCommunityPluginCannotExecute(t *testing.T) {
	r := NewRegistry(nil)
	ref := stageplan.RouteRef{ID: "glm-cn-claude", Revision: 1}
	c, _ := r.Candidate(ref)
	route := proofRoute(c)
	if e := r.Admit(route, Evidence{}); e == nil {
		t.Fatal("diagnostic admitted")
	}
	if _, e := r.Candidate(stageplan.RouteRef{ID: "grok-plugin", Revision: 1}); e == nil {
		t.Fatal("community plugin automatic admission")
	}
}

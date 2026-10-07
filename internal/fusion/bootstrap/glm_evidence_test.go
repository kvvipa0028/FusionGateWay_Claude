package bootstrap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/fusion/runtime/glm"
	"github.com/yetone/magpie/internal/fusion/stageplan"
)

func glmEvidenceFixture(t *testing.T, mutate func(map[string]any)) string {
	t.Helper()
	dir := privateExecutionDir(t)
	document := map[string]any{
		"schema_version": 1, "kind": "fusion-glm-generation-evidence",
		"checked_at":      "2026-10-06T15:00:00+00:00",
		"endpoint":        "https://open.bigmodel.cn/api/anthropic",
		"requested_model": "glm-5.3", "probe_status": "pass",
		"connection_verified": true, "native_reported_models": []string{"glm-5.3"},
		"publisher_runtime_version": glm.CLIVersion, "publisher_verified": true,
		"quota_windows": []map[string]any{{"kind": "coding_plan", "unit": "percent", "used_percent": 42.5}},
		"transport_id":  "glm-cn-claude-coding-plan",
	}
	if mutate != nil {
		mutate(document)
	}
	raw, e := json.Marshal(document)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "glm-generation-evidence.json")
	if os.WriteFile(path, raw, 0600) != nil {
		t.Fatal("evidence write")
	}
	return path
}

func glmEvidenceRoute() stageplan.Route {
	return stageplan.Route{ID: "local-glm", Revision: 1, Model: "glm-5.3", Account: "fixture-account", Workspace: "fixture-provider-home", CredentialIdentity: "fixture-identity", RuntimeVersion: glm.CLIVersion, BillingPath: "coding_plan", BillingKnown: true, Admitted: true, NoEffort: true, LockEnforcement: stageplan.ControlledCalls, Capabilities: []string{"text"}}
}

func TestLoadGLMEvidenceAcceptsAndReverifiesExactFile(t *testing.T) {
	path := glmEvidenceFixture(t, nil)
	route := glmEvidenceRoute()
	evidence, verify, e := LoadGLMEvidence(path, route)
	if e != nil || evidence.Purpose != "generation" || evidence.Region != "CN" || evidence.Billing != "coding_plan" || !evidence.BillingVerified || !evidence.ModelVerified || !evidence.PublisherVerified || evidence.TransportID != "glm-cn-claude-coding-plan" || len(evidence.ReportHash) != 64 {
		t.Fatal("valid evidence refused", e)
	}
	if !verify(evidence) {
		t.Fatal("verify rejected its own evidence")
	}
	// Model drift between the file and the declared route must fail.
	other := route
	other.Model = "glm-other"
	if _, _, e := LoadGLMEvidence(path, other); e == nil {
		t.Fatal("model drift accepted")
	}
	// Tampering with the file must fail the verify callback.
	raw, _ := os.ReadFile(path)
	tampered := strings.Replace(string(raw), "42.5", "43.5", 1)
	if os.WriteFile(path, []byte(tampered), 0600) == nil && verify(evidence) {
		t.Fatal("tampered evidence still verified")
	}
}

func TestLoadGLMEvidenceRejectsUnsafeShapes(t *testing.T) {
	route := glmEvidenceRoute()
	cases := map[string]func(map[string]any){
		"probe_failed":    func(d map[string]any) { d["probe_status"] = "timeout" },
		"connection_off":  func(d map[string]any) { d["connection_verified"] = false },
		"no_quota_window": func(d map[string]any) { d["quota_windows"] = []map[string]any{} },
		"quota_unbounded": func(d map[string]any) {
			d["quota_windows"] = []map[string]any{{"kind": "coding_plan", "unit": "percent", "used_percent": 142.0}}
		},
		"publisher_wrong":  func(d map[string]any) { d["publisher_runtime_version"] = "0.0.0" },
		"model_mismatch":   func(d map[string]any) { d["native_reported_models"] = []string{"other"} },
		"transport_drift":  func(d map[string]any) { d["transport_id"] = "other-transport" },
		"foreign_endpoint": func(d map[string]any) { d["endpoint"] = "https://example.invalid/api" },
		"schema_drift":     func(d map[string]any) { d["schema_version"] = 2 },
		"unknown_field": func(d map[string]any) {
			raw, _ := json.Marshal(d)
			_ = raw
			d["extra"] = "field"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			path := glmEvidenceFixture(t, mutate)
			if _, _, e := LoadGLMEvidence(path, route); e == nil {
				t.Fatal("unsafe evidence accepted")
			}
		})
	}
	// Permissions and placement are part of the certificate.
	path := glmEvidenceFixture(t, nil)
	if os.Chmod(path, 0644) == nil {
		if _, _, e := LoadGLMEvidence(path, route); e == nil {
			t.Fatal("group-readable evidence accepted")
		}
		_ = os.Chmod(path, 0600)
	}
	insideGit := filepath.Join(privateExecutionDir(t), "repo", "evidence.json")
	os.MkdirAll(filepath.Dir(insideGit), 0700)
	raw, _ := os.ReadFile(path)
	os.WriteFile(insideGit, raw, 0600)
	os.Mkdir(filepath.Join(filepath.Dir(insideGit), ".git"), 0700)
	if _, _, e := LoadGLMEvidence(insideGit, route); e == nil {
		t.Fatal("evidence inside a Git worktree accepted")
	}
}

package bootstrap

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/fusion/routes"
	"github.com/yetone/magpie/internal/fusion/runtime/glm"
	"github.com/yetone/magpie/internal/fusion/stageplan"
)

// The generation evidence file is the operator-approved admission certificate
// for one exact declared GLM route revision: it records the real tool-free
// probe pass, the pinned-publisher verification and one real read-only quota
// observation proving a billed Coding Plan exists for the account. It never
// proves a current launch's quota: the live CN reader gates that separately.
const glmevidenceKind = "fusion-glm-generation-evidence"

type GLMEvidenceWindow struct {
	Kind        string   `json:"kind"`
	Unit        string   `json:"unit"`
	UsedPercent *float64 `json:"used_percent"`
}

type GLMEvidenceFile struct {
	SchemaVersion         int                 `json:"schema_version"`
	Kind                  string              `json:"kind"`
	CheckedAt             string              `json:"checked_at"`
	Endpoint              string              `json:"endpoint"`
	RequestedModel        string              `json:"requested_model"`
	ProbeStatus           string              `json:"probe_status"`
	ConnectionVerified    bool                `json:"connection_verified"`
	NativeReportedModels  []string            `json:"native_reported_models"`
	PublisherRuntimeVersn string              `json:"publisher_runtime_version"`
	PublisherVerified     bool                `json:"publisher_verified"`
	QuotaWindows          []GLMEvidenceWindow `json:"quota_windows"`
	TransportID           string              `json:"transport_id"`
}

// glmevidenceTransport is the fixed CN transport label recorded by the probe
// recorder; the runtime always uses the built-in fixed CN transport.
const glmevidenceTransport = "glm-cn-claude-coding-plan"

func decodeGLMEvidence(raw []byte) (GLMEvidenceFile, error) {
	var file GLMEvidenceFile
	if len(raw) == 0 || len(raw) > 64<<10 || !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
		return file, ErrControlHost
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&file) != nil {
		return GLMEvidenceFile{}, ErrControlHost
	}
	if _, err := d.Token(); err == nil {
		return GLMEvidenceFile{}, ErrControlHost // trailing values are not evidence
	}
	return file, nil
}

func validEvidencePercent(v *float64) bool {
	return v != nil && *v >= 0 && *v <= 100
}

func glmevidenceValid(file GLMEvidenceFile, route stageplan.Route) bool {
	if file.SchemaVersion != 1 || file.Kind != glmevidenceKind || !strings.Contains(file.Endpoint, "open.bigmodel.cn") || strings.ContainsAny(file.Endpoint, "\x00\n") || file.CheckedAt == "" || len(file.CheckedAt) > 64 {
		return false
	}
	if file.ProbeStatus != "pass" || !file.ConnectionVerified || !file.PublisherVerified || file.PublisherRuntimeVersn != glm.CLIVersion {
		return false
	}
	if file.RequestedModel != route.Model || len(file.NativeReportedModels) != 1 || file.NativeReportedModels[0] != route.Model {
		return false
	}
	if file.TransportID != glmevidenceTransport {
		return false
	}
	// Billing evidence: at least one real Coding Plan window was observed.
	found := false
	for _, w := range file.QuotaWindows {
		if (w.Kind == "coding_plan" || w.Kind == "subscription") && w.Unit == "percent" && validEvidencePercent(w.UsedPercent) {
			found = true
		}
	}
	if !found {
		return false
	}
	return route.BillingPath == "coding_plan" && route.PluginVersion == nil && route.RuntimeVersion == glm.CLIVersion
}

// LoadGLMEvidence reads the private evidence file for one declared route and
// returns the Registry evidence plus a verify callback that revalidates the
// file on every resolution. The file must stay a 0600 regular file outside
// Git; drift fails closed.
func LoadGLMEvidence(path string, route stageplan.Route) (routes.Evidence, func(routes.Evidence) bool, error) {
	var zero routes.Evidence
	info, e := os.Stat(path)
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() == 0 || info.Size() > 64<<10 {
		return zero, nil, ErrControlHost
	}
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		if _, e := os.Lstat(filepath.Join(parent, ".git")); e == nil {
			return zero, nil, ErrControlHost
		}
		if filepath.Dir(parent) == parent {
			break
		}
	}
	raw, e := os.ReadFile(path)
	if e != nil || len(raw) == 0 || len(raw) > 64<<10 {
		return zero, nil, ErrControlHost
	}
	file, e := decodeGLMEvidence(raw)
	if e != nil || !glmevidenceValid(file, route) {
		return zero, nil, ErrControlHost
	}
	digest := sha256.Sum256(raw)
	manifest, _ := json.Marshal(route)
	manifestHash := sha256.Sum256(manifest)
	evidence := routes.Evidence{Route: stageplan.RouteRef{ID: route.ID, Revision: route.Revision}, Purpose: "generation", RouteHash: hex.EncodeToString(manifestHash[:]), RuntimeVersion: route.RuntimeVersion, Model: route.Model, Account: route.Account, Workspace: route.Workspace, CredentialIdentity: route.CredentialIdentity, Region: "CN", Billing: "coding_plan", TransportID: file.TransportID, ReportHash: hex.EncodeToString(digest[:]), PublisherVerified: true, IdentityVerified: true, BillingVerified: true, ModelVerified: true, EffortVerified: route.NoEffort || len(route.Efforts) > 0, AllCallsControlled: true, WorkspaceEnforced: true}
	verify := func(candidate routes.Evidence) bool {
		current, err := os.ReadFile(path)
		if err != nil || len(current) == 0 || len(current) > 64<<10 {
			return false
		}
		digest := sha256.Sum256(current)
		if hex.EncodeToString(digest[:]) != evidence.ReportHash {
			return false
		}
		file, err := decodeGLMEvidence(current)
		return err == nil && glmevidenceValid(file, route)
	}
	return evidence, verify, nil
}

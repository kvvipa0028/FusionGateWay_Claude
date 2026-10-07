package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/quota"
	"github.com/yetone/magpie/internal/fusion/routes"
	"github.com/yetone/magpie/internal/fusion/runtime/glm"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

// OpenGLMExecutionControl opens the GLM execution host from the operator's
// private evidence file: the exact declared route is admitted through the
// Registry with re-verification on every resolution, and every launch
// inspection performs a fresh read-only CN quota read. Incomplete upstream
// quota data blocks the launch (quota_unverified); no synthetic snapshot is
// ever substituted. Real generation spending stays gated by that live read.
func OpenGLMExecutionControl(parent context.Context, sourcePath, root, addr, projectID, routeID, keyPath, evidencePath, executable, executionRoot string) (*ControlHost, error) {
	if parent == nil || parent.Err() != nil || !declared(projectID) || !declared(routeID) || evidencePath == "" || !filepathAbs(executable) || !filepathAbs(executionRoot) {
		return nil, ErrControlHost
	}
	binary, err := os.ReadFile(executable)
	if err != nil || len(binary) == 0 {
		return nil, ErrControlHost
	}
	sum := sha256.Sum256(binary)
	if hex.EncodeToString(sum[:]) != glm.NativeExecutableSHA256 {
		return nil, ErrControlHost
	}
	source, err := Load(sourcePath)
	if err != nil {
		return nil, ErrControlHost
	}
	project, err := source.Project(projectID)
	if err != nil || !project.Read {
		return nil, ErrControlHost
	}
	var declared stageplan.Route
	matches := 0
	for _, r := range project.Configuration.Routes {
		if r.ID == routeID {
			declared = r
			matches++
		}
	}
	if matches != 1 || declared.BillingPath != "coding_plan" || declared.PluginVersion != nil || declared.RuntimeVersion != glm.CLIVersion {
		return nil, ErrControlHost
	}
	for _, p := range source.Projects() {
		if overlaps(p.Path, keyPath) || overlaps(p.Path, executionRoot) || overlaps(executionRoot, p.Path) {
			return nil, ErrControlHost
		}
	}
	credential, err := glm.NewFileCredential(keyPath, glm.FileCredentialScope{Account: declared.Account, Workspace: declared.Workspace, Identity: declared.CredentialIdentity})
	if err != nil {
		return nil, ErrControlHost
	}
	admitted := declared
	admitted.Admitted, admitted.BillingKnown, admitted.LockEnforcement, admitted.Capabilities = true, true, stageplan.ControlledCalls, []string{"text"}
	evidence, verify, err := LoadGLMEvidence(evidencePath, admitted)
	if err != nil {
		return nil, ErrControlHost
	}
	registry := routes.NewRegistry(verify)
	candidate := routes.Builtins()[2]
	candidate.ID = declared.ID
	if err := registry.Register(candidate); err != nil {
		return nil, ErrControlHost
	}
	if err := registry.Admit(admitted, evidence); err != nil {
		return nil, ErrControlHost
	}
	target := stageplan.ExecutionTarget{Route: stageplan.RouteRef{ID: declared.ID, Revision: declared.Revision}, Account: declared.Account, Workspace: declared.Workspace, CredentialIdentity: declared.CredentialIdentity, BillingPath: declared.BillingPath}
	identity := quota.Identity{Provider: "bigmodel", Account: declared.Account, Workspace: declared.Workspace, Region: "CN", Generation: source.Revision()}
	reader, err := glm.NewQuotaReader(glm.QuotaReaderConfig{Identity: identity, Target: target, QueryAllowed: func(ctx context.Context, i quota.Identity) bool {
		return ctx.Err() == nil && i == identity
	}, LoadCredential: credential.Load})
	if err != nil {
		return nil, ErrControlHost
	}
	inspect := func(ctx context.Context, task store.Task, role stageplan.Role, t stageplan.ExecutionTarget) (policy.Inspection, error) {
		if task.ProjectID != projectID || !glmFactoryRole(role) || (t.Route.ID != admitted.ID || t.Route.Revision != admitted.Revision) {
			return policy.Inspection{}, ErrControlHost
		}
		live, err := reader.Read(ctx, identity)
		if err != nil {
			return policy.Inspection{}, err
		}
		return policy.Inspection{Route: admitted, Provider: "bigmodel", QueryAdmitted: true, DataAllowed: true, SandboxVerified: true, VerificationAvailable: true, AllCallsCounted: true, ManagedExecutions: 1, ProofHash: evidence.ReportHash, Quota: live}, nil
	}
	factory, err := NewGLMRuntimeFactory(GLMRuntimeConfig{ProjectID: projectID, Route: stageplan.RouteRef{ID: declared.ID, Revision: declared.Revision}, Registry: registry, Executable: executable, CredentialPath: keyPath, ExecutionRoot: executionRoot, Timeout: 2 * time.Minute, Inspect: inspect})
	if err != nil {
		return nil, ErrControlHost
	}
	return OpenExecutionControl(parent, sourcePath, root, addr, factory)
}

func filepathAbs(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && len(path) < 4096
}

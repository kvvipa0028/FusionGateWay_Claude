package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/fusion/control"
	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/routes"
	"github.com/yetone/magpie/internal/fusion/runtime/codex"
	"github.com/yetone/magpie/internal/fusion/runtime/codexadapter"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
	"github.com/yetone/magpie/internal/fusion/workspace"
)

// CodexRuntimeConfig is trusted server wiring, never an HTTP/JSON DTO. Identity
// is an independently verified current account epoch; UpstreamAccount binds
// the official header without extracting JWT claims. Neither supplies rights.
// Registry and Inspect must prove actual subscription, data, sandbox and quota.
type CodexRuntimeConfig struct {
	ProjectID                                                  string
	Route                                                      stageplan.RouteRef
	Registry                                                   *routes.Registry
	Executable, CredentialPath, UpstreamAccount, ExecutionRoot string
	TestingWritePaths                                          []string
	Verification                                               *StageVerificationConfig
	Timeout                                                    time.Duration
	Identity                                                   func(context.Context) (codex.Identity, error)
	Inspect                                                    func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (policy.Inspection, error)
}

// NewCodexRuntimeFactory wires the private cache, fixed subscription endpoint
// and owned engineering pipeline. Startup performs no scheduling/model/query.
// Tool/write/resume capability remains governed by Adapter preflight; a writer
// is never converted into a readonly launch. This is not automatic admission.
func NewCodexRuntimeFactory(c CodexRuntimeConfig) (RuntimeFactory, error) {
	return newCodexRuntimeFactory(c, func(credential *codex.FileCredential) (codex.NativeForwarder, error) {
		return codex.NewSubscriptionForwarder(credential)
	})
}

// Only package tests may replace the upstream. No product URL/client/key option.
func newCodexRuntimeFactory(c CodexRuntimeConfig, buildForwarder func(*codex.FileCredential) (codex.NativeForwarder, error)) (RuntimeFactory, error) {
	c.TestingWritePaths = append([]string(nil), c.TestingWritePaths...)
	c.Verification = copyRuntimeValue(c.Verification)
	authRoot := filepath.Dir(filepath.Dir(filepath.Dir(c.CredentialPath)))
	if !declared(c.ProjectID) || !declared(c.Route.ID) || c.Route.Revision < 1 || c.Registry == nil || c.Identity == nil || c.Inspect == nil || buildForwarder == nil || !filepath.IsAbs(c.Executable) || filepath.Clean(c.Executable) != c.Executable || !utf8.ValidString(c.Executable) || strings.ContainsRune(c.Executable, 0) || workspace.PrivateState(c.ExecutionRoot) != nil || overlaps(c.ExecutionRoot, authRoot) || overlaps(authRoot, c.ExecutionRoot) || c.Timeout < 0 || c.Timeout > 4*time.Minute || !glmTestingPathsValid(c.TestingWritePaths) || !glmVerificationValid(c.Verification) {
		return nil, ErrControlHost
	}
	if c.Timeout == 0 {
		c.Timeout = 2 * time.Minute
	}
	rootInfo, err := os.Lstat(c.ExecutionRoot)
	if err != nil {
		return nil, ErrControlHost
	}
	rootCurrent := func() bool {
		live, err := os.Lstat(c.ExecutionRoot)
		return err == nil && os.SameFile(rootInfo, live) && workspace.PrivateState(c.ExecutionRoot) == nil
	}
	return func(ctx context.Context, e RuntimeEnvironment) (RuntimeRegistration, error) {
		fail := func() (RuntimeRegistration, error) { return RuntimeRegistration{}, ErrControlHost }
		if ctx == nil || ctx.Err() != nil || e.Store == nil || e.Manager == nil || e.Source == nil || e.Current == nil || e.Scheduler.Store != e.Store || e.Scheduler.Inspect == nil || !e.Current(c.ProjectID) || !rootCurrent() {
			return fail()
		}
		p, err := e.Source.Project(c.ProjectID)
		if err != nil || !p.Read || e.Source.nativeRoutes[c.Route] != "codex-chatgpt" {
			return fail()
		}
		candidate, err := c.Registry.Candidate(c.Route)
		if err != nil || candidate.Kind != "official_runtime" || candidate.Provider != "openai" || candidate.Runtime != "codex" || candidate.AuthMethod != "oauth" || candidate.AuthenticationOwner != "local_user" || candidate.Billing != "subscription" || candidate.Region == "unverified" {
			return fail()
		}
		route, err := c.Registry.Resolve(c.Route)
		if err != nil || route.RuntimeVersion != codex.CLIVersion || route.PluginVersion != nil || route.BillingPath != "subscription" || route.NoEffort || route.LockEnforcement != stageplan.ControlledCalls {
			return fail()
		}
		found := false
		for _, declared := range p.Configuration.Routes {
			if declared.ID == c.Route.ID && declared.Revision == c.Route.Revision && routeIdentityEqual(declared, route) {
				found = true
			}
		}
		if !found {
			return fail()
		}
		for _, project := range e.Source.Projects() {
			if overlaps(project.Path, c.ExecutionRoot) || overlaps(c.ExecutionRoot, project.Path) || overlaps(project.Path, authRoot) || overlaps(authRoot, project.Path) {
				return fail()
			}
		}
		identity, err := c.Identity(ctx)
		identityMatches := func(i codex.Identity) bool {
			return i.Generation >= 1 && i.Account == route.Account && i.Workspace == route.Workspace && i.CredentialIdentity == route.CredentialIdentity
		}
		if err != nil || !identityMatches(identity) {
			return fail()
		}
		credential, err := codex.NewFileCredential(c.CredentialPath, codex.FileCredentialScope{Route: c.Route, Account: route.Account, Workspace: route.Workspace, Identity: route.CredentialIdentity, UpstreamAccount: c.UpstreamAccount})
		if err != nil {
			return fail()
		}
		forwarder, err := buildForwarder(credential)
		if err != nil || forwarder == nil {
			return fail()
		}
		var closed atomic.Bool
		current := func(call context.Context, target stageplan.ExecutionTarget) bool {
			if call == nil || call.Err() != nil || ctx.Err() != nil || closed.Load() || !e.Current(c.ProjectID) || !rootCurrent() {
				return false
			}
			live, err := c.Registry.Resolve(c.Route)
			if err != nil || !reflect.DeepEqual(live, route) || !codexFactoryTarget(route, target) || codex.ValidateGatewayTarget(target) != nil {
				return false
			}
			epoch, err := c.Identity(call)
			if err != nil || epoch != identity {
				return false
			}
			if _, err := credential.Load(call, target); err != nil {
				return false
			}
			return call.Err() == nil && ctx.Err() == nil && !closed.Load() && e.Current(c.ProjectID) && rootCurrent()
		}
		inspect := func(call context.Context, task store.Task, role stageplan.Role, target stageplan.ExecutionTarget) (policy.Inspection, error) {
			if task.ProjectID != p.ID || !glmFactoryRole(role) || !current(call, target) {
				return policy.Inspection{}, control.ErrForbidden
			}
			in, err := c.Inspect(call, copyRuntimeValue(task), role, copyRuntimeValue(target))
			write := p.Write && (role == stageplan.Implementation || role == stageplan.Testing)
			q := in.Quota
			if err != nil || !current(call, target) || !reflect.DeepEqual(in.Route, route) || in.Provider != "openai" || q.Identity.Provider != "openai" || q.Identity.Account != identity.Account || q.Identity.Workspace != identity.Workspace || q.Identity.Region != candidate.Region || q.Identity.Generation != identity.Generation || q.Pool.Provider != "openai" || q.Pool.Region != candidate.Region || !write && in.WriteKey != "" || write && in.WriteKey == "" {
				return policy.Inspection{}, control.ErrForbidden
			}
			return copyRuntimeValue(in), nil
		}
		adapter, err := codexadapter.NewAdapter(codexadapter.AdapterConfig{Scheduler: e.Scheduler, Manager: e.Manager, Executable: c.Executable, Forwarder: forwarder, Identity: func(target stageplan.ExecutionTarget) codex.Identity {
			if !current(ctx, target) {
				return codex.Identity{}
			}
			return identity
		}, Current: func(b codex.Binding) bool { return b.Identity == identity && current(ctx, b.Target) }})
		if err != nil {
			return fail()
		}
		backend := watchStageExecution(control.BindAdapter(adapter), current)
		return stageExecutionRegistration(ctx, e, p, route, stageExecutionConfig{ProjectID: c.ProjectID, ExecutionRoot: c.ExecutionRoot, TestingWritePaths: c.TestingWritePaths, Verification: c.Verification, Timeout: c.Timeout}, current, func() bool { return ctx.Err() == nil && !closed.Load() && e.Current(c.ProjectID) }, inspect, backend, adapter.VerifyStop, func(id string, generation int64) (stageObservation, string, error) {
			out, text, err := adapter.Observation(id, generation)
			return stageObservation{State: out.State, SessionID: out.SessionID}, text, err
		}, func(context.Context) error { closed.Store(true); return nil }), nil
	}, nil
}

// codexFactoryTarget keeps the exact Compile reconstruction check while taking
// the admissible effort tiers from the admitted route itself: the official
// 0.160.0 effort catalogue is an open enumeration, so GLM's fixed tier list
// must not gate Codex targets.
func codexFactoryTarget(route stageplan.Route, target stageplan.ExecutionTarget) bool {
	effort := &stageplan.EffortSelection{Mode: target.Effort.RequestedMode}
	if target.Effort.Value != nil {
		effort.Value = *target.Effort.Value
	}
	tiers := map[string]bool{}
	for _, tier := range route.Efforts {
		tiers[tier] = true
	}
	if route.DefaultEffort != nil {
		tiers[*route.DefaultEffort] = true
	}
	if !tiers[effort.Value] {
		return false
	}
	if effort.Mode == stageplan.EffortDefault {
		// The frozen target records the resolved value. The configuration
		// compiler expects an empty requested value in default mode.
		effort.Value = ""
	}
	plan, err := stageplan.Compile(1, []stageplan.Role{stageplan.Design}, stageplan.Layer{Roles: map[stageplan.Role]stageplan.Binding{stageplan.Design: {Mode: stageplan.Locked, Route: &stageplan.RouteRef{ID: route.ID, Revision: route.Revision}, Model: route.Model, Effort: effort}}}, stageplan.Layer{}, stageplan.Layer{}, []stageplan.Route{route})
	return err == nil && reflect.DeepEqual(*plan.Bindings[stageplan.Design].Target, target)
}

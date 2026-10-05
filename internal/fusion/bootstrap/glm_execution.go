package bootstrap

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yetone/magpie/internal/fusion/control"
	"github.com/yetone/magpie/internal/fusion/handoff"
	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/routes"
	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/runtime/glm"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
	"github.com/yetone/magpie/internal/fusion/workspace"
)

// GLMRuntimeConfig is trusted product wiring, not an HTTP or private-JSON DTO.
// Registry must verify an independent stored admission report; Inspect must
// supply current account/quota/pool/data/sandbox evidence. Possession of a key
// or a successful diagnostic is insufficient. There is no URL/argv override.
type GLMRuntimeConfig struct {
	ProjectID      string
	Route          stageplan.RouteRef
	Registry       *routes.Registry
	Executable     string
	CredentialPath string
	ExecutionRoot  string
	Timeout        time.Duration
	Inspect        func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (policy.Inspection, error)
}

// NewGLMRuntimeFactory assembles the real Adapter, credential reader, fixed CN
// Transport and private workspace resolver. It grants no route admission and
// performs no outbound query, scheduling, launch or model call at startup.
// Until engineering Handoff is wired, it accepts only frozen single-role plans.
func NewGLMRuntimeFactory(c GLMRuntimeConfig) (RuntimeFactory, error) {
	return newGLMRuntimeFactory(c, glm.NewCNTransport())
}

// The alternate upstream is a package-private test seam. Product callers use
// NewGLMRuntimeFactory, which always selects the fixed native CN transport.
func newGLMRuntimeFactory(c GLMRuntimeConfig, transport http.RoundTripper) (RuntimeFactory, error) {
	if !declared(c.ProjectID) || !declared(c.Route.ID) || c.Route.Revision < 1 || c.Registry == nil || c.Inspect == nil || transport == nil || !filepath.IsAbs(c.Executable) || filepath.Clean(c.Executable) != c.Executable || strings.ContainsRune(c.Executable, 0) || workspace.PrivateState(c.ExecutionRoot) != nil || overlaps(c.ExecutionRoot, c.CredentialPath) || overlaps(c.CredentialPath, c.ExecutionRoot) || c.Timeout < 0 || c.Timeout > 4*time.Minute {
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
		info, err := os.Lstat(c.ExecutionRoot)
		return err == nil && os.SameFile(rootInfo, info) && workspace.PrivateState(c.ExecutionRoot) == nil
	}
	return func(ctx context.Context, e RuntimeEnvironment) (RuntimeRegistration, error) {
		fail := func() (RuntimeRegistration, error) { return RuntimeRegistration{}, ErrControlHost }
		if ctx == nil || ctx.Err() != nil || e.Source == nil || e.Store == nil || e.Manager == nil || e.Current == nil || e.Scheduler.Store != e.Store || e.Scheduler.Inspect == nil || !e.Current(c.ProjectID) || !rootCurrent() {
			return fail()
		}
		p, err := e.Source.Project(c.ProjectID)
		if err != nil || !p.Read {
			return fail()
		}
		candidate, err := c.Registry.Candidate(c.Route)
		if err != nil || candidate.Provider != "bigmodel" || candidate.Runtime != "claude" || candidate.Region != "CN" || candidate.AuthMethod != "api_key" || candidate.Billing != "coding_plan" || candidate.AuthenticationOwner != "local_user" {
			return fail()
		}
		route, err := c.Registry.Resolve(c.Route)
		if err != nil || route.RuntimeVersion != glm.CLIVersion || route.NoEffort || route.PluginVersion != nil || route.BillingPath != "coding_plan" {
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
			if overlaps(project.Path, c.ExecutionRoot) || overlaps(c.ExecutionRoot, project.Path) || overlaps(project.Path, c.CredentialPath) {
				return fail()
			}
		}
		credential, err := glm.NewFileCredential(c.CredentialPath, glm.FileCredentialScope{Account: route.Account, Workspace: route.Workspace, Identity: route.CredentialIdentity})
		if err != nil {
			return fail()
		}
		var closed atomic.Bool
		current := func(call context.Context, target stageplan.ExecutionTarget) bool {
			if call == nil || call.Err() != nil || ctx.Err() != nil || closed.Load() || !e.Current(c.ProjectID) || !rootCurrent() {
				return false
			}
			live, err := c.Registry.Resolve(c.Route)
			if err != nil || !reflect.DeepEqual(route, live) || !glmFactoryTarget(route, target) {
				return false
			}
			_, err = credential.Load(call, target)
			return err == nil && call.Err() == nil && ctx.Err() == nil && !closed.Load() && e.Current(c.ProjectID)
		}
		inspect := func(call context.Context, task store.Task, role stageplan.Role, target stageplan.ExecutionTarget) (policy.Inspection, error) {
			if task.ProjectID != p.ID || !glmFactoryRole(role) || !current(call, target) {
				return policy.Inspection{}, control.ErrForbidden
			}
			in, err := c.Inspect(call, copyRuntimeValue(task), role, copyRuntimeValue(target))
			write := p.Write && (role == stageplan.Implementation || role == stageplan.Testing)
			if err != nil || !current(call, target) || !reflect.DeepEqual(route, in.Route) || in.Provider != "bigmodel" || in.Quota.Identity.Region != "CN" || !write && in.WriteKey != "" || write && in.WriteKey == "" {
				return policy.Inspection{}, control.ErrForbidden
			}
			return copyRuntimeValue(in), nil
		}
		adapter, err := glm.NewAdapter(glm.AdapterConfig{Scheduler: e.Scheduler, Manager: e.Manager, Executable: c.Executable, LoadCredential: credential.Load, Current: func(b glm.Binding) bool { return current(ctx, b.Target) }, Transport: transport})
		if err != nil {
			return fail()
		}
		backend := control.BindAdapter(adapter)
		start := backend.Start
		backend.Start = func(parent context.Context, run store.StageRun, spec managed.Spec) (control.Execution, error) {
			owned, cancel := context.WithCancel(parent)
			done := make(chan struct{})
			// Source changes already have host/Controller fences. Independently
			// revoked route evidence, rotated credentials or replaced state
			// must also stop a Native process while its request is in flight.
			go func() {
				tick := time.NewTicker(100 * time.Millisecond)
				defer tick.Stop()
				for {
					select {
					case <-done:
						return
					case <-owned.Done():
						return
					case <-tick.C:
						if !current(owned, run.Target) {
							cancel()
							return
						}
					}
				}
			}()
			execution, err := start(owned, run, spec)
			if execution == nil {
				close(done)
				cancel()
				return nil, err
			}
			// Preserve any known handle on failure. Only its terminal Wait can
			// retire the watcher; Controller still owns stop proof and release.
			go func() {
				execution.Wait(context.Background())
				close(done)
				cancel()
			}()
			return execution, err
		}
		resolve := func(call context.Context, task store.Task, role stageplan.Role, target stageplan.ExecutionTarget) (control.Launch, error) {
			plan, err := e.Store.Plan(task.ID, task.PlanRevision)
			if err != nil || len(plan.RequiredRoles) != 1 || plan.RequiredRoles[0] != role {
				// A fresh source copy for each phase would silently throw away
				// implementation output. Require the future verified Handoff
				// resolver rather than pretending this is an engineering loop.
				return control.Launch{}, control.ErrUnsupported
			}
			in, err := inspect(call, task, role, target)
			if err != nil || !in.DataAllowed {
				return control.Launch{}, control.ErrForbidden
			}
			input, err := json.Marshal(struct {
				Role stageplan.Role `json:"role"`
				Goal string         `json:"goal"`
			}{role, task.Goal})
			if err != nil || len(input) > 64<<10 {
				return control.Launch{}, control.ErrUnsupported
			}
			// This directory and its copy belong only to this launch. Retain it
			// for later evidence/reconciliation; never delete an unknown run.
			launchRoot, err := os.MkdirTemp(c.ExecutionRoot, "launch-")
			if err != nil {
				return control.Launch{}, control.ErrForbidden
			}
			worker := filepath.Join(launchRoot, "worker")
			if os.Mkdir(worker, 0700) != nil {
				return control.Launch{}, control.ErrForbidden
			}
			snapshot, err := workspace.Copy(p.Path, launchRoot, "workspace")
			if err != nil || !current(call, target) {
				return control.Launch{}, control.ErrForbidden
			}
			guard, err := snapshot.Guard()
			if err != nil {
				return control.Launch{}, control.ErrForbidden
			}
			// This backend owns one launch and one artifact, never a shared
			// mutable last-workspace pointer across tasks or runs.
			local := backend
			var handle control.Execution
			var handleMu, releaseMu sync.Mutex
			var bundle handoff.Bundle
			published := false
			local.Start = func(ctx context.Context, run store.StageRun, spec managed.Spec) (control.Execution, error) {
				if run.TaskID != task.ID || run.Role != role || run.PlanRevision != plan.Revision || !reflect.DeepEqual(run.Target, target) {
					return nil, control.ErrIdentity
				}
				h, err := backend.Start(ctx, run, spec)
				handleMu.Lock()
				handle = h
				handleMu.Unlock()
				return h, err
			}
			local.Release = func(proof policy.StopProof) error {
				releaseMu.Lock()
				defer releaseMu.Unlock()
				if !adapter.VerifyStop(proof) {
					return control.ErrReconcile
				}
				handleMu.Lock()
				h := handle
				handleMu.Unlock()
				if h == nil {
					return control.ErrReconcile
				}
				result, err := h.Wait(context.Background())
				run, runErr := e.Store.Run(proof.RunID)
				if err != nil || runErr != nil || !result.StoppedVerified || result.Proof != proof || run.TaskID != task.ID || run.Role != role || run.PlanRevision != plan.Revision || run.Generation != proof.Generation || run.NativeSessionID != proof.NativeSessionID || !reflect.DeepEqual(run.Target, target) {
					return control.ErrReconcile
				}
				if run.State != "succeeded" {
					return backend.Release(proof)
				}
				identityCurrent := func() bool {
					live, err := e.Store.Task(task.ID)
					return err == nil && live.PlanRevision == plan.Revision && live.Generation == run.Generation && live.Goal == task.Goal && current(ctx, target) && guard.ValidFor(snapshot.Path)
				}
				if result.State != "succeeded" || !identityCurrent() {
					return control.ErrReconcile
				}
				raw, _ := json.Marshal(target)
				binding := handoff.Binding{TaskID: task.ID, PlanRevision: plan.Revision, PlanHash: plan.Hash, RunID: run.ID, Generation: run.Generation, Role: role, TargetHash: handoff.Hash(raw)}
				if !published {
					artifact, err := workspace.Freeze(snapshot, launchRoot, "handoff")
					if err != nil {
						return control.ErrReconcile
					}
					bundle, err = handoff.Publish(artifact, binding, task.Goal, handoff.Evidence{OutputHash: result.OutputHash, StopProofHash: proof.ReportHash})
					if err != nil {
						return control.ErrReconcile
					}
					published = true
				}
				if _, err := bundle.Read(binding); err != nil || !identityCurrent() {
					return control.ErrReconcile
				}
				// Publication failure retains reservations for reconciliation.
				// Durable artifact indexing/next-stage resolution is still pending.
				return backend.Release(proof)
			}
			return control.Launch{Backend: local, Spec: managed.Spec{Root: worker, Workspace: snapshot.Path, Source: guard, Input: input, Timeout: c.Timeout, Writable: p.Write && (role == stageplan.Implementation || role == stageplan.Testing)}}, nil
		}
		return RuntimeRegistration{Routes: map[string][]stageplan.Route{p.ID: {route}}, Inspect: inspect, Resolve: resolve, Close: func(context.Context) error { closed.Store(true); return nil }}, nil
	}, nil
}

func glmFactoryRole(role stageplan.Role) bool {
	for _, known := range stageplan.AllRoles() {
		if role == known {
			return true
		}
	}
	return false
}

func glmFactoryTarget(route stageplan.Route, target stageplan.ExecutionTarget) bool {
	effort := &stageplan.EffortSelection{Mode: target.Effort.RequestedMode}
	if target.Effort.Value != nil {
		effort.Value = *target.Effort.Value
	}
	if effort.Value != "low" && effort.Value != "medium" && effort.Value != "high" && effort.Value != "max" {
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

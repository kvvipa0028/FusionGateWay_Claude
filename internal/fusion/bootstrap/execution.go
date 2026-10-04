package bootstrap

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/yetone/magpie/internal/fusion/api"
	"github.com/yetone/magpie/internal/fusion/control"
	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/quota"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

// RuntimeFactory is trusted in-process wiring, never JSON, a CLI admission
// override, or a proof of real provider admission. It may initialize private
// services but must not launch jobs or invoke Scheduler before returning.
type RuntimeFactory func(context.Context, RuntimeEnvironment) (RuntimeRegistration, error)
type RuntimeEnvironment struct {
	Store     *store.Store
	Manager   *policy.Manager
	Source    *Loaded
	Scheduler policy.Scheduler
	Current   func(string) bool
}
type RuntimeRegistration struct {
	Routes       map[string][]stageplan.Route
	Inspect      func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (policy.Inspection, error)
	Resolve      func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (control.Launch, error)
	SelectAuto   func(context.Context, store.Task, stageplan.Role, stageplan.FrozenBinding) (stageplan.ExecutionTarget, error)
	QuotaSources map[string][]api.QuotaSource
	Close        func(context.Context) error
}

// OpenExecutionControl preserves the private registration identity and
// enables execution only through independently verified server services.
// The existing draft CLI deliberately does not call this entry point.
func OpenExecutionControl(parent context.Context, sourcePath, root, addr string, factory RuntimeFactory) (*ControlHost, error) {
	if parent == nil || parent.Err() != nil || factory == nil {
		return nil, ErrControlHost
	}
	return openControl(parent, sourcePath, root, addr, factory, false)
}

func (h *ControlHost) executionCurrent(projectID string) bool {
	h.mu.Lock()
	live := !h.closed && h.runtimeContext != nil && h.runtimeContext.Err() == nil
	h.mu.Unlock()
	return live && h.current() && h.source.Current(projectID)
}
func copyRuntimeValue[T any](v T) T {
	b, _ := json.Marshal(v)
	var out T
	_ = json.Unmarshal(b, &out)
	return out
}
func routeIdentityEqual(declared, admitted stageplan.Route) bool {
	admitted.Admitted = declared.Admitted
	admitted.BillingKnown = declared.BillingKnown
	admitted.LockEnforcement = declared.LockEnforcement
	admitted.Capabilities = declared.Capabilities
	return reflect.DeepEqual(declared, admitted)
}
func (h *ControlHost) registeredRoute(projectID string, ref stageplan.RouteRef) (stageplan.Route, bool) {
	h.mu.Lock()
	reg := h.runtime
	h.mu.Unlock()
	if reg != nil {
		for _, r := range reg.Routes[projectID] {
			if r.ID == ref.ID && r.Revision == ref.Revision {
				return copyRuntimeValue(r), true
			}
		}
	}
	return stageplan.Route{}, false
}
func (h *ControlHost) inspectExecution(ctx context.Context, t store.Task, role stageplan.Role, target stageplan.ExecutionTarget) (policy.Inspection, error) {
	if ctx.Err() != nil || !h.executionCurrent(t.ProjectID) {
		return policy.Inspection{}, control.ErrForbidden
	}
	p, e := h.source.Project(t.ProjectID)
	r, ok := h.registeredRoute(t.ProjectID, target.Route)
	if e != nil || !p.Read || !ok {
		return policy.Inspection{}, control.ErrForbidden
	}
	h.mu.Lock()
	reg := h.runtime
	h.mu.Unlock()
	in, e := reg.Inspect(ctx, copyRuntimeValue(t), role, copyRuntimeValue(target))
	if ctx.Err() != nil || !h.executionCurrent(t.ProjectID) || !reflect.DeepEqual(r, in.Route) || !p.Write && in.WriteKey != "" {
		return policy.Inspection{}, control.ErrForbidden
	}
	return copyRuntimeValue(in), e
}
func (h *ControlHost) resolveExecution(ctx context.Context, t store.Task, role stageplan.Role, target stageplan.ExecutionTarget) (control.Launch, error) {
	if ctx.Err() != nil || !h.executionCurrent(t.ProjectID) {
		return control.Launch{}, control.ErrForbidden
	}
	p, e := h.source.Project(t.ProjectID)
	_, ok := h.registeredRoute(t.ProjectID, target.Route)
	if e != nil || !p.Read || !ok {
		return control.Launch{}, control.ErrForbidden
	}
	h.mu.Lock()
	reg := h.runtime
	h.mu.Unlock()
	l, e := reg.Resolve(ctx, copyRuntimeValue(t), role, copyRuntimeValue(target))
	if e != nil || ctx.Err() != nil || !h.executionCurrent(t.ProjectID) || !l.Spec.Source.BoundToSource(p.Path) || !l.Spec.Source.ValidFor(l.Spec.Workspace) || l.Spec.Writable && !p.Write {
		return control.Launch{}, control.ErrForbidden
	}
	for _, registered := range h.source.projects {
		if overlaps(l.Spec.Root, registered.project.Path) || overlaps(registered.project.Path, l.Spec.Root) {
			return control.Launch{}, control.ErrForbidden
		}
	}
	l.Spec.Input = append([]byte(nil), l.Spec.Input...)
	return l, nil
}
func (h *ControlHost) installRuntime(parent context.Context, s *api.Server, f RuntimeFactory, queryOnly bool) error {
	h.runtimeContext, h.runtimeCancel = context.WithCancel(parent)
	scheduler := policy.Scheduler{Store: h.store, Inspect: h.inspectExecution}
	reg, e := f(h.runtimeContext, RuntimeEnvironment{Store: h.store, Manager: h.auth, Source: h.source, Scheduler: scheduler, Current: h.executionCurrent})
	// Retain cleanup even when validation or factory initialization fails.
	reg.Routes = copyRuntimeValue(reg.Routes)
	h.mu.Lock()
	h.runtime = &reg
	h.mu.Unlock()
	if e != nil || h.runtimeContext.Err() != nil {
		return ErrControlHost
	}
	if queryOnly {
		if len(reg.Routes) != 0 || reg.Inspect != nil || reg.Resolve != nil || reg.SelectAuto != nil || len(reg.QuotaSources) == 0 {
			return ErrControlHost
		}
	} else if reg.Inspect == nil || reg.Resolve == nil || len(reg.Routes) == 0 {
		return ErrControlHost
	}
	for id, routes := range reg.Routes {
		p, e := h.source.Project(id)
		if e != nil || len(routes) == 0 || len(routes) > len(p.Configuration.Routes) {
			return ErrControlHost
		}
		seen := map[string]bool{}
		for _, r := range routes {
			if seen[r.ID] || !r.Admitted || !r.BillingKnown || len(r.Capabilities) == 0 || r.LockEnforcement != stageplan.ControlledCalls && r.LockEnforcement != stageplan.PrimaryOnly {
				return ErrControlHost
			}
			seen[r.ID] = true
			found := false
			for i, declared := range p.Configuration.Routes {
				if declared.ID == r.ID && routeIdentityEqual(declared, r) {
					p.Configuration.Routes[i] = r
					found = true
					break
				}
			}
			if !found {
				return ErrControlHost
			}
		}
		if s.SetProject(id, p.Configuration) != nil {
			return ErrControlHost
		}
	}
	for id, sources := range reg.QuotaSources {
		if !h.executionCurrent(id) || queryOnly && len(sources) == 0 {
			return ErrControlHost
		}
		wrapped := make([]api.QuotaSource, len(sources))
		for i, source := range sources {
			if source.Current == nil {
				return ErrControlHost
			}
			wrapped[i] = source
			wrapped[i].Current = func(ctx context.Context, q quota.Identity) bool {
				return ctx.Err() == nil && h.executionCurrent(id) && source.Current(ctx, q) && h.executionCurrent(id)
			}
			if source.Fetch != nil {
				wrapped[i].Fetch = func(ctx context.Context, q quota.Identity) (quota.Snapshot, error) {
					h.mu.Lock()
					if h.closed {
						h.mu.Unlock()
						return quota.Snapshot{}, control.ErrClosed
					}
					h.services.Add(1)
					h.mu.Unlock()
					defer h.services.Done()
					if ctx.Err() != nil || !h.executionCurrent(id) || !source.Current(ctx, q) || !h.executionCurrent(id) {
						return quota.Snapshot{}, control.ErrForbidden
					}
					owned, cancel := context.WithCancel(ctx)
					stop := context.AfterFunc(h.runtimeContext, cancel)
					defer stop()
					defer cancel()
					v, e := source.Fetch(owned, q)
					if owned.Err() != nil || !h.executionCurrent(id) || !source.Current(owned, q) || !h.executionCurrent(id) {
						return quota.Snapshot{}, control.ErrForbidden
					}
					return v, e
				}
			}
		}
		if s.SetQuotaSources(id, wrapped) != nil {
			return ErrControlHost
		}
	}
	if queryOnly {
		return nil
	}
	selectAuto := reg.SelectAuto
	if selectAuto != nil {
		selectAuto = func(ctx context.Context, t store.Task, role stageplan.Role, b stageplan.FrozenBinding) (stageplan.ExecutionTarget, error) {
			if !h.executionCurrent(t.ProjectID) {
				return stageplan.ExecutionTarget{}, control.ErrForbidden
			}
			v, e := reg.SelectAuto(ctx, copyRuntimeValue(t), role, copyRuntimeValue(b))
			if ctx.Err() != nil || !h.executionCurrent(t.ProjectID) {
				return stageplan.ExecutionTarget{}, control.ErrForbidden
			}
			return copyRuntimeValue(v), e
		}
	}
	h.controller, e = control.New(h.runtimeContext, control.Config{RequireSource: true, Scheduler: scheduler, Resolve: h.resolveExecution, SelectAuto: selectAuto})
	if e != nil || s.SetController(h.controller) != nil {
		return ErrControlHost
	}
	return nil
}

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yetone/magpie/internal/fusion/control"
	"github.com/yetone/magpie/internal/fusion/quota"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

// QuotaSource is trusted bootstrap wiring. Current must be a local,
// nonblocking check of exact quota-purpose authority and credential identity;
// Fetch must independently recheck those conditions at its outbound boundary.
// Neither callback is accepted from HTTP or proves generation admission.
type QuotaSource struct {
	Route    stageplan.RouteRef
	Identity quota.Identity
	Current  func(context.Context, quota.Identity) bool
	Fetch    quota.Fetch
}
type quotaRegistration struct {
	mu       sync.Mutex
	revision int64
	broker   *quota.Broker
	sources  map[string]QuotaSource
	failures map[string]quota.Status
}
type RouteQuota struct {
	Route    stageplan.RouteRef `json:"route"`
	Identity *quota.Identity    `json:"identity,omitempty"`
	Status   quota.Status       `json:"status"`
	Snapshot *quota.Snapshot    `json:"snapshot,omitempty"`
	Error    *controlError      `json:"error,omitempty"`
}
type QuotaReply struct {
	ConfigurationRevision int64            `json:"configuration_revision"`
	Routes                []RouteQuota     `json:"routes"`
	Pools                 []quota.PoolView `json:"pools"`
}

// Keep the Broker's cancellation/deadline while making the already verified
// issuer context available to the trusted reader's outbound authorization.
type quotaQueryContext struct {
	context.Context
	authority context.Context
}

func (c quotaQueryContext) Value(key any) any { return c.authority.Value(key) }

// SetQuotaSources registers one immutable catalogue per project configuration
// revision. A new SetProject revision requires a new registration, never a
// silent replacement of a live source or reuse of its cached observations.
func (s *Server) SetQuotaSources(projectID string, sources []QuotaSource) error {
	if s == nil || !opaque(projectID) || len(sources) > 256 {
		return errInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.projects[projectID]
	if !ok {
		return errProject
	}
	if old := s.quotaProjects[projectID]; old != nil && old.revision == p.revision {
		return store.ErrConflict
	}
	reg := &quotaRegistration{revision: p.revision, broker: quota.NewBroker(10 * time.Second), sources: map[string]QuotaSource{}, failures: map[string]quota.Status{}}
	for _, source := range sources {
		if !opaque(source.Route.ID) || source.Route.Revision < 1 || source.Current == nil {
			return errInvalid
		}
		if _, exists := reg.sources[source.Route.ID]; exists {
			return errInvalid
		}
		matched := false
		for _, route := range p.configuration.Routes {
			if route.ID == source.Route.ID && route.Revision == source.Route.Revision && route.Account == source.Identity.Account && route.Workspace == source.Identity.Workspace {
				matched = true
				break
			}
		}
		if !matched || reg.broker.SetIdentity(source.Identity) != nil {
			return errInvalid
		}
		reg.sources[source.Route.ID] = source
	}
	if s.quotaProjects == nil {
		s.quotaProjects = map[string]*quotaRegistration{}
	}
	s.quotaProjects[projectID] = reg
	return nil
}
func (s *Server) quotaConfiguration(projectID string) (project, *quotaRegistration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.projects[projectID]
	if !ok {
		return project{}, nil, store.ErrNotFound
	}
	return p, s.quotaProjects[projectID], nil
}
func (s *Server) quotaCurrent(projectID string, reg *quotaRegistration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return reg != nil && s.quotaProjects[projectID] == reg && s.projects[projectID].revision == reg.revision
}
func publishedQuota(snapshot quota.Snapshot, now time.Time) quota.Snapshot {
	snapshot.Status = quota.State(snapshot, now, time.Minute)
	switch snapshot.Status {
	case quota.Available, quota.Zero, quota.Unknown, quota.Stale, quota.Unverified, quota.AuthRequired, quota.Unsupported:
	default:
		snapshot.Status = quota.Unknown
	}
	return snapshot
}
func (s *Server) quotaReply(ctx context.Context, projectID string, p project, reg *quotaRegistration, refreshError string) QuotaReply {
	out := QuotaReply{ConfigurationRevision: p.revision, Routes: []RouteQuota{}, Pools: []quota.PoolView{}}
	snapshots := []quota.Snapshot{}
	for _, route := range p.configuration.Routes {
		view := RouteQuota{Route: stageplan.RouteRef{ID: route.ID, Revision: route.Revision}, Status: quota.Unsupported}
		if reg != nil {
			source, ok := reg.sources[route.ID]
			if ok {
				if !s.quotaCurrent(projectID, reg) || source.Route != view.Route || !source.Current(ctx, source.Identity) {
					view.Status = quota.Unverified
				} else {
					i := source.Identity
					view.Identity = &i
					if source.Fetch != nil {
						reg.mu.Lock()
						view.Status = reg.failures[route.ID]
						reg.mu.Unlock()
						if view.Status == "" {
							view.Status = quota.Unknown
						}
						if snapshot, e := reg.broker.Current(i); e == nil {
							snapshot = publishedQuota(snapshot, s.now())
							view.Status = snapshot.Status
							view.Snapshot = &snapshot
							snapshots = append(snapshots, snapshot)
						}
					}
				}
			}
		}
		if refreshError == route.ID {
			view.Error = &controlError{Code: "quota_query_failed", Message: "Quota refresh unavailable"}
		}
		out.Routes = append(out.Routes, view)
	}
	sort.Slice(out.Routes, func(i, j int) bool { return out.Routes[i].Route.ID < out.Routes[j].Route.ID })
	if len(snapshots) > 0 {
		out.Pools = quota.GroupPools(snapshots)
	}
	return out
}
func quotaFailure(w http.ResponseWriter, e error) {
	code, reason := 0, ""
	switch {
	case errors.Is(e, store.ErrConflict):
		code, reason = 409, "quota_source_changed"
	case errors.Is(e, control.ErrUnsupported):
		code, reason = 503, "quota_query_unsupported"
	case errors.Is(e, store.ErrNotFound):
		code, reason = 404, "quota_record_unavailable"
	default:
		controlFailure(w, e)
		return
	}
	respond(w, code, map[string]any{"error": controlError{Code: reason, Message: http.StatusText(code)}})
}
func (s *Server) quotaControl(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/control/v1/projects/"), "/")
	list := len(parts) == 2 && parts[1] == "quota"
	refresh := len(parts) == 4 && parts[1] == "quota" && parts[3] == "refresh" && opaque(parts[2])
	if (!list && !refresh) || !opaque(parts[0]) {
		quotaFailure(w, errInvalid)
		return
	}
	if list && r.Method != http.MethodGet || refresh && r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", 405)
		return
	}
	if refresh {
		var body struct{}
		if read(r, &body) != nil {
			quotaFailure(w, errInvalid)
			return
		}
	}
	p, reg, e := s.quotaConfiguration(parts[0])
	if e != nil {
		quotaFailure(w, e)
		return
	}
	if !s.auth.ManagementCurrent(r.Context()) {
		quotaFailure(w, control.ErrForbidden)
		return
	}
	failed := ""
	if refresh {
		// Resolve only server-registered route identity; no URL, token,
		// generation, account or workspace can be selected by the body.
		found := false
		for _, route := range p.configuration.Routes {
			if route.ID == parts[2] {
				found = true
				break
			}
		}
		if !found {
			quotaFailure(w, store.ErrNotFound)
			return
		}
		if reg == nil || !s.quotaCurrent(parts[0], reg) {
			quotaFailure(w, store.ErrConflict)
			return
		}
		source, ok := reg.sources[parts[2]]
		if !ok || source.Fetch == nil {
			quotaFailure(w, control.ErrUnsupported)
			return
		}
		if !source.Current(r.Context(), source.Identity) {
			quotaFailure(w, store.ErrConflict)
			return
		}
		// Broker owns its bounded query lifetime. Preserve the issuer's
		// grant values without letting the first HTTP disconnect cancel a
		// coalesced query; revocation is still checked before/after Fetch.
		authority := context.WithoutCancel(r.Context())
		_, e = reg.broker.Refresh(r.Context(), source.Identity, func(ctx context.Context, i quota.Identity) (quota.Snapshot, error) {
			ctx = quotaQueryContext{Context: ctx, authority: authority}
			live := func() bool {
				return ctx.Err() == nil && s.auth.ManagementCurrent(authority) && s.quotaCurrent(parts[0], reg) && source.Current(ctx, i)
			}
			if !live() {
				return quota.Snapshot{}, control.ErrForbidden
			}
			select {
			case s.quotaSlots <- struct{}{}:
				defer func() { <-s.quotaSlots }()
			default:
				return quota.Snapshot{}, quota.ErrBusy
			}
			snapshot, err := source.Fetch(ctx, i)
			if !live() {
				return quota.Snapshot{}, control.ErrForbidden
			}
			if err == nil {
				raw, marshalError := json.Marshal(snapshot)
				if marshalError != nil || len(raw) > 128<<10 || len(snapshot.Windows) > 32 || len(snapshot.Resources) > 32 {
					return quota.Snapshot{}, quota.ErrMalformed
				}
			}
			return snapshot, err
		})
		if !s.auth.ManagementCurrent(r.Context()) {
			quotaFailure(w, control.ErrForbidden)
			return
		}
		if !s.quotaCurrent(parts[0], reg) || !source.Current(r.Context(), source.Identity) {
			quotaFailure(w, store.ErrConflict)
			return
		}
		if errors.Is(e, quota.ErrBusy) {
			respond(w, 429, map[string]any{"error": controlError{Code: "quota_refresh_capacity_reached", Message: http.StatusText(429)}})
			return
		}
		if e != nil {
			failed = parts[2]
			status := quota.Unknown
			var queryError *quota.QueryError
			if errors.As(e, &queryError) && (queryError.Status == quota.AuthRequired || queryError.Status == quota.Unsupported) {
				status = queryError.Status
			}
			reg.mu.Lock()
			reg.failures[parts[2]] = status
			reg.mu.Unlock()
		}
	}
	out := s.quotaReply(r.Context(), parts[0], p, reg, failed)
	if !s.auth.ManagementCurrent(r.Context()) {
		quotaFailure(w, control.ErrForbidden)
		return
	}
	// A configuration replacement while building the view cannot publish
	// an old catalogue as the current project configuration.
	current, _, e := s.quotaConfiguration(parts[0])
	if e != nil || current.revision != p.revision {
		quotaFailure(w, store.ErrConflict)
		return
	}
	respond(w, 200, out)
}

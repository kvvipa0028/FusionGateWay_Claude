package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/fusion/control"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

type DefaultLayerInput struct {
	Layer *stageplan.Layer `json:"layer"`
}
type DefaultLayerView struct {
	Scope      string           `json:"scope"`
	ID         string           `json:"id"`
	Revision   int64            `json:"revision"`
	Configured bool             `json:"configured"`
	Layer      *stageplan.Layer `json:"layer"`
	Hash       *string          `json:"hash"`
	CreatedAt  *time.Time       `json:"created_at"`
}

func defaultView(d store.DefaultLayer) DefaultLayerView {
	return DefaultLayerView{Scope: d.Scope, ID: d.ID, Revision: d.Revision, Configured: true, Layer: &d.Layer, Hash: &d.Hash, CreatedAt: &d.CreatedAt}
}
func applyDefaults(p project, d store.DefaultLayers) project {
	p.configuration.Global = p.baseGlobal
	p.configuration.Project = p.baseProject
	if d.Global != nil {
		p.configuration.Global = d.Global.Layer
	}
	if d.Project != nil {
		p.configuration.Project = d.Project.Layer
	}
	p.defaults = d.Stamp()
	return p
}

// Caller holds Server.mu. Read the latest pair even when another API instance
// wrote it; native route registration has a separate trusted revision.
func (s *Server) currentProjectLocked(id string) (project, error) {
	p, ok := s.projects[id]
	if !ok {
		return project{}, errProject
	}
	d, e := s.store.DefaultLayers(id)
	if e != nil {
		return project{}, e
	}
	if p.defaults != d.Stamp() {
		if p.revision == int64(1<<63-1) {
			return project{}, errCapacity
		}
		p = applyDefaults(p, d)
		p.revision++
		s.projects[id] = p
	}
	return p, nil
}
func defaultsPath(path string) bool {
	if path == "/control/v1/defaults/global" || strings.HasPrefix(path, "/control/v1/defaults/global/") {
		return true
	}
	parts := strings.Split(strings.TrimPrefix(path, "/control/v1/projects/"), "/")
	return strings.HasPrefix(path, "/control/v1/projects/") && len(parts) > 1 && parts[1] == "defaults"
}
func defaultFailure(w http.ResponseWriter, e error) {
	code, reason := 0, ""
	switch {
	case errors.Is(e, errPresetPrecondition):
		code, reason = 428, "defaults_precondition_required"
	case errors.Is(e, store.ErrConflict):
		code, reason = 412, "defaults_revision_conflict"
	case errors.Is(e, store.ErrDefaultsCapacity):
		code, reason = 429, "defaults_capacity_reached"
	case errors.Is(e, store.ErrNotFound), errors.Is(e, errProject):
		code, reason = 404, "defaults_record_unavailable"
	default:
		controlFailure(w, e)
		return
	}
	respond(w, code, map[string]any{"error": controlError{Code: reason, Message: http.StatusText(code)}})
}
func (s *Server) defaultRegistryLocked(scope, id string) ProjectConfiguration {
	if scope == "project" {
		return s.projects[id].configuration
	}
	// Remove globally ambiguous references instead of selecting an account or
	// workspace merely because one project happened to be iterated first.
	seen := map[stageplan.RouteRef]string{}
	routes := map[stageplan.RouteRef]stageplan.Route{}
	ambiguous := map[stageplan.RouteRef]bool{}
	for _, p := range s.projects {
		for _, route := range p.configuration.Routes {
			ref := stageplan.RouteRef{ID: route.ID, Revision: route.Revision}
			raw, _ := json.Marshal(route)
			if old, ok := seen[ref]; ok && old != string(raw) {
				ambiguous[ref] = true
			}
			seen[ref] = string(raw)
			routes[ref] = route
		}
	}
	out := ProjectConfiguration{}
	for ref, route := range routes {
		if !ambiguous[ref] {
			out.Routes = append(out.Routes, route)
		}
	}
	return out
}
func (s *Server) saveDefaults(scope, id string, base int64, in DefaultLayerInput, r *http.Request) (store.DefaultLayerReceipt, error) {
	if in.Layer == nil {
		return store.DefaultLayerReceipt{}, errInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if scope == "project" {
		if _, ok := s.projects[id]; !ok {
			return store.DefaultLayerReceipt{}, errProject
		}
	}
	_, e := s.store.DefaultLayer(scope, id, base+1)
	if errors.Is(e, store.ErrNotFound) {
		if !knownPresetRoutes(*in.Layer, s.defaultRegistryLocked(scope, id)) {
			return store.DefaultLayerReceipt{}, errInvalid
		}
	} else if e != nil {
		return store.DefaultLayerReceipt{}, e
	}
	if !s.auth.ManagementCurrent(r.Context()) {
		return store.DefaultLayerReceipt{}, control.ErrForbidden
	}
	return s.store.SaveDefaultLayer(scope, id, base, *in.Layer)
}
func (s *Server) defaultsControl(w http.ResponseWriter, r *http.Request) {
	scope, id, tail := "global", "global", strings.TrimPrefix(r.URL.Path, "/control/v1/defaults/global")
	if strings.HasPrefix(r.URL.Path, "/control/v1/projects/") {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/control/v1/projects/"), "/")
		if len(parts) < 2 || !opaque(parts[0]) || parts[1] != "defaults" {
			defaultFailure(w, errInvalid)
			return
		}
		scope, id = "project", parts[0]
		tail = ""
		if len(parts) > 2 {
			tail = "/" + strings.Join(parts[2:], "/")
		}
		s.mu.Lock()
		_, ok := s.projects[id]
		s.mu.Unlock()
		if !ok {
			defaultFailure(w, errProject)
			return
		}
	}
	var revision int64
	if tail != "" {
		parts := strings.Split(tail, "/")
		if len(parts) != 3 || parts[1] != "versions" {
			defaultFailure(w, errInvalid)
			return
		}
		n, e := strconv.ParseInt(parts[2], 10, 64)
		if e != nil || n < 1 || strconv.FormatInt(n, 10) != parts[2] {
			defaultFailure(w, errInvalid)
			return
		}
		revision = n
	}
	if tail == "" && r.Method == http.MethodPut {
		base, e := presetBase(r)
		if e != nil {
			defaultFailure(w, e)
			return
		}
		var in DefaultLayerInput
		if read(r, &in) != nil {
			defaultFailure(w, errInvalid)
			return
		}
		out, e := s.saveDefaults(scope, id, base, in, r)
		if e != nil {
			defaultFailure(w, e)
			return
		}
		w.Header().Set("ETag", etag(out.DefaultLayer.Revision))
		path := "/control/v1/defaults/global"
		if scope == "project" {
			path = "/control/v1/projects/" + url.PathEscape(id) + "/defaults"
		}
		w.Header().Set("Location", path+"/versions/"+strconv.FormatInt(out.DefaultLayer.Revision, 10))
		code := 200
		if out.Created {
			code = 201
		}
		respond(w, code, out)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", 405)
		return
	}
	d, e := s.store.DefaultLayer(scope, id, revision)
	var out DefaultLayerView
	if errors.Is(e, store.ErrNotFound) && revision == 0 {
		out = DefaultLayerView{Scope: scope, ID: id}
	} else if e != nil {
		defaultFailure(w, e)
		return
	} else {
		out = defaultView(d)
	}
	if !s.auth.ManagementCurrent(r.Context()) {
		defaultFailure(w, control.ErrForbidden)
		return
	}
	w.Header().Set("ETag", etag(out.Revision))
	respond(w, 200, out)
}

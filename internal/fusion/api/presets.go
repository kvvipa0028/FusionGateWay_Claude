package api

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/yetone/magpie/internal/fusion/control"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

type PresetSelection struct {
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
}

func projectSettingsPath(path string) bool {
	const prefix = "/control/v1/projects/"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	parts := strings.SplitN(strings.TrimPrefix(path, prefix), "/", 3)
	return len(parts) > 1 && (parts[1] == "presets" || parts[1] == "configuration")
}

var errPresetPrecondition = errors.New("preset precondition required")

func presetBase(r *http.Request) (int64, error) {
	v := r.Header.Values("If-Match")
	if len(v) == 0 {
		return 0, errPresetPrecondition
	}
	if len(v) != 1 || len(v[0]) < 3 || len(v[0]) > 21 || v[0][0] != '"' || v[0][len(v[0])-1] != '"' {
		return 0, errInvalid
	}
	text := v[0][1 : len(v[0])-1]
	n, e := strconv.ParseInt(text, 10, 64)
	if e != nil || n < 0 || n == int64(1<<63-1) || strconv.FormatInt(n, 10) != text {
		return 0, errInvalid
	}
	return n, nil
}
func presetFailure(w http.ResponseWriter, e error) {
	code, reason := 0, ""
	switch {
	case errors.Is(e, errPresetPrecondition):
		code, reason = 428, "preset_precondition_required"
	case errors.Is(e, store.ErrConflict):
		code, reason = 412, "preset_revision_conflict"
	case errors.Is(e, store.ErrPresetCapacity):
		code, reason = 429, "preset_capacity_reached"
	case errors.Is(e, errProject), errors.Is(e, store.ErrNotFound):
		code, reason = 404, "preset_record_unavailable"
	default:
		controlFailure(w, e)
		return
	}
	respond(w, code, map[string]any{"error": controlError{Code: reason, Message: http.StatusText(code)}})
}
func knownPresetRoutes(in stageplan.Layer, c ProjectConfiguration) bool {
	l, e := stageplan.ExpandLayer(in)
	if e != nil {
		return false
	}
	registry := map[stageplan.RouteRef]string{}
	for _, route := range c.Routes {
		registry[stageplan.RouteRef{ID: route.ID, Revision: route.Revision}] = route.Model
	}
	for _, b := range l.Roles {
		if b.Mode == stageplan.Locked && registry[*b.Route] != b.Model {
			return false
		}
		for _, candidate := range b.Candidates {
			if registry[candidate.Route] != candidate.Model {
				return false
			}
		}
	}
	return true
}
func (s *Server) savePreset(projectID, id string, base int64, in store.PresetInput, r *http.Request) (store.PresetReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.projects[projectID]
	if !ok {
		return store.PresetReceipt{}, errProject
	}
	// A retry can read its immutable old version even after that route is no
	// longer in the current registry. New saves must name registered models.
	_, e := s.store.Preset(projectID, id, base+1)
	if errors.Is(e, store.ErrNotFound) {
		if !knownPresetRoutes(in.Layer, p.configuration) {
			return store.PresetReceipt{}, errInvalid
		}
	} else if e != nil {
		return store.PresetReceipt{}, e
	}
	if !s.auth.ManagementCurrent(r.Context()) {
		return store.PresetReceipt{}, control.ErrForbidden
	}
	return s.store.SavePreset(projectID, id, base, in)
}
func (s *Server) presetControl(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/control/v1/projects/"), "/")
	if len(parts) < 2 || !opaque(parts[0]) {
		presetFailure(w, errInvalid)
		return
	}
	s.mu.Lock()
	p, e := s.currentProjectLocked(parts[0])
	s.mu.Unlock()
	if e != nil {
		presetFailure(w, e)
		return
	}
	if parts[1] == "configuration" && len(parts) == 2 {
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", 405)
			return
		}
		if !s.auth.ManagementCurrent(r.Context()) {
			presetFailure(w, control.ErrForbidden)
			return
		}
		w.Header().Set("ETag", etag(p.revision))
		respond(w, 200, struct {
			ProjectID     string               `json:"project_id"`
			Revision      int64                `json:"revision"`
			Configuration ProjectConfiguration `json:"configuration"`
		}{parts[0], p.revision, p.configuration})
		return
	}
	if parts[1] != "presets" || (len(parts) != 2 && len(parts) != 3 && len(parts) != 5) || len(parts) > 2 && !opaque(parts[2]) {
		presetFailure(w, errInvalid)
		return
	}
	if len(parts) == 2 {
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", 405)
			return
		}
		list, e := s.store.Presets(parts[0])
		if e != nil {
			presetFailure(w, e)
			return
		}
		if !s.auth.ManagementCurrent(r.Context()) {
			presetFailure(w, control.ErrForbidden)
			return
		}
		respond(w, 200, map[string]any{"presets": list})
		return
	}
	if len(parts) == 3 && r.Method == http.MethodPut {
		base, e := presetBase(r)
		if e != nil {
			presetFailure(w, e)
			return
		}
		var in store.PresetInput
		if read(r, &in) != nil {
			presetFailure(w, errInvalid)
			return
		}
		out, e := s.savePreset(parts[0], parts[2], base, in, r)
		if e != nil {
			presetFailure(w, e)
			return
		}
		w.Header().Set("ETag", etag(out.Preset.Revision))
		w.Header().Set("Location", "/control/v1/projects/"+url.PathEscape(parts[0])+"/presets/"+url.PathEscape(parts[2])+"/versions/"+strconv.FormatInt(out.Preset.Revision, 10))
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
	var version int64
	if len(parts) == 5 {
		if parts[3] != "versions" {
			presetFailure(w, errInvalid)
			return
		}
		n, e := strconv.ParseInt(parts[4], 10, 64)
		if e != nil || n < 1 || strconv.FormatInt(n, 10) != parts[4] {
			presetFailure(w, errInvalid)
			return
		}
		version = n
	}
	out, e := s.store.Preset(parts[0], parts[2], version)
	if e != nil {
		presetFailure(w, e)
		return
	}
	if !s.auth.ManagementCurrent(r.Context()) {
		presetFailure(w, control.ErrForbidden)
		return
	}
	w.Header().Set("ETag", etag(out.Revision))
	respond(w, 200, out)
}
func (s *Server) taskPreset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", 405)
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/control/v1/tasks/"), "/preset")
	if !opaque(id) {
		presetFailure(w, errInvalid)
		return
	}
	ref, e := s.store.TaskPreset(id)
	if e != nil {
		presetFailure(w, e)
		return
	}
	if !s.auth.ManagementCurrent(r.Context()) {
		presetFailure(w, control.ErrForbidden)
		return
	}
	respond(w, 200, map[string]any{"preset": ref})
}

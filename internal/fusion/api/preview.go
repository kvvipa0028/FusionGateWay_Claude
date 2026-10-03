// Package api exposes authenticated task controls. Preview and submit never
// launch a Runtime; execution admission remains the scheduler's responsibility.
package api

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/fusion/control"
	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

const maxBody = 128 << 10

var errInvalid = errors.New("invalid task request")
var errPreview = errors.New("task preview expired or changed")
var errProject = errors.New("project unavailable")
var errCapacity = errors.New("task preview capacity reached")

// ProjectConfiguration's registry/budget/bootstrap come from the trusted
// controller. HTTP callers can persist separate global/project binding layers,
// but cannot supply this whole object or change its route authority.
type ProjectConfiguration struct {
	Global        stageplan.Layer   `json:"global"`
	Project       stageplan.Layer   `json:"project"`
	Routes        []stageplan.Route `json:"routes"`
	DefaultBudget *BudgetLimits     `json:"default_budget,omitempty"`
}

// BudgetLimits accepts limits only. Used counters belong to the persistent
// scheduler and cannot be supplied by a task caller.
type BudgetLimits struct {
	MaxCalls   int `json:"max_calls"`
	MaxReworks int `json:"max_reworks"`
}

func validLimits(b BudgetLimits) bool {
	return b.MaxCalls >= 1 && b.MaxCalls <= 1000 && b.MaxReworks >= 0 && b.MaxReworks <= 1
}

type project struct {
	configuration           ProjectConfiguration
	revision                int64
	routeRevision           int64
	baseGlobal, baseProject stageplan.Layer
	defaults                store.DefaultStamp
}
type PreviewRequest struct {
	ProjectID     string           `json:"project_id"`
	Goal          string           `json:"goal"`
	RequiredRoles []stageplan.Role `json:"required_roles"`
	Task          stageplan.Layer  `json:"task,omitempty"`
	Budget        *BudgetLimits    `json:"budget,omitempty"`
	Preset        *PresetSelection `json:"preset,omitempty"`
}
type Preview struct {
	ID                    string             `json:"preview_id"`
	ConfigurationRevision int64              `json:"configuration_revision"`
	ExpiresAt             time.Time          `json:"expires_at"`
	Plan                  stageplan.Snapshot `json:"plan"`
	Budget                BudgetLimits       `json:"budget"`
	Preset                *store.PresetRef   `json:"preset,omitempty"`
}
type SubmitRequest struct {
	PreviewID string `json:"preview_id"`
	PlanHash  string `json:"plan_hash"`
}
type receipt struct {
	preview   Preview
	request   PreviewRequest
	committed store.Task
	key       string
	taskID    string
	base      int64
	applied   bool
	defaults  store.DefaultStamp
}
type Server struct {
	mu            sync.Mutex
	store         *store.Store
	auth          *policy.Manager
	projects      map[string]project
	previews      map[string]*receipt
	now           func() time.Time
	eventPoll     time.Duration
	eventSlots    chan struct{}
	controller    *control.Controller
	quotaProjects map[string]*quotaRegistration
	quotaSlots    chan struct{}
}

func New(st *store.Store, auth *policy.Manager) (*Server, error) {
	if st == nil || auth == nil {
		return nil, errInvalid
	}
	return &Server{store: st, auth: auth, projects: map[string]project{}, previews: map[string]*receipt{}, now: time.Now, eventPoll: time.Second, eventSlots: make(chan struct{}, 8), quotaSlots: make(chan struct{}, 8)}, nil
}
func opaque(s string) bool {
	return s != "" && len(s) <= 256 && !strings.ContainsAny(s, "/\\") && !strings.ContainsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

// SetProject replaces the entire trusted configuration. Even an equal value is
// a new revision, invalidating uncommitted previews without rewriting old tasks.
func (s *Server) SetProject(id string, c ProjectConfiguration) error {
	if !opaque(id) || len(c.Routes) > 256 {
		return errInvalid
	}
	raw, e := json.Marshal(c)
	if e != nil || len(raw) > 1<<20 {
		return errInvalid
	}
	var copied ProjectConfiguration
	if json.Unmarshal(raw, &copied) != nil {
		return errInvalid
	}
	if copied.DefaultBudget == nil {
		copied.DefaultBudget = &BudgetLimits{MaxCalls: 50, MaxReworks: 1}
	}
	if !validLimits(*copied.DefaultBudget) {
		return errInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.projects[id]
	if old.revision == 0 && len(s.projects) >= 128 || old.revision == int64(1<<63-1) || old.routeRevision == int64(1<<63-1) {
		return errCapacity
	}
	d, e := s.store.DefaultLayers(id)
	if e != nil {
		return e
	}
	s.projects[id] = applyDefaults(project{configuration: copied, baseGlobal: copied.Global, baseProject: copied.Project, revision: old.revision + 1, routeRevision: old.routeRevision + 1}, d)
	return nil
}
func copyPreview(p Preview) Preview {
	raw, _ := json.Marshal(p)
	var copied Preview
	json.Unmarshal(raw, &copied)
	return copied
}
func (s *Server) preview(in PreviewRequest) (Preview, error) {
	if !opaque(in.ProjectID) || in.Goal == "" || len(in.Goal) > 65536 {
		return Preview{}, errInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, e := s.currentProjectLocked(in.ProjectID)
	if e != nil {
		return Preview{}, e
	}
	var presetRef *store.PresetRef
	if in.Preset != nil {
		if !opaque(in.Preset.ID) || in.Preset.Revision < 1 {
			return Preview{}, errInvalid
		}
		preset, e := s.store.Preset(in.ProjectID, in.Preset.ID, in.Preset.Revision)
		if e != nil {
			return Preview{}, e
		}
		in.Task, e = stageplan.ApplyPreset(preset.Layer, in.Task)
		if e != nil {
			return Preview{}, errInvalid
		}
		presetRef = &store.PresetRef{ID: preset.ID, Revision: preset.Revision, Hash: preset.Hash}
	}
	budget := *p.configuration.DefaultBudget
	if in.Budget != nil {
		budget = *in.Budget
	}
	if !validLimits(budget) {
		return Preview{}, errInvalid
	}
	// Parse through the same role/group validator as configuration import. Global
	// and project layers are never taken from the caller's body.
	typed := stageplan.Input{SchemaVersion: 1, Revision: 1, RequiredRoles: in.RequiredRoles, Global: p.configuration.Global, Project: p.configuration.Project, Task: in.Task}
	raw, e := json.Marshal(typed)
	if e != nil {
		return Preview{}, errInvalid
	}
	validated, e := stageplan.ParsePlan(raw)
	if e != nil {
		return Preview{}, errInvalid
	}
	plan, e := stageplan.Compile(1, validated.RequiredRoles, validated.Global, validated.Project, validated.Task, p.configuration.Routes)
	if e != nil {
		return Preview{}, e
	}
	now := s.now()
	for id, v := range s.previews {
		if !now.Before(v.preview.ExpiresAt) {
			delete(s.previews, id)
		}
	}
	if len(s.previews) >= 1024 {
		return Preview{}, errCapacity
	}
	var entropy [32]byte
	if _, e = rand.Read(entropy[:]); e != nil {
		return Preview{}, e
	}
	out := Preview{ID: "preview-" + hex.EncodeToString(entropy[:]), ConfigurationRevision: p.revision, ExpiresAt: now.Add(5 * time.Minute), Plan: plan, Budget: budget, Preset: presetRef}
	s.previews[out.ID] = &receipt{preview: copyPreview(out), request: PreviewRequest{ProjectID: in.ProjectID, Goal: in.Goal}, defaults: p.defaults}
	return out, nil
}
func (s *Server) submit(in SubmitRequest, key string) (store.Task, error) {
	if !opaque(in.PreviewID) || !opaque(key) || in.PlanHash == "" {
		return store.Task{}, errInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.previews[in.PreviewID]
	if !ok || r.taskID != "" || r.preview.Plan.Hash != in.PlanHash {
		return store.Task{}, errPreview
	}
	// Retry of an already committed receipt returns its task, even if global
	// configuration changed. It neither recompiles nor starts a new attempt.
	if r.committed.ID != "" {
		if r.key != key {
			return store.Task{}, errPreview
		}
		return s.store.Task(r.committed.ID)
	}
	budget := store.Budget{MaxCalls: r.preview.Budget.MaxCalls, MaxReworks: r.preview.Budget.MaxReworks}
	request := store.CreateRequest{ProjectID: r.request.ProjectID, Goal: r.request.Goal, Plan: r.preview.Plan, Budget: &budget, Preset: r.preview.Preset}
	readCommitted := func() (store.Task, error) {
		task, e := s.store.LookupCreation(key, request)
		if e == nil {
			r.committed, r.key = task, key
		}
		return task, e
	}
	// A peer may have committed this exact request. Read its durable receipt
	// before checking eligibility to create a new task.
	if task, e := readCommitted(); !errors.Is(e, store.ErrNotFound) {
		return task, e
	}
	p, e := s.currentProjectLocked(r.request.ProjectID)
	if e != nil || !s.now().Before(r.preview.ExpiresAt) || p.revision != r.preview.ConfigurationRevision || p.defaults != r.defaults {
		// The peer can commit between the first lookup and the current check.
		if task, lookupErr := readCommitted(); !errors.Is(lookupErr, store.ErrNotFound) {
			return task, lookupErr
		}
		if e != nil {
			return store.Task{}, e
		}
		return store.Task{}, errPreview
	}
	task, e := s.store.CreateCurrent(key, request, r.defaults)
	if errors.Is(e, store.ErrDefaultsChanged) {
		return store.Task{}, errPreview
	}
	if e != nil {
		return store.Task{}, e
	}
	r.committed = task
	r.key = key
	return task, nil
}
func failure(w http.ResponseWriter, e error) {
	code := http.StatusInternalServerError
	reason := "internal_error"
	switch {
	case errors.Is(e, errIfMatchMissing):
		code = 428
		reason = "plan_precondition_required"
	case errors.Is(e, errPlanConflict):
		code = 409
		reason = "plan_revision_or_stage_conflict"
	case errors.Is(e, errInvalid), errors.Is(e, store.ErrInvalid):
		code = 400
		reason = "invalid_request"
	case errors.Is(e, errProject), errors.Is(e, store.ErrNotFound):
		code = 404
		reason = "record_unavailable"
	case errors.Is(e, errPreview), errors.Is(e, store.ErrConflict):
		code = 409
		reason = "preview_expired_or_changed"
		if errors.Is(e, store.ErrConflict) {
			reason = "idempotency_conflict"
		}
	case errors.Is(e, stageplan.ErrInvalidPlan):
		code = 422
		reason = "plan_not_admitted"
		// Only constant compiler reason names are public. Never return the
		// arbitrary error text of a store, Runtime or injected dependency.
		candidate := strings.TrimPrefix(e.Error(), stageplan.ErrInvalidPlan.Error()+": ")
		switch candidate {
		case "route_revision_not_admitted", "route_identity_or_version_unknown", "billing_or_model_unverified", "call_lock_scope_insufficient", "required_capability_missing", "none_not_admitted", "default_effort_undisclosed", "effort_unsupported", "plugin_version_unknown":
			reason = candidate
		}
	case errors.Is(e, errCapacity):
		code = 429
		reason = "preview_capacity_reached"
	case errors.Is(e, errEventCapacity):
		code = 429
		reason = "event_capacity_reached"
	case errors.Is(e, errCursor):
		code = 409
		reason = "event_cursor_ahead"
	case errors.Is(e, errStreamTransport):
		code = 503
		reason = "stream_transport_unsupported"
	}
	// Never echo native errors, rejected JSON or caller-provided credentials.
	respond(w, code, map[string]any{"error": map[string]string{"code": reason, "message": http.StatusText(code)}})
}
func respond(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(value)
}
func (s *Server) Handler() http.Handler {
	return s.auth.Management(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.URL.RawQuery != "" {
			failure(w, errInvalid)
			return
		}
		switch {
		case defaultsPath(r.URL.Path):
			s.defaultsControl(w, r)
		case projectSettingsPath(r.URL.Path):
			s.presetControl(w, r)
		case strings.HasPrefix(r.URL.Path, "/control/v1/tasks/") && strings.HasSuffix(r.URL.Path, "/preset"):
			s.taskPreset(w, r)
		case strings.HasPrefix(r.URL.Path, "/control/v1/projects/") && (strings.HasSuffix(r.URL.Path, "/quota") || strings.HasSuffix(r.URL.Path, "/refresh")):
			s.quotaControl(w, r)
		case strings.HasPrefix(r.URL.Path, "/control/v1/tasks/") && (strings.HasSuffix(r.URL.Path, "/pause") || strings.HasSuffix(r.URL.Path, "/continue") || strings.HasSuffix(r.URL.Path, "/cancel") && strings.Count(r.URL.Path, "/") == 5):
			s.taskControl(w, r)
		case strings.HasPrefix(r.URL.Path, "/control/v1/tasks/") && (strings.HasSuffix(r.URL.Path, "/start") || strings.HasSuffix(r.URL.Path, "/resume") || strings.HasSuffix(r.URL.Path, "/cancel")):
			s.executionControl(w, r)
		case strings.HasPrefix(r.URL.Path, "/control/v1/tasks/") && (strings.HasSuffix(r.URL.Path, "/plan") || strings.HasSuffix(r.URL.Path, "/plan/preview")):
			s.planControl(w, r)
		case r.URL.Path == "/control/v1/tasks/preview":
			if r.Method != http.MethodPost {
				http.Error(w, "Method Not Allowed", 405)
				return
			}
			var in PreviewRequest
			if read(r, &in) != nil {
				failure(w, errInvalid)
				return
			}
			p, e := s.preview(in)
			if e != nil {
				failure(w, e)
				return
			}
			respond(w, 200, p)
		case r.URL.Path == "/agent/v1/tasks":
			if r.Method != http.MethodPost {
				http.Error(w, "Method Not Allowed", 405)
				return
			}
			values := r.Header.Values("Idempotency-Key")
			if len(values) != 1 {
				failure(w, errInvalid)
				return
			}
			var in SubmitRequest
			if read(r, &in) != nil {
				failure(w, errInvalid)
				return
			}
			task, e := s.submit(in, values[0])
			if e != nil {
				failure(w, e)
				return
			}
			respond(w, 201, task)
		case strings.HasPrefix(r.URL.Path, "/control/v1/tasks/") && strings.HasSuffix(r.URL.Path, "/budget"):
			if r.Method != http.MethodGet {
				http.Error(w, "Method Not Allowed", 405)
				return
			}
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/control/v1/tasks/"), "/budget")
			if !opaque(id) {
				failure(w, errInvalid)
				return
			}
			budget, e := s.store.Budget(id)
			if e != nil {
				failure(w, e)
				return
			}
			respond(w, 200, budget)
		case strings.HasPrefix(r.URL.Path, "/agent/v1/tasks/") && strings.HasSuffix(r.URL.Path, "/events"):
			s.events(w, r)
		case strings.HasPrefix(r.URL.Path, "/agent/v1/tasks/") && strings.Contains(r.URL.Path, "/runs/"):
			s.readRun(w, r)
		case strings.HasPrefix(r.URL.Path, "/agent/v1/tasks/"):
			if r.Method != http.MethodGet {
				http.Error(w, "Method Not Allowed", 405)
				return
			}
			id := strings.TrimPrefix(r.URL.Path, "/agent/v1/tasks/")
			if !opaque(id) {
				failure(w, errInvalid)
				return
			}
			task, e := s.store.Task(id)
			if e != nil {
				failure(w, e)
				return
			}
			w.Header().Set("ETag", taskETag(task))
			respond(w, 200, task)
		default:
			http.NotFound(w, r)
		}
	}))
}
func read(r *http.Request, out any) error {
	if r.Body == nil || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Content-Encoding") != "" {
		return errInvalid
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if e != nil || len(raw) == 0 || len(raw) > maxBody || !utf8.Valid(raw) {
		return errInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if unique(d, 0) != nil {
		return errInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return errInvalid
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		return errInvalid
	}
	return nil
}

// JSON field names are canonical lower-case ASCII protocol names. Checking every nested
// object also prevents Go's case-insensitive struct decode from merging aliases.
func unique(d *json.Decoder, depth int) error {
	if depth > 32 {
		return errInvalid
	}
	t, e := d.Token()
	if e != nil || t == nil {
		return errInvalid
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, e := d.Token()
			name, ok := key.(string)
			if e != nil || !ok || name == "" || seen[name] {
				return errInvalid
			}
			for _, c := range name {
				if c != '_' && (c < 'a' || c > 'z') {
					return errInvalid
				}
			}
			seen[name] = true
			if unique(d, depth+1) != nil {
				return errInvalid
			}
		}
		t, e = d.Token()
		if e != nil || t != json.Delim('}') {
			return errInvalid
		}
	case '[':
		for d.More() {
			if unique(d, depth+1) != nil {
				return errInvalid
			}
		}
		t, e = d.Token()
		if e != nil || t != json.Delim(']') {
			return errInvalid
		}
	default:
		return errInvalid
	}
	return nil
}

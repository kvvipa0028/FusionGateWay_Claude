package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/yetone/magpie/internal/fusion/control"
	"github.com/yetone/magpie/internal/fusion/store"
)

// SubmissionView exposes only the original UI submission, not a CreateRequest
// or authority-bearing workspace/credential configuration.
type SubmissionView struct {
	ProjectID string  `json:"project_id"`
	Goal      string  `json:"goal"`
	Key       string  `json:"key"`
	State     string  `json:"state"`
	TaskID    string  `json:"task_id"`
	Preview   Preview `json:"preview"`
}
type SubmissionReply struct {
	Submission *SubmissionView `json:"submission"`
}

func submissionView(v store.SubmissionJournal) (*SubmissionView, error) {
	d := v.Draft
	// UI previews always freeze explicit limits. A generic Store draft without
	// them is not a UI preview; do not invent limits or clear its pending record.
	if d.Request.Budget == nil {
		return nil, errPreview
	}
	return &SubmissionView{ProjectID: d.Request.ProjectID, Goal: d.Request.Goal, Key: d.Key, State: v.State, TaskID: v.TaskID, Preview: Preview{
		ID: d.PreviewID, ConfigurationRevision: d.ConfigurationRevision, ExpiresAt: d.ExpiresAt, Plan: d.Request.Plan,
		Budget: BudgetLimits{MaxCalls: d.Request.Budget.MaxCalls, MaxReworks: d.Request.Budget.MaxReworks}, Preset: d.Request.Preset,
	}}, nil
}
func submissionPath(path string) bool {
	if !strings.HasPrefix(path, "/control/v1/projects/") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(path, "/control/v1/projects/"), "/")
	return len(parts) > 1 && parts[1] == "submission"
}

// The caller holds s.mu, has authenticated current Management and registered
// the path's project. Replaying history cannot recreate a missing preview.
func (s *Server) prepareSubmissionLocked(in store.SubmissionIdentity) (store.SubmissionJournal, error) {
	if old, e := s.store.LookupSubmissionJournal(in); !errors.Is(e, store.ErrNotFound) {
		return old, e
	}
	r, ok := s.previews[in.PreviewID]
	if !ok || r.taskID != "" || r.request.ProjectID != in.ProjectID || r.preview.Plan.Hash != in.PlanHash {
		return store.SubmissionJournal{}, errPreview
	}
	p, e := s.currentProjectLocked(in.ProjectID)
	if e != nil {
		return store.SubmissionJournal{}, e
	}
	if !s.now().Before(r.preview.ExpiresAt) || p.revision != r.preview.ConfigurationRevision || p.defaults != r.defaults {
		return store.SubmissionJournal{}, errPreview
	}
	budget := store.Budget{MaxCalls: r.preview.Budget.MaxCalls, MaxReworks: r.preview.Budget.MaxReworks}
	request := store.CreateRequest{ProjectID: in.ProjectID, Goal: r.request.Goal, Plan: r.preview.Plan, Budget: &budget, Preset: r.preview.Preset}
	out, e := s.store.PrepareSubmission(store.SubmissionDraft{PreviewID: in.PreviewID, Key: in.Key, Request: request, ExpiresAt: r.preview.ExpiresAt, ConfigurationRevision: r.preview.ConfigurationRevision}, r.defaults)
	return out.Submission, e
}

func (s *Server) submissionControl(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/control/v1/projects/"), "/")
	if len(parts) < 2 || !opaque(parts[0]) || len(parts) > 3 || len(parts) == 3 && parts[2] != "acknowledge" && parts[2] != "abandon" {
		failure(w, errInvalid)
		return
	}
	if r.Method != "POST" && !(r.Method == "GET" && len(parts) == 2) {
		allow := "POST"
		if len(parts) == 2 {
			allow = "GET, POST"
		}
		w.Header().Set("Allow", allow)
		http.Error(w, "Method Not Allowed", 405)
		return
	}
	var in SubmitRequest
	key := ""
	if r.Method == "POST" {
		values := r.Header.Values("Idempotency-Key")
		if len(values) != 1 || !opaque(values[0]) || read(r, &in) != nil || !opaque(in.PreviewID) || in.PlanHash == "" {
			failure(w, errInvalid)
			return
		}
		key = values[0]
	}
	s.mu.Lock()
	if !s.auth.ManagementCurrent(r.Context()) {
		s.mu.Unlock()
		controlFailure(w, control.ErrForbidden)
		return
	}
	if _, ok := s.projects[parts[0]]; !ok {
		s.mu.Unlock()
		failure(w, errProject)
		return
	}
	var value store.SubmissionJournal
	var e error
	if r.Method == "GET" {
		value, e = s.store.PendingSubmission(parts[0])
	} else {
		identity := store.SubmissionIdentity{ProjectID: parts[0], PreviewID: in.PreviewID, Key: key, PlanHash: in.PlanHash}
		if len(parts) == 2 {
			value, e = s.prepareSubmissionLocked(identity)
		} else {
			value, e = s.store.LookupSubmissionJournal(identity)
			if e == nil {
				_, e = submissionView(value)
			}
			if e == nil {
				value, e = s.store.ResolveSubmission(identity, parts[2])
			}
		}
	}
	s.mu.Unlock()
	// No draft, key, task or existence detail is emitted after authority loss.
	if !s.auth.ManagementCurrent(r.Context()) {
		controlFailure(w, control.ErrForbidden)
		return
	}
	if r.Method == "GET" && errors.Is(e, store.ErrNotFound) {
		respond(w, 200, SubmissionReply{})
		return
	}
	if errors.Is(e, store.ErrConflict) || errors.Is(e, store.ErrDefaultsChanged) {
		e = errPreview
	}
	if e != nil {
		failure(w, e)
		return
	}
	view, e := submissionView(value)
	if e != nil {
		failure(w, e)
		return
	}
	respond(w, 200, SubmissionReply{Submission: view})
}

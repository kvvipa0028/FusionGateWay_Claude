package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

var errIfMatchMissing = errors.New("plan precondition required")
var errPlanConflict = errors.New("plan revision or started stage conflict")

type RevisionRequest struct {
	Task stageplan.Layer `json:"task"`
}

func ifMatch(r *http.Request) (int64, error) {
	values := r.Header.Values("If-Match")
	if len(values) == 0 {
		return 0, errIfMatchMissing
	}
	if len(values) != 1 {
		return 0, errInvalid
	}
	v := values[0]
	if len(v) < 3 || v[0] != '"' || v[len(v)-1] != '"' {
		return 0, errInvalid
	}
	v = v[1 : len(v)-1]
	n, e := strconv.ParseInt(v, 10, 64)
	if e != nil || n < 1 || strconv.FormatInt(n, 10) != v {
		return 0, errInvalid
	}
	return n, nil
}
func etag(n int64) string { return `"` + strconv.FormatInt(n, 10) + `"` }

func planError(e error) error {
	if errors.Is(e, store.ErrConflict) {
		return errPlanConflict
	}
	return e
}

func (s *Server) previewRevision(id string, base int64, in RevisionRequest) (Preview, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, e := s.store.Task(id)
	if e != nil {
		return Preview{}, e
	}
	if task.PlanRevision != base {
		return Preview{}, errPlanConflict
	}
	c, e := s.currentProjectLocked(task.ProjectID)
	if e != nil {
		return Preview{}, e
	}
	old, e := s.store.Plan(id, base)
	if e != nil {
		return Preview{}, e
	}
	plan, e := stageplan.CompileRevision(old, in.Task, c.configuration.Global, c.configuration.Project, c.configuration.Routes)
	if e != nil {
		return Preview{}, e
	}
	if e = s.store.ValidateRevision(id, base, plan); e != nil {
		return Preview{}, planError(e)
	}
	b, e := s.store.Budget(id)
	if e != nil {
		return Preview{}, e
	}
	now := s.now()
	for key, v := range s.previews {
		if !now.Before(v.preview.ExpiresAt) {
			delete(s.previews, key)
		}
	}
	if len(s.previews) >= 1024 {
		return Preview{}, errCapacity
	}
	var entropy [32]byte
	if _, e = rand.Read(entropy[:]); e != nil {
		return Preview{}, e
	}
	p := Preview{ID: "preview-" + hex.EncodeToString(entropy[:]), ConfigurationRevision: c.revision, ExpiresAt: now.Add(5 * time.Minute), Plan: plan, Budget: BudgetLimits{MaxCalls: b.MaxCalls, MaxReworks: b.MaxReworks}}
	s.previews[p.ID] = &receipt{preview: copyPreview(p), request: PreviewRequest{ProjectID: task.ProjectID}, taskID: id, base: base, defaults: c.defaults}
	return p, nil
}

func (s *Server) applyRevision(id string, base int64, in SubmitRequest) (stageplan.Snapshot, error) {
	if !opaque(in.PreviewID) || in.PlanHash == "" {
		return stageplan.Snapshot{}, errInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.previews[in.PreviewID]
	if !ok || r.taskID != id || r.base != base || r.preview.Plan.Hash != in.PlanHash {
		return stageplan.Snapshot{}, errPreview
	}
	if r.applied {
		// Return the exact committed revision, even if newer revisions exist.
		// This receipt never resets counters, starts work, or writes a new event.
		return copyPreview(r.preview).Plan, nil
	}
	p, e := s.currentProjectLocked(r.request.ProjectID)
	if e != nil {
		return stageplan.Snapshot{}, e
	}
	if !s.now().Before(r.preview.ExpiresAt) || p.revision != r.preview.ConfigurationRevision || p.defaults != r.defaults {
		return stageplan.Snapshot{}, errPreview
	}
	if e := s.store.RevisePlanCurrent(id, base, r.preview.Plan, r.defaults); e != nil {
		if errors.Is(e, store.ErrDefaultsChanged) {
			return stageplan.Snapshot{}, errPreview
		}
		return stageplan.Snapshot{}, planError(e)
	}
	r.applied = true
	return copyPreview(r.preview).Plan, nil
}

func (s *Server) planControl(w http.ResponseWriter, r *http.Request) {
	preview := strings.HasSuffix(r.URL.Path, "/plan/preview")
	suffix := "/plan"
	if preview {
		suffix += "/preview"
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/control/v1/tasks/"), suffix)
	if !opaque(id) {
		failure(w, errInvalid)
		return
	}
	if !preview && r.Method == http.MethodGet {
		task, e := s.store.Task(id)
		if e != nil {
			failure(w, e)
			return
		}
		plan, e := s.store.Plan(id, task.PlanRevision)
		if e != nil {
			failure(w, e)
			return
		}
		w.Header().Set("ETag", etag(plan.Revision))
		respond(w, 200, plan)
		return
	}
	if preview && r.Method != http.MethodPost || !preview && r.Method != http.MethodPut {
		http.Error(w, "Method Not Allowed", 405)
		return
	}
	base, e := ifMatch(r)
	if e != nil {
		failure(w, e)
		return
	}
	if preview {
		var in RevisionRequest
		if read(r, &in) != nil {
			failure(w, errInvalid)
			return
		}
		p, e := s.previewRevision(id, base, in)
		if e != nil {
			failure(w, e)
			return
		}
		w.Header().Set("ETag", etag(base))
		respond(w, 200, p)
		return
	}
	var in SubmitRequest
	if read(r, &in) != nil {
		failure(w, errInvalid)
		return
	}
	plan, e := s.applyRevision(id, base, in)
	if e != nil {
		failure(w, e)
		return
	}
	w.Header().Set("ETag", etag(plan.Revision))
	respond(w, 200, plan)
}

package api

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/yetone/magpie/internal/fusion/control"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

// StartRequestView contains original metadata, never execution admission or
// a replacement current Task. The key is a selector, not a credential.
type StartRequestView struct {
	Key   string             `json:"key"`
	Task  store.Task         `json:"task"`
	Plan  stageplan.Snapshot `json:"plan"`
	Role  stageplan.Role     `json:"role"`
	ETag  string             `json:"etag"`
	State string             `json:"state"`
	RunID string             `json:"run_id"`
}
type StartRequestReply struct {
	Request *StartRequestView `json:"request"`
}
type AcknowledgeStartRequest struct {
	Role  stageplan.Role `json:"role"`
	RunID string         `json:"run_id"`
}

func startRequestView(j store.StartJournal) *StartRequestView {
	d := j.Draft
	return &StartRequestView{Key: d.Key, Task: d.Task, Plan: d.Plan, Role: d.Identity.Role, ETag: taskETag(d.Task), State: j.State, RunID: j.RunID}
}
func startJournalPath(path string) bool {
	for _, prefix := range []string{"/control/v1/projects/", "/control/v1/tasks/"} {
		if strings.HasPrefix(path, prefix) {
			parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
			return len(parts) > 1 && parts[1] == "start-request"
		}
	}
	return false
}
func (s *Server) startJournalAuthority(r *http.Request) error {
	if !s.auth.ManagementCurrent(r.Context()) {
		return control.ErrForbidden
	}
	return r.Context().Err()
}

// No controller, preview, model request or current binding compilation is used.
func (s *Server) startJournalControl(w http.ResponseWriter, r *http.Request) {
	if e := s.startJournalAuthority(r); e != nil {
		controlFailure(w, e)
		return
	}
	project := strings.HasPrefix(r.URL.Path, "/control/v1/projects/")
	prefix := "/control/v1/tasks/"
	if project {
		prefix = "/control/v1/projects/"
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, prefix), "/")
	if len(parts) < 2 || !opaque(parts[0]) || len(parts) > 3 || project && len(parts) != 2 || len(parts) == 3 && parts[2] != "acknowledge" && parts[2] != "abandon" {
		controlFailure(w, errInvalid)
		return
	}
	allowed := r.Method == "GET" && len(parts) == 2 || !project && r.Method == "POST"
	if !allowed {
		allow := "POST"
		if len(parts) == 2 {
			allow = "GET, POST"
		}
		if project {
			allow = "GET"
		}
		w.Header().Set("Allow", allow)
		http.Error(w, "Method Not Allowed", 405)
		return
	}
	var condition taskCondition
	var key string
	var in AcknowledgeStartRequest
	if !project {
		var e error
		condition, e = taskIfMatch(r)
		if e != nil {
			controlFailure(w, e)
			return
		}
		values := r.Header.Values("Idempotency-Key")
		if len(values) != 1 || !opaque(values[0]) {
			controlFailure(w, errInvalid)
			return
		}
		key = values[0]
		if r.Method == "POST" {
			if len(parts) == 3 && parts[2] == "acknowledge" {
				if read(r, &in) != nil || !validRole(in.Role) || !opaque(in.RunID) {
					controlFailure(w, errInvalid)
					return
				}
			} else {
				var role StartRoleRequest
				if read(r, &role) != nil || !validRole(role.Role) {
					controlFailure(w, errInvalid)
					return
				}
				in.Role = role.Role
			}
		}
	}
	if r.Method == "GET" && r.Body != nil {
		body, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(body) != 0 {
			controlFailure(w, errInvalid)
			return
		}
	}
	s.mu.Lock()
	value, e := s.startJournalLocked(r, parts, project, key, condition, in)
	s.mu.Unlock()
	// A write may have committed before authority loss: disclose no receipt, and
	// let a later authorized read reconcile the same original request.
	if authErr := s.startJournalAuthority(r); authErr != nil {
		controlFailure(w, authErr)
		return
	}
	if project && e == nil && value.Draft.Key == "" {
		respond(w, 200, StartRequestReply{})
		return
	}
	if e != nil {
		controlFailure(w, e)
		return
	}
	respond(w, 200, StartRequestReply{Request: startRequestView(value)})
}
func (s *Server) startJournalLocked(r *http.Request, parts []string, project bool, key string, condition taskCondition, in AcknowledgeStartRequest) (store.StartJournal, error) {
	if e := s.startJournalAuthority(r); e != nil {
		return store.StartJournal{}, e
	}
	if project {
		if _, ok := s.projects[parts[0]]; !ok {
			return store.StartJournal{}, store.ErrNotFound
		}
		j, e := s.store.PendingStart(parts[0])
		if errors.Is(e, store.ErrNotFound) {
			return store.StartJournal{}, nil
		}
		return j, e
	}
	task, e := s.store.Task(parts[0])
	if e != nil {
		return store.StartJournal{}, e
	}
	if _, registered := s.projects[task.ProjectID]; !registered {
		return store.StartJournal{}, store.ErrNotFound
	}
	if e = s.startJournalAuthority(r); e != nil {
		return store.StartJournal{}, e
	}
	var j store.StartJournal
	identity := store.StartIdentity{TaskID: task.ID, Role: in.Role, PlanRevision: condition.revision, Generation: condition.generation}
	if r.Method == "GET" {
		j, e = s.store.ReadStartJournal(key)
	} else {
		j, e = s.store.LookupStartJournal(key, identity)
	}
	if errors.Is(e, store.ErrNotFound) && r.Method == "POST" && len(parts) == 2 {
		if condition.state != "ready" || taskETag(task) != condition.raw {
			return store.StartJournal{}, errTaskPrecondition
		}
		if e = s.startJournalAuthority(r); e != nil {
			return store.StartJournal{}, e
		}
		receipt, err := s.store.PrepareStart(key, identity)
		return receipt.Journal, err
	}
	if e != nil {
		return store.StartJournal{}, e
	}
	if j.Draft.Task.ID != task.ID || j.Draft.Task.ProjectID != task.ProjectID {
		return store.StartJournal{}, store.ErrNotFound
	}
	if condition.raw != taskETag(j.Draft.Task) {
		return store.StartJournal{}, errTaskPrecondition
	}
	if r.Method == "POST" && len(parts) == 3 {
		if e = s.startJournalAuthority(r); e != nil {
			return store.StartJournal{}, e
		}
		return s.store.ResolveStart(key, identity, parts[2], in.RunID)
	}
	return j, nil
}

package api

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/yetone/magpie/internal/fusion/control"
	"github.com/yetone/magpie/internal/fusion/store"
	"github.com/yetone/magpie/internal/fusion/workflow"
)

type WorkflowReply struct {
	Task     store.Task          `json:"task"`
	Workflow *store.WorkflowView `json:"workflow"`
}
type AttachWorkflowRequest struct {
	Kind workflow.Kind `json:"kind"`
}
type WorkflowDesignRequest struct {
	RunID    string                  `json:"run_id"`
	Document workflow.DesignDocument `json:"document"`
}
type ApproveWorkflowRequest struct {
	DesignHash     string `json:"design_hash"`
	AcceptanceHash string `json:"acceptance_hash"`
}

func workflowPath(path string) bool {
	if !strings.HasPrefix(path, "/control/v1/tasks/") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(path, "/control/v1/tasks/"), "/")
	return len(parts) > 1 && parts[1] == "workflow"
}
func (s *Server) workflowAuthority(r *http.Request) error {
	if !s.auth.ManagementCurrent(r.Context()) {
		return control.ErrForbidden
	}
	return r.Context().Err()
}
func (s *Server) workflowControl(w http.ResponseWriter, r *http.Request) {
	if e := s.workflowAuthority(r); e != nil {
		controlFailure(w, e)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/control/v1/tasks/"), "/")
	if len(parts) < 2 || len(parts) > 3 || !opaque(parts[0]) || len(parts) == 3 && parts[2] != "design" && parts[2] != "approve" || r.URL.RawPath != "" || len(r.Header.Values("Idempotency-Key")) != 0 {
		controlFailure(w, errInvalid)
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
	var condition taskCondition
	var attach AttachWorkflowRequest
	var design WorkflowDesignRequest
	var approve ApproveWorkflowRequest
	var e error
	if r.Method == "POST" {
		condition, e = taskIfMatch(r)
		if e != nil {
			controlFailure(w, e)
			return
		}
		switch {
		case len(parts) == 2:
			e = read(r, &attach)
			if e == nil {
				_, e = workflow.DefinitionFor(attach.Kind)
			}
		case parts[2] == "design":
			e = read(r, &design)
			if e == nil && !opaque(design.RunID) {
				e = errInvalid
			}
		case parts[2] == "approve":
			e = read(r, &approve)
			if e == nil && (!workflowHash(approve.DesignHash) || !workflowHash(approve.AcceptanceHash)) {
				e = errInvalid
			}
		}
	} else if r.Body != nil {
		var body []byte
		body, e = io.ReadAll(io.LimitReader(r.Body, 1))
		if e == nil && len(body) > 0 {
			e = errInvalid
		}
	}
	if e != nil {
		controlFailure(w, errInvalid)
		return
	}
	s.mu.Lock()
	out, e := s.workflowLocked(r, parts, condition, attach, design, approve)
	s.mu.Unlock()
	if authErr := s.workflowAuthority(r); authErr != nil {
		controlFailure(w, authErr)
		return
	}
	if e != nil {
		controlFailure(w, e)
		return
	}
	w.Header().Set("ETag", taskETag(out.Task))
	respond(w, 200, out)
}
func workflowHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func (s *Server) workflowLocked(r *http.Request, parts []string, condition taskCondition, attach AttachWorkflowRequest, design WorkflowDesignRequest, approve ApproveWorkflowRequest) (WorkflowReply, error) {
	var out WorkflowReply
	if e := s.workflowAuthority(r); e != nil {
		return out, e
	}
	task, e := s.store.Task(parts[0])
	if e != nil {
		return out, e
	}
	if _, ok := s.projects[task.ProjectID]; !ok {
		return out, store.ErrNotFound
	}
	if r.Method == "POST" && taskETag(task) != condition.raw {
		return out, errTaskPrecondition
	}
	if e = s.workflowAuthority(r); e != nil {
		return out, e
	}
	var v store.WorkflowView
	if r.Method == "GET" {
		v, e = s.store.Workflow(task.ID)
		if errors.Is(e, store.ErrNotFound) {
			e = nil
		} else if e == nil {
			out.Workflow = &v
		}
	} else {
		version := store.TaskVersion{PlanRevision: condition.revision, Generation: condition.generation, State: condition.state}
		switch {
		case len(parts) == 2:
			v, e = s.store.AttachWorkflowAuthorized(task.ID, version, attach.Kind, func() bool { return s.auth.ManagementCurrent(r.Context()) })
		case parts[2] == "design":
			v, e = s.store.SaveWorkflowDesignAuthorized(task.ID, version, design.RunID, design.Document, func() bool { return s.auth.ManagementCurrent(r.Context()) })
		case parts[2] == "approve":
			v, e = s.store.ApproveWorkflowDesignAuthorized(task.ID, version, approve.DesignHash, approve.AcceptanceHash, func() bool { return s.auth.ManagementCurrent(r.Context()) })
		}
		if e == nil {
			out.Workflow = &v
		}
	}
	if e != nil {
		return out, e
	}
	if e = s.workflowAuthority(r); e != nil {
		return WorkflowReply{}, e
	}
	current, e := s.store.Task(task.ID)
	if e != nil {
		return WorkflowReply{}, e
	}
	if current != task {
		return WorkflowReply{}, errTaskPrecondition
	}
	out.Task = task
	return out, nil
}

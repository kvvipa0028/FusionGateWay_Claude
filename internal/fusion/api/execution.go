package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/yetone/magpie/internal/fusion/control"
	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

var errTaskPrecondition = errors.New("task state precondition changed")
var errTaskPreconditionMissing = errors.New("task state precondition required")

// SetController is trusted bootstrap wiring. A running server cannot rebind
// execution ownership or attach a controller using another task database.
func (s *Server) SetController(c *control.Controller) error {
	if c == nil || !c.UsesStore(s.store) {
		return errInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.controller != nil {
		return store.ErrConflict
	}
	s.controller = c
	return nil
}

type StartRoleRequest struct {
	Role stageplan.Role `json:"role"`
}
type ResumeRoleRequest struct {
	Role    stageplan.Role        `json:"role"`
	Restore store.RestoreIdentity `json:"restore"`
}
type CheckpointReply struct {
	Checkpoint control.CheckpointRef `json:"checkpoint"`
	Run        RunView               `json:"run"`
}
type RunView struct {
	ID              string                    `json:"id"`
	TaskID          string                    `json:"task_id"`
	Role            stageplan.Role            `json:"role"`
	Attempt         int64                     `json:"attempt"`
	Generation      int64                     `json:"generation"`
	PlanRevision    int64                     `json:"plan_revision"`
	State           string                    `json:"state"`
	StartupIntent   bool                      `json:"startup_intent"`
	LaunchConfirmed bool                      `json:"launch_confirmed"`
	Target          stageplan.ExecutionTarget `json:"target"`
}
type ExecutionReply struct {
	Run     RunView       `json:"run"`
	Created bool          `json:"created"`
	Error   *controlError `json:"error,omitempty"`
}
type controlError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func runView(r store.StageRun) RunView {
	return RunView{ID: r.ID, TaskID: r.TaskID, Role: r.Role, Attempt: r.Attempt, Generation: r.Generation, PlanRevision: r.PlanRevision, State: r.State, StartupIntent: r.StartupIntent, LaunchConfirmed: r.LaunchConfirmed, Target: r.Target}
}
func taskETag(t store.Task) string {
	return fmt.Sprintf(`"p%d-g%d-%s"`, t.PlanRevision, t.Generation, t.State)
}

type taskCondition struct {
	revision, generation int64
	state                string
	raw                  string
}

func taskIfMatch(r *http.Request) (taskCondition, error) {
	v := r.Header.Values("If-Match")
	if len(v) == 0 {
		return taskCondition{}, errTaskPreconditionMissing
	}
	if len(v) != 1 {
		return taskCondition{}, errInvalid
	}
	raw := v[0]
	if len(raw) < 8 || len(raw) > 90 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return taskCondition{}, errInvalid
	}
	parts := strings.Split(raw[1:len(raw)-1], "-")
	if len(parts) != 3 || !strings.HasPrefix(parts[0], "p") || !strings.HasPrefix(parts[1], "g") || len(parts[2]) == 0 || len(parts[2]) > 32 {
		return taskCondition{}, errInvalid
	}
	for _, c := range parts[2] {
		if c != '_' && (c < 'a' || c > 'z') {
			return taskCondition{}, errInvalid
		}
	}
	parse := func(s string) (int64, error) {
		n, e := strconv.ParseInt(s, 10, 64)
		if e != nil || n < 0 || strconv.FormatInt(n, 10) != s {
			return 0, errInvalid
		}
		return n, nil
	}
	rev, e := parse(parts[0][1:])
	if e != nil || rev < 1 {
		return taskCondition{}, errInvalid
	}
	gen, e := parse(parts[1][1:])
	if e != nil {
		return taskCondition{}, errInvalid
	}
	return taskCondition{revision: rev, generation: gen, state: parts[2], raw: raw}, nil
}
func validRole(role stageplan.Role) bool {
	for _, v := range stageplan.AllRoles() {
		if role == v {
			return true
		}
	}
	return false
}
func controlErrorValue(e error) (int, controlError) {
	code, reason := 500, "internal_error"
	switch {
	case errors.Is(e, control.ErrForbidden), errors.Is(e, store.ErrWorkflowAuthority):
		code, reason = 401, "management_authority_unavailable"
	case errors.Is(e, errInvalid), errors.Is(e, store.ErrInvalid):
		code, reason = 400, "invalid_request"
	case errors.Is(e, errTaskPreconditionMissing):
		code, reason = 428, "task_precondition_required"
	case errors.Is(e, errTaskPrecondition):
		code, reason = 412, "task_state_changed"
	case errors.Is(e, store.ErrNotFound):
		code, reason = 404, "record_unavailable"
	case errors.Is(e, control.ErrBusy):
		code, reason = 429, "execution_capacity_reached"
	case errors.Is(e, control.ErrClosed):
		code, reason = 503, "execution_controller_unavailable"
	case errors.Is(e, control.ErrUnsupported):
		code, reason = 503, "runtime_unsupported"
	case errors.Is(e, control.ErrLaunch), errors.Is(e, control.ErrReconcile), errors.Is(e, store.ErrPauseReconcile), errors.Is(e, store.ErrCancelReconcile):
		code, reason = 409, "execution_requires_reconciliation"
	case errors.Is(e, store.ErrFenced):
		code, reason = 409, "execution_fenced"
	case errors.Is(e, store.ErrIndependence):
		code, reason = 409, "project_independence_conflict"
	case errors.Is(e, store.ErrWorkflowGate), errors.Is(e, store.ErrStageLimit):
		code, reason = 409, "workflow_requires_review"
	case errors.Is(e, store.ErrConflict), errors.Is(e, control.ErrIdentity):
		code, reason = 409, "execution_conflict"
	default:
		var b *policy.Blocker
		if errors.As(e, &b) {
			switch b.Code {
			case "admission_unavailable", "admission_proof_missing", "capacity_unavailable":
				code, reason = 503, b.Code
			case "data_permission_denied", "write_permission_denied":
				code, reason = 403, b.Code
			case "budget_exhausted":
				code, reason = 429, b.Code
			case "start_request_invalid":
				code, reason = 400, b.Code
			case "task_unavailable", "plan_unavailable":
				code, reason = 404, b.Code
			case "task_not_ready", "target_not_approved", "route_changed", "sandbox_unverified", "verification_unavailable", "internal_execution_uncontrolled", "quota_query_not_admitted", "quota_identity_mismatch", "quota_unknown", "quota_stale", "quota_unverified", "quota_auth_required", "quota_unsupported", "quota_zero", "budget_missing", "capacity_busy", "reservation_failed", "stage_identity_invalid", "execution_fenced", "reservation_changed", "stop_unverified", "execution_not_reconciled", "cancelled", "start_request_conflict", "start_conflict":
				code, reason = 409, b.Code
			}
		}
	}
	return code, controlError{Code: reason, Message: http.StatusText(code)}
}
func controlFailure(w http.ResponseWriter, e error) {
	code, value := controlErrorValue(e)
	respond(w, code, map[string]any{"error": value})
}
func (s *Server) taskHeader(w http.ResponseWriter, id string) {
	if t, e := s.store.Task(id); e == nil {
		w.Header().Set("X-Fusion-Task-ETag", taskETag(t))
	}
}
func (s *Server) executionControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", 405)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/control/v1/tasks/"), "/")
	start := len(parts) == 2 && parts[1] == "start"
	resume := len(parts) == 2 && parts[1] == "resume"
	cancel := len(parts) == 4 && parts[1] == "runs" && parts[3] == "cancel"
	checkpoint := len(parts) == 4 && parts[1] == "runs" && parts[3] == "checkpoint"
	if !start && !resume && !cancel && !checkpoint || !opaque(parts[0]) || (cancel || checkpoint) && !opaque(parts[2]) {
		controlFailure(w, errInvalid)
		return
	}
	s.mu.Lock()
	c := s.controller
	s.mu.Unlock()
	if c == nil {
		controlFailure(w, control.ErrClosed)
		return
	}
	condition, e := taskIfMatch(r)
	if e != nil {
		controlFailure(w, e)
		return
	}
	if start || resume {
		if condition.state != "ready" {
			controlFailure(w, errTaskPrecondition)
			return
		}
		values := r.Header.Values("Idempotency-Key")
		if len(values) != 1 || !opaque(values[0]) {
			controlFailure(w, errInvalid)
			return
		}
		in := store.StartIdentity{TaskID: parts[0], PlanRevision: condition.revision, Generation: condition.generation}
		if resume {
			var body ResumeRoleRequest
			if read(r, &body) != nil || !validRole(body.Role) {
				controlFailure(w, errInvalid)
				return
			}
			in.Role = body.Role
			in.Restore = &body.Restore
		} else {
			var body StartRoleRequest
			if read(r, &body) != nil || !validRole(body.Role) {
				controlFailure(w, errInvalid)
				return
			}
			in.Role = body.Role
		}
		// Retries refer to their original condition, even after the task changes.
		// A new key must match the current ready representation. The controller
		// and Store still own atomic generation/state checks and launch winner.
		_, lookupErr := s.store.LookupStart(values[0], in)
		if errors.Is(lookupErr, store.ErrNotFound) {
			task, e := s.store.Task(parts[0])
			if e != nil {
				controlFailure(w, e)
				return
			}
			if condition.raw != taskETag(task) {
				controlFailure(w, errTaskPrecondition)
				return
			}
		} else if lookupErr != nil {
			controlFailure(w, lookupErr)
			return
		}
		if !s.auth.ManagementCurrent(r.Context()) {
			controlFailure(w, control.ErrForbidden)
			return
		}
		var receipt store.StartReceipt
		if resume {
			receipt, e = c.RestoreAuthorized(r.Context(), values[0], in, s.auth.ManagementCurrent)
		} else {
			receipt, e = c.StartAuthorized(r.Context(), values[0], in, s.auth.ManagementCurrent)
		}
		if receipt.Run.ID == "" {
			if e != nil {
				controlFailure(w, e)
			} else {
				controlFailure(w, errInvalid)
			}
			return
		}
		current, re := s.store.Run(receipt.Run.ID)
		if re != nil {
			controlFailure(w, re)
			return
		}
		out := ExecutionReply{Run: runView(current), Created: receipt.Created}
		w.Header().Set("Location", "/agent/v1/tasks/"+parts[0]+"/runs/"+current.ID)
		s.taskHeader(w, parts[0])
		if e != nil {
			code, value := controlErrorValue(e)
			out.Error = &value
			respond(w, code, out)
			return
		}
		code := 200
		if receipt.Created {
			code = 202
		}
		respond(w, code, out)
		return
	}
	var body struct{}
	if read(r, &body) != nil {
		controlFailure(w, errInvalid)
		return
	}
	if checkpoint {
		ref, e := c.CheckpointAuthorized(r.Context(), parts[0], parts[2], store.TaskVersion{PlanRevision: condition.revision, Generation: condition.generation, State: condition.state}, s.auth.ManagementCurrent)
		if errors.Is(e, store.ErrConflict) {
			e = errTaskPrecondition
		}
		if e != nil {
			controlFailure(w, e)
			return
		}
		run, e := s.store.Run(parts[2])
		if e != nil || run.TaskID != parts[0] {
			controlFailure(w, control.ErrIdentity)
			return
		}
		if !s.auth.ManagementCurrent(r.Context()) {
			controlFailure(w, control.ErrForbidden)
			return
		}
		s.taskHeader(w, parts[0])
		w.Header().Set("Location", "/agent/v1/tasks/"+parts[0]+"/runs/"+run.ID)
		respond(w, 200, CheckpointReply{Checkpoint: ref, Run: runView(run)})
		return
	}
	task, e := s.store.Task(parts[0])
	if e != nil {
		controlFailure(w, e)
		return
	}
	run, e := s.store.Run(parts[2])
	if e != nil || run.TaskID != task.ID {
		controlFailure(w, store.ErrNotFound)
		return
	}
	if condition.revision != task.PlanRevision || condition.generation != task.Generation || run.Generation != condition.generation {
		controlFailure(w, errTaskPrecondition)
		return
	}
	done := terminalRun(run.State)
	if !done && condition.raw != taskETag(task) {
		controlFailure(w, errTaskPrecondition)
		return
	}
	if !s.auth.ManagementCurrent(r.Context()) {
		controlFailure(w, control.ErrForbidden)
		return
	}
	current, e := c.CancelAtRevision(task.ID, run.ID, run.Generation, condition.revision)
	if e != nil {
		if errors.Is(e, store.ErrConflict) {
			e = errTaskPrecondition
		}
		controlFailure(w, e)
		return
	}
	s.taskHeader(w, task.ID)
	code := 202
	if done {
		code = 200
	}
	respond(w, code, ExecutionReply{Run: runView(current)})
}
func terminalRun(state string) bool {
	switch state {
	case "succeeded", "failed", "cancelled", "interrupted", "advisory_only":
		return true
	}
	return false
}
func (s *Server) readRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", 405)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/agent/v1/tasks/"), "/")
	if len(parts) != 3 || parts[1] != "runs" || !opaque(parts[0]) || !opaque(parts[2]) {
		controlFailure(w, errInvalid)
		return
	}
	run, e := s.store.Run(parts[2])
	if e != nil || run.TaskID != parts[0] {
		controlFailure(w, store.ErrNotFound)
		return
	}
	if !s.auth.ManagementCurrent(r.Context()) {
		controlFailure(w, control.ErrForbidden)
		return
	}
	s.taskHeader(w, run.TaskID)
	respond(w, 200, ExecutionReply{Run: runView(run)})
}

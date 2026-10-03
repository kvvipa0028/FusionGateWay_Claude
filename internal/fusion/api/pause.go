package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/yetone/magpie/internal/fusion/control"
	"github.com/yetone/magpie/internal/fusion/store"
)

type TaskControlReply struct {
	Task    store.Task    `json:"task"`
	Run     *RunView      `json:"run,omitempty"`
	Changed bool          `json:"changed"`
	Error   *controlError `json:"error,omitempty"`
}

func (s *Server) taskControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", 405)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/control/v1/tasks/"), "/")
	if len(parts) != 2 || !opaque(parts[0]) || parts[1] != "pause" && parts[1] != "continue" && parts[1] != "cancel" {
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
	var body struct{}
	if read(r, &body) != nil {
		controlFailure(w, errInvalid)
		return
	}
	expected := store.TaskVersion{PlanRevision: condition.revision, Generation: condition.generation, State: condition.state}
	var receipt store.TaskControlReceipt
	switch parts[1] {
	case "pause":
		receipt, e = c.PauseAuthorized(r.Context(), parts[0], expected, s.auth.ManagementCurrent)
	case "continue":
		receipt, e = c.ContinueAuthorized(r.Context(), parts[0], expected, s.auth.ManagementCurrent)
	case "cancel":
		receipt, e = c.CancelTaskAuthorized(r.Context(), parts[0], expected, s.auth.ManagementCurrent)
	}
	if errors.Is(e, store.ErrConflict) {
		e = errTaskPrecondition
	}
	if receipt.Task.ID == "" {
		if e == nil {
			e = errInvalid
		}
		controlFailure(w, e)
		return
	}
	// Preserve an accepted intent even if its stop remains uncertain. Return
	// the current Task and exactly matching header, never private StageRun.
	task, readErr := s.store.Task(receipt.Task.ID)
	if readErr != nil {
		controlFailure(w, readErr)
		return
	}
	out := TaskControlReply{Task: task, Changed: receipt.Changed}
	if receipt.Run != nil {
		run, readErr := s.store.Run(receipt.Run.ID)
		if readErr != nil {
			controlFailure(w, readErr)
			return
		}
		view := runView(run)
		out.Run = &view
	}
	w.Header().Set("X-Fusion-Task-ETag", taskETag(out.Task))
	w.Header().Set("Location", "/agent/v1/tasks/"+out.Task.ID)
	if e != nil {
		code, value := controlErrorValue(e)
		out.Error = &value
		respond(w, code, out)
		return
	}
	code := 200
	if receipt.Task.State == "pausing" || receipt.Task.State == "cancelling" {
		code = 202
	}
	respond(w, code, out)
}

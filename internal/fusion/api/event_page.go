package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/yetone/magpie/internal/fusion/control"
	"github.com/yetone/magpie/internal/fusion/store"
)

// EventPageReply is a bounded companion to SSE, not a new execution receipt.
type EventPageReply struct {
	TaskID    string        `json:"task_id"`
	After     int64         `json:"after"`
	NextAfter int64         `json:"next_after"`
	HasMore   bool          `json:"has_more"`
	Events    []store.Event `json:"events"`
}

func eventPagePath(path string) bool {
	const prefix = "/agent/v1/tasks/"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	return len(parts) > 2 && parts[1] == "events" && parts[2] == "page"
}

func eventPathCursor(value string) (int64, error) {
	if value == "" || len(value) > 19 || len(value) > 1 && value[0] == '0' {
		return 0, errInvalid
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return 0, errInvalid
		}
	}
	n, e := strconv.ParseInt(value, 10, 64)
	if e != nil {
		return 0, errInvalid
	}
	return n, nil
}

func (s *Server) eventsPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method Not Allowed", 405)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/agent/v1/tasks/"), "/")
	if len(parts) != 4 || !opaque(parts[0]) || parts[1] != "events" || parts[2] != "page" || len(r.Header.Values("Last-Event-ID")) != 0 {
		failure(w, errInvalid)
		return
	}
	after, e := eventPathCursor(parts[3])
	if e != nil {
		failure(w, e)
		return
	}
	// History is only visible for a currently registered project. No route
	// inspection, scheduling, reservations or Native calls occur here.
	task, e := s.store.Task(parts[0])
	if e != nil {
		failure(w, e)
		return
	}
	registered := func() bool { s.mu.Lock(); defer s.mu.Unlock(); _, ok := s.projects[task.ProjectID]; return ok }
	if !registered() {
		failure(w, store.ErrNotFound)
		return
	}
	page, e := s.store.EventsPage(task.ID, after, 33)
	// Check authority before disclosing results or cursor/error information.
	if !s.auth.ManagementCurrent(r.Context()) || r.Context().Err() != nil {
		controlFailure(w, control.ErrForbidden)
		return
	}
	if !registered() {
		failure(w, store.ErrNotFound)
		return
	}
	if e == store.ErrConflict {
		e = errCursor
	}
	if e != nil {
		failure(w, e)
		return
	}
	for _, v := range page {
		if _, e = eventLine(v); e != nil {
			failure(w, e)
			return
		}
	}
	out := EventPageReply{TaskID: task.ID, After: after, NextAfter: after, HasMore: len(page) > 32, Events: page}
	if out.HasMore {
		out.Events = page[:32]
	}
	if len(out.Events) > 0 {
		out.NextAfter = out.Events[len(out.Events)-1].Seq
	}
	if !s.auth.ManagementCurrent(r.Context()) || r.Context().Err() != nil {
		controlFailure(w, control.ErrForbidden)
		return
	}
	respond(w, 200, out)
}

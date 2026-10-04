package api

import (
	"net/http"
	"strings"

	"github.com/yetone/magpie/internal/fusion/control"
	"github.com/yetone/magpie/internal/fusion/store"
)

type TaskListEntry struct {
	store.Task
	GoalTruncated bool   `json:"goal_truncated"`
	ETag          string `json:"etag"`
}

type TaskListReply struct {
	Tasks      []TaskListEntry `json:"tasks"`
	NextBefore string          `json:"next_before"`
}

// Match the resource segment, not the final suffix: project or cursor IDs can
// themselves be called presets, quota or tasks without changing dispatch.
func taskIndexPath(path string) bool {
	const prefix = "/control/v1/projects/"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	return len(parts) > 1 && parts[1] == "tasks"
}

func (s *Server) taskIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method Not Allowed", 405)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/control/v1/projects/"), "/")
	before := ""
	if len(parts) != 2 && (len(parts) != 4 || parts[2] != "before" || !opaque(parts[3])) || !opaque(parts[0]) {
		failure(w, errInvalid)
		return
	}
	if len(parts) == 4 {
		before = parts[3]
	}
	s.mu.Lock()
	_, err := s.currentProjectLocked(parts[0])
	s.mu.Unlock()
	if !s.auth.ManagementCurrent(r.Context()) {
		controlFailure(w, control.ErrForbidden)
		return
	}
	if err != nil {
		failure(w, err)
		return
	}
	page, err := s.store.ProjectTasks(r.Context(), parts[0], before)
	// Authority can be revoked while waiting for the Store. No partial data is
	// sent before this fence, including errors concerning a cursor's existence.
	if !s.auth.ManagementCurrent(r.Context()) {
		controlFailure(w, control.ErrForbidden)
		return
	}
	if err != nil {
		failure(w, err)
		return
	}
	out := TaskListReply{Tasks: make([]TaskListEntry, 0, len(page.Tasks)), NextBefore: page.NextBefore}
	for _, task := range page.Tasks {
		entry := TaskListEntry{Task: task, ETag: taskETag(task)}
		goal := []rune(task.Goal)
		if len(goal) > 512 {
			entry.Goal = string(goal[:512])
			entry.GoalTruncated = true
		}
		out.Tasks = append(out.Tasks, entry)
	}
	respond(w, 200, out)
}

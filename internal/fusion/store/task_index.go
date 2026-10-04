package store

import (
	"context"
	"database/sql"
	"errors"
)

const TaskPageSize = 32

type TaskPage struct {
	Tasks      []Task
	NextBefore string
}

// ProjectTasks reads existing tasks in reverse insertion order. The cursor is
// an existing task ID in the same project, never a client-supplied SQL offset.
// New tasks can appear on a refreshed first page without shifting older pages.
func (s *Store) ProjectTasks(ctx context.Context, projectID, before string) (TaskPage, error) {
	if !opaque(projectID) || before != "" && !opaque(before) {
		return TaskPage{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return TaskPage{}, ErrClosed
	}
	upper := int64(1<<63 - 1)
	if before != "" {
		err := s.db.QueryRowContext(ctx, "SELECT rowid FROM tasks WHERE project_id=? AND id=?", projectID, before).Scan(&upper)
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrNotFound
		}
		if err != nil {
			return TaskPage{}, err
		}
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id,project_id,goal,state,plan_revision,generation FROM tasks WHERE project_id=? AND rowid<? ORDER BY rowid DESC LIMIT ?", projectID, upper, TaskPageSize+1)
	if err != nil {
		return TaskPage{}, err
	}
	defer rows.Close()
	out := TaskPage{Tasks: make([]Task, 0, TaskPageSize)}
	for rows.Next() {
		var task Task
		if err := rows.Scan(&task.ID, &task.ProjectID, &task.Goal, &task.State, &task.PlanRevision, &task.Generation); err != nil {
			return TaskPage{}, err
		}
		if len(out.Tasks) == TaskPageSize {
			out.NextBefore = out.Tasks[len(out.Tasks)-1].ID
			break
		}
		out.Tasks = append(out.Tasks, task)
	}
	if err := rows.Err(); err != nil {
		return TaskPage{}, err
	}
	if err := ctx.Err(); err != nil {
		return TaskPage{}, err
	}
	return out, nil
}

package store

import (
	"database/sql"
	"errors"
)

// EventsPage reads a bounded task-specific suffix. A cursor beyond the durable
// tail is rejected so a mistaken reconnect cannot silently skip future events.
func (s *Store) EventsPage(taskID string, after int64, limit int) ([]Event, error) {
	if !opaque(taskID) || after < 0 || limit < 1 || limit > 256 {
		return nil, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, ErrClosed
	}
	var tail int64
	e := s.db.QueryRow("SELECT next_event_seq FROM tasks WHERE id=?", taskID).Scan(&tail)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	if after > tail {
		return nil, ErrConflict
	}
	rows, e := s.db.Query("SELECT task_id,seq,kind,run_id,generation FROM events WHERE task_id=? AND seq>? ORDER BY seq LIMIT ?", taskID, after, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Event{}
	expected := after + 1
	for rows.Next() {
		var v Event
		if e = rows.Scan(&v.TaskID, &v.Seq, &v.Kind, &v.RunID, &v.Generation); e != nil {
			return nil, e
		}
		if v.Seq != expected || v.Seq > tail {
			return nil, ErrInvalid
		}
		expected++
		out = append(out, v)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	if len(out) < limit && expected <= tail {
		return nil, ErrInvalid
	}
	return out, nil
}

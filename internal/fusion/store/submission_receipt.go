package store

import (
	"database/sql"
	"errors"
)

type creationSubmission struct {
	previewID    string
	existingOnly bool
}

// CreateSubmission commits the trusted preview identity with the task and its
// original creation payload. A committed retry bypasses new-task defaults checks.
func (s *Store) CreateSubmission(previewID, key string, in CreateRequest, expected DefaultStamp) (Task, error) {
	if !opaque(previewID) {
		return Task{}, ErrInvalid
	}
	return s.create(key, in, &expected, &creationSubmission{previewID: previewID})
}

// LookupCreationSubmission binds a genuine server preview to an already
// committed exact project/key/payload receipt. It never creates a new task.
func (s *Store) LookupCreationSubmission(previewID, key string, in CreateRequest) (Task, error) {
	if !opaque(previewID) {
		return Task{}, ErrInvalid
	}
	return s.create(key, in, nil, &creationSubmission{previewID: previewID, existingOnly: true})
}

// LookupSubmission reconciles only the complete original HTTP identity. The
// caller must authenticate and check the returned task's project registration.
func (s *Store) LookupSubmission(previewID, key, planHash string) (Task, error) {
	if !opaque(previewID) || !opaque(key) || planHash == "" {
		return Task{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return Task{}, ErrClosed
	}
	var oldKey, oldHash, taskID string
	e := s.db.QueryRow("SELECT idempotency_key,plan_hash,task_id FROM task_submissions WHERE preview_id=?", previewID).Scan(&oldKey, &oldHash, &taskID)
	if errors.Is(e, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	if e != nil {
		return Task{}, e
	}
	if oldKey != key || oldHash != planHash {
		return Task{}, ErrConflict
	}
	return taskIn(s.db, taskID)
}

func saveSubmission(tx *sql.Tx, submission *creationSubmission, key, payloadHash string, in CreateRequest, taskID string) error {
	if submission == nil {
		return nil
	}
	_, e := tx.Exec("INSERT INTO task_submissions VALUES(?,?,?,?,?,?)", submission.previewID, key, in.ProjectID, in.Plan.Hash, payloadHash, taskID)
	return e
}

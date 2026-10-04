package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// SubmissionDraft is frozen by the trusted preview owner, never decoded from a
// client-supplied CreateRequest. Its key is a request identity, not authentication.
type SubmissionDraft struct {
	PreviewID             string        `json:"preview_id"`
	Key                   string        `json:"key"`
	Request               CreateRequest `json:"request"`
	ExpiresAt             time.Time     `json:"expires_at"`
	ConfigurationRevision int64         `json:"configuration_revision"`
}
type SubmissionIdentity struct{ ProjectID, PreviewID, Key, PlanHash string }
type SubmissionJournal struct {
	Draft  SubmissionDraft
	State  string
	TaskID string
}
type SubmissionJournalReceipt struct {
	Submission SubmissionJournal
	Created    bool
}

func canonicalSubmissionDraft(in SubmissionDraft) (SubmissionDraft, string, []byte, error) {
	if !opaque(in.PreviewID) || in.ConfigurationRevision < 1 || in.ExpiresAt.Unix() <= 0 || in.ExpiresAt.Year() > 2262 {
		return in, "", nil, ErrInvalid
	}
	request, payloadHash, e := creationPayload(in.Key, in.Request)
	if e != nil {
		return in, "", nil, e
	}
	in.Request = request
	in.ExpiresAt = in.ExpiresAt.UTC()
	raw, e := json.Marshal(in)
	if e != nil {
		return in, "", nil, e
	}
	var frozen SubmissionDraft
	if e = json.Unmarshal(raw, &frozen); e != nil {
		return in, "", nil, e
	}
	return frozen, payloadHash, raw, nil
}
func submissionJournalIn(q queryRow, previewID string) (SubmissionJournal, error) {
	var value SubmissionJournal
	var project, key, planHash, payloadHash, raw, draftHash string
	var taskID sql.NullString
	e := q.QueryRow("SELECT project_id,idempotency_key,plan_hash,payload_hash,draft_json,draft_hash,state,task_id FROM submission_journals WHERE preview_id=?", previewID).Scan(&project, &key, &planHash, &payloadHash, &raw, &draftHash, &value.State, &taskID)
	if errors.Is(e, sql.ErrNoRows) {
		return value, ErrNotFound
	}
	if e != nil {
		return value, e
	}
	if json.Unmarshal([]byte(raw), &value.Draft) != nil || hash([]byte(raw)) != draftHash {
		return SubmissionJournal{}, ErrConflict
	}
	frozen, actualHash, _, e := canonicalSubmissionDraft(value.Draft)
	if e != nil || frozen.PreviewID != previewID || frozen.Request.ProjectID != project || frozen.Key != key || frozen.Request.Plan.Hash != planHash || actualHash != payloadHash {
		return SubmissionJournal{}, ErrConflict
	}
	value.Draft = frozen
	value.TaskID = taskID.String
	return value, nil
}

// PrepareSubmission persists the original request before the caller sends its
// task POST. Each project has one unresolved journal; terminal history remains.
func (s *Store) PrepareSubmission(in SubmissionDraft, expected DefaultStamp) (SubmissionJournalReceipt, error) {
	frozen, payloadHash, raw, e := canonicalSubmissionDraft(in)
	if e != nil {
		return SubmissionJournalReceipt{}, e
	}
	var result SubmissionJournalReceipt
	e = s.transaction(func(tx *sql.Tx) error {
		old, err := submissionJournalIn(tx, frozen.PreviewID)
		if err == nil {
			_, _, oldRaw, e := canonicalSubmissionDraft(old.Draft)
			if e != nil || hash(oldRaw) != hash(raw) {
				return ErrConflict
			}
			result.Submission = old
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		var pending string
		err = tx.QueryRow("SELECT preview_id FROM submission_journals WHERE project_id=? AND state IN ('prepared','committed')", frozen.Request.ProjectID).Scan(&pending)
		if err == nil {
			return ErrConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		state := "prepared"
		var taskID any
		var oldKey, oldProject, oldPlan, oldPayload, id string
		err = tx.QueryRow("SELECT idempotency_key,project_id,plan_hash,payload_hash,task_id FROM task_submissions WHERE preview_id=?", frozen.PreviewID).Scan(&oldKey, &oldProject, &oldPlan, &oldPayload, &id)
		if err == nil {
			if oldKey != frozen.Key || oldProject != frozen.Request.ProjectID || oldPlan != frozen.Request.Plan.Hash || oldPayload != payloadHash {
				return ErrConflict
			}
			state = "committed"
			taskID = id
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		} else if e := checkDefaults(tx, frozen.Request.ProjectID, &expected); e != nil {
			return e
		}
		if _, err = tx.Exec("INSERT INTO submission_journals VALUES(?,?,?,?,?,?,?,?,?)", frozen.PreviewID, frozen.Request.ProjectID, frozen.Key, frozen.Request.Plan.Hash, payloadHash, string(raw), hash(raw), state, taskID); err != nil {
			return err
		}
		result.Submission, err = submissionJournalIn(tx, frozen.PreviewID)
		result.Created = err == nil
		return err
	})
	return result, e
}

func (s *Store) PendingSubmission(projectID string) (SubmissionJournal, error) {
	if !opaque(projectID) {
		return SubmissionJournal{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return SubmissionJournal{}, ErrClosed
	}
	var previewID string
	e := s.db.QueryRow("SELECT preview_id FROM submission_journals WHERE project_id=? AND state IN ('prepared','committed')", projectID).Scan(&previewID)
	if errors.Is(e, sql.ErrNoRows) {
		return SubmissionJournal{}, ErrNotFound
	}
	if e != nil {
		return SubmissionJournal{}, e
	}
	return submissionJournalIn(s.db, previewID)
}

// ResolveSubmission either acknowledges a proved task or atomically seals an
// uncommitted original preview. A lost acknowledgement can repeat the identity.
func (s *Store) ResolveSubmission(in SubmissionIdentity, action string) (SubmissionJournal, error) {
	if !opaque(in.ProjectID) || !opaque(in.PreviewID) || !opaque(in.Key) || in.PlanHash == "" || (action != "acknowledge" && action != "abandon") {
		return SubmissionJournal{}, ErrInvalid
	}
	var result SubmissionJournal
	e := s.transaction(func(tx *sql.Tx) error {
		value, e := submissionJournalIn(tx, in.PreviewID)
		if e != nil {
			return e
		}
		if value.Draft.Request.ProjectID != in.ProjectID || value.Draft.Key != in.Key || value.Draft.Request.Plan.Hash != in.PlanHash {
			return ErrConflict
		}
		next := "acknowledged"
		required := "committed"
		if action == "abandon" {
			next = "abandoned"
			required = "prepared"
		}
		if value.State == next {
			result = value
			return nil
		}
		if value.State != required {
			return ErrConflict
		}
		if action == "abandon" {
			var taskID string
			e = tx.QueryRow("SELECT task_id FROM task_submissions WHERE preview_id=?", in.PreviewID).Scan(&taskID)
			if e == nil {
				return ErrConflict
			}
			if !errors.Is(e, sql.ErrNoRows) {
				return e
			}
		}
		if _, e = tx.Exec("UPDATE submission_journals SET state=? WHERE preview_id=?", next, in.PreviewID); e != nil {
			return e
		}
		result, e = submissionJournalIn(tx, in.PreviewID)
		return e
	})
	return result, e
}

func checkSubmissionJournal(tx *sql.Tx, submission *creationSubmission, key, payloadHash string, in CreateRequest) error {
	journal, e := submissionJournalIn(tx, submission.previewID)
	if errors.Is(e, ErrNotFound) {
		return nil
	}
	if e != nil {
		return e
	}
	raw, e := json.Marshal(journal.Draft.Request)
	if e != nil {
		return e
	}
	if journal.Draft.Request.ProjectID != in.ProjectID || journal.Draft.Key != key || journal.Draft.Request.Plan.Hash != in.Plan.Hash || hash(raw) != payloadHash || journal.State == "abandoned" {
		return ErrConflict
	}
	return nil
}

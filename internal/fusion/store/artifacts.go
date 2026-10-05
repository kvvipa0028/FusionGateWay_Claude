package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/yetone/magpie/internal/fusion/handoff"
)

var ErrArtifactPending = errors.New("artifact requires matching released stop proof")
var ErrArtifactAuthority = errors.New("artifact producer authority unavailable")

// ArtifactRecord is internal trusted producer data, never an HTTP task input.
// Registration precedes release; reads independently require the released proof.
type ArtifactRecord struct {
	Reference     handoff.Reference     `json:"reference"`
	ParentRunID   string                `json:"parent_run_id"`
	InputTreeHash string                `json:"input_tree_hash"`
	Verification  *ArtifactVerification `json:"verification,omitempty"`
	Review        *ArtifactReview       `json:"review,omitempty"`
}

func (ArtifactRecord) String() string   { return "stored stage artifact (redacted)" }
func (ArtifactRecord) GoString() string { return "ArtifactRecord(<redacted>)" }

func artifactRecordIn(q queryRow, runID string) (ArtifactRecord, error) {
	var a ArtifactRecord
	var taskID, raw, checksum string
	err := q.QueryRow("SELECT task_id,record_json,record_hash FROM stage_artifacts WHERE run_id=?", runID).Scan(&taskID, &raw, &checksum)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	if hash([]byte(raw)) != checksum || json.Unmarshal([]byte(raw), &a) != nil || !handoff.ValidReference(a.Reference) || a.Reference.Binding.RunID != runID || a.Reference.Binding.TaskID != taskID || !validHash(a.InputTreeHash) {
		return ArtifactRecord{}, ErrInvalid
	}
	if a.Verification != nil && !validArtifactVerification(a) {
		return ArtifactRecord{}, ErrInvalid
	}
	if a.Review != nil && !validArtifactReview(a) {
		return ArtifactRecord{}, ErrInvalid
	}
	b, err := json.Marshal(a)
	if err != nil || string(b) != raw {
		return ArtifactRecord{}, ErrInvalid
	}
	return a, nil
}
func artifactRunMatches(q queryRow, a ArtifactRecord) (StageRun, error) {
	b := a.Reference.Binding
	r, err := runIn(q, b.RunID)
	if err != nil {
		return r, err
	}
	if r.TaskID != b.TaskID || r.Role != b.Role || r.Generation != b.Generation || r.PlanRevision != b.PlanRevision || r.State != "succeeded" || r.Owner != "" || r.NativeSessionID == "" || !r.StartupIntent || !r.LaunchConfirmed {
		return r, ErrConflict
	}
	p, err := planIn(q, r.TaskID, r.PlanRevision)
	if err != nil || p.Hash != b.PlanHash {
		return r, ErrConflict
	}
	target, _ := json.Marshal(r.Target)
	if hash(target) != b.TargetHash {
		return r, ErrConflict
	}
	binding, ok := p.Bindings[r.Role]
	match := ok && binding.Target != nil && reflect.DeepEqual(*binding.Target, r.Target)
	for _, candidate := range binding.Candidates {
		match = match || reflect.DeepEqual(candidate, r.Target)
	}
	if !match {
		return r, ErrConflict
	}
	var state, proof string
	if err := q.QueryRow("SELECT state,stop_proof_hash FROM reservations WHERE run_id=?", r.ID).Scan(&state, &proof); err != nil {
		return r, ErrConflict
	}
	if state != "held" && state != "released" || state == "released" && proof != a.Reference.StopProofHash {
		return r, ErrConflict
	}
	return r, nil
}
func artifactReleasedIn(q queryRow, runID string) (ArtifactRecord, error) {
	a, err := artifactRecordIn(q, runID)
	if err != nil {
		return a, err
	}
	r, err := artifactRunMatches(q, a)
	if err != nil {
		return ArtifactRecord{}, err
	}
	if !workflowRunStopped(q, r) {
		return ArtifactRecord{}, ErrArtifactPending
	}
	return a, nil
}

// Artifact returns a deep, hash-verified historical receipt only after the exact
// run was released. Files/source/root still require handoff.Restore validation.
func (s *Store) Artifact(runID string) (ArtifactRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return ArtifactRecord{}, ErrClosed
	}
	return artifactReleasedIn(s.db, runID)
}
func (s *Store) RecordArtifact(a ArtifactRecord) error {
	return s.RecordArtifactAuthorized(a, func() bool { return true })
}

// current is a bounded local authority check and must not reenter Store.
// The producer must verify actual Supervisor stop and current filesystem bytes
// independently before calling; a caller-declared boolean/hash is insufficient.
func (s *Store) RecordArtifactAuthorized(a ArtifactRecord, current func() bool) error {
	if a.Verification != nil || a.Review != nil {
		return ErrInvalid
	}
	return s.recordArtifactAuthorized(a, current)
}

func (s *Store) recordArtifactAuthorized(a ArtifactRecord, current func() bool) error {
	if current == nil || !current() {
		return ErrArtifactAuthority
	}
	if !handoff.ValidReference(a.Reference) || !validHash(a.InputTreeHash) || a.ParentRunID != "" && !opaque(a.ParentRunID) {
		return ErrInvalid
	}
	raw, err := json.Marshal(a)
	if err != nil || len(raw) > 40<<20 {
		return ErrInvalid
	}
	return s.transaction(func(tx *sql.Tx) error {
		if !current() {
			return ErrArtifactAuthority
		}
		r, err := artifactRunMatches(tx, a)
		if err != nil {
			return err
		}
		old, err := artifactRecordIn(tx, r.ID)
		if err == nil {
			if !reflect.DeepEqual(old, a) {
				return ErrConflict
			}
		} else if errors.Is(err, ErrNotFound) {
			t, err := taskIn(tx, r.TaskID)
			if err != nil || t.State != "ready" || t.Generation != r.Generation || t.PlanRevision != r.PlanRevision {
				return ErrConflict
			}
			if err := artifactParentIn(tx, t, r, a); err != nil {
				return err
			}
			if a.Verification != nil && !verificationContextIn(tx, t, a.Verification) {
				return ErrConflict
			}
			if a.Review != nil && !reviewContextIn(tx, t, a) {
				return ErrConflict
			}
			if _, err := tx.Exec("INSERT INTO stage_artifacts VALUES(?,?,?,?)", r.ID, r.TaskID, string(raw), hash(raw)); err != nil {
				return err
			}
			if err := event(tx, r.TaskID, "artifact_recorded", r.ID, r.Generation); err != nil {
				return err
			}
			if a.Verification != nil && a.Verification.Data.Verdict.Status != "passed" {
				if _, err := tx.Exec("UPDATE tasks SET state='needs_review' WHERE id=?", r.TaskID); err != nil {
					return err
				}
				if err := event(tx, r.TaskID, "verification_requires_review", r.ID, r.Generation); err != nil {
					return err
				}
			}
			if a.Review != nil && (!a.Review.Valid || a.Review.Document.Verdict != "approve") {
				if _, err := tx.Exec("UPDATE tasks SET state='needs_review' WHERE id=?", r.TaskID); err != nil {
					return err
				}
				if err := event(tx, r.TaskID, "review_requires_review", r.ID, r.Generation); err != nil {
					return err
				}
			}
		} else {
			return err
		}
		if !current() {
			return ErrArtifactAuthority
		}
		return nil
	})
}
func artifactParentIn(tx *sql.Tx, t Task, r StageRun, a ArtifactRecord) error {
	p, err := planIn(tx, t.ID, r.PlanRevision)
	if err != nil {
		return err
	}
	if len(p.RequiredRoles) == 1 {
		if a.ParentRunID != "" || a.InputTreeHash != a.Reference.BaseTreeHash {
			return ErrConflict
		}
		return nil
	}
	v, err := workflowIn(tx, t)
	if err != nil {
		return ErrConflict
	}
	n := -1
	for i, role := range v.Definition.RequiredRoles {
		if role == r.Role {
			n = i
		}
	}
	if n < 0 {
		return ErrConflict
	}
	if n == 0 {
		if a.ParentRunID != "" || a.InputTreeHash != a.Reference.BaseTreeHash {
			return ErrConflict
		}
		return nil
	}
	var parent string
	if err := tx.QueryRow("SELECT id FROM stage_runs WHERE task_id=? AND role=? AND generation<? ORDER BY generation DESC LIMIT 1", t.ID, v.Definition.RequiredRoles[n-1], r.Generation).Scan(&parent); err != nil || parent != a.ParentRunID {
		return ErrConflict
	}
	previous, err := artifactReleasedIn(tx, parent)
	if err != nil || previous.Reference.TreeHash != a.InputTreeHash || previous.Reference.BaseTreeHash != a.Reference.BaseTreeHash || !handoff.DerivedReference(a.Reference, previous.Reference) {
		return ErrConflict
	}
	return nil
}

package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/yetone/magpie/internal/fusion/evidence"
	"github.com/yetone/magpie/internal/fusion/handoff"
	"github.com/yetone/magpie/internal/fusion/stageplan"
)

type ReworkRecord struct {
	TaskID         string `json:"task_id"`
	ReviewRunID    string `json:"review_run_id"`
	PlanRevision   int64  `json:"plan_revision"`
	PlanHash       string `json:"plan_hash"`
	Generation     int64  `json:"generation"`
	DefinitionHash string `json:"definition_hash"`
	TreeHash       string `json:"tree_hash"`
	SpecHash       string `json:"spec_hash"`
	DesignHash     string `json:"design_hash"`
	AcceptanceHash string `json:"acceptance_hash"`
	TextHash       string `json:"text_hash"`
}

// ReworkEvidence is minted only by the independent source/root/spec reader.
// Neither model text nor a JSON receipt can construct its private authority.
type ReworkEvidence struct {
	owner    *Store
	artifact ArtifactRecord
	bundles  []handoff.Bundle
	bindings []handoff.Binding
}

func (ReworkEvidence) String() string { return "owned rework evidence (redacted)" }
func (p ReworkEvidence) Current() bool {
	if p.owner == nil || len(p.bundles) != 2 || len(p.bindings) != 2 {
		return false
	}
	for i, b := range p.bundles {
		if _, e := b.Read(p.bindings[i]); e != nil {
			return false
		}
	}
	return true
}
func (s *Store) PrepareReworkEvidence(runID, source, root string, spec evidence.Spec) (ReworkEvidence, error) {
	a, hard, e := s.ReviewedArtifact(runID, source, root, spec)
	if e != nil {
		return ReworkEvidence{}, e
	}
	if hard.Status != evidence.Passed || a.Review == nil || !a.Review.Valid || a.Review.Document.Verdict != "changes_required" {
		return ReworkEvidence{}, ErrWorkflowGate
	}
	p := ReworkEvidence{owner: s, artifact: a}
	for _, id := range []string{runID, a.ParentRunID} {
		v, e := s.Artifact(id)
		if e != nil {
			return ReworkEvidence{}, e
		}
		b, e := handoff.Restore(v.Reference, source, root)
		if e != nil {
			return ReworkEvidence{}, ErrInvalid
		}
		p.bundles = append(p.bundles, b)
		p.bindings = append(p.bindings, v.Reference.Binding)
	}
	if !p.Current() {
		return ReworkEvidence{}, ErrWorkflowGate
	}
	return p, nil
}
func reworkIn(q queryRow, taskID string) (ReworkRecord, error) {
	var raw, checksum string
	if e := q.QueryRow("SELECT record_json,record_hash FROM workflow_reworks WHERE task_id=?", taskID).Scan(&raw, &checksum); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			e = ErrNotFound
		}
		return ReworkRecord{}, e
	}
	var r ReworkRecord
	if workflowJSON(raw, checksum, &r) != nil || r.TaskID != taskID || !opaque(r.ReviewRunID) || r.PlanRevision < 1 || r.Generation < 1 {
		return ReworkRecord{}, ErrWorkflowGate
	}
	for _, h := range []string{r.PlanHash, r.DefinitionHash, r.TreeHash, r.SpecHash, r.DesignHash, r.AcceptanceHash, r.TextHash} {
		if !validHash(h) {
			return ReworkRecord{}, ErrWorkflowGate
		}
	}
	return r, nil
}
func (s *Store) Rework(taskID string) (ReworkRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return ReworkRecord{}, ErrClosed
	}
	return reworkIn(s.db, taskID)
}

// Structural receipt validation only: do not call reviewContextIn here,
// because it calls workflowIn. Fresh owned file/evidence checks belong to the
// trusted producer and every subsequent resolver, not this read-only view.
func workflowSequenceIn(q workflowQuery, t Task, v WorkflowView) ([]stageplan.Role, error) {
	// Migration fixtures and offline readers may consume genuine pre-12
	// snapshots. They retain the original sequence and cannot open a repair.
	// Open always upgrades production stores before exposing them.
	var version int
	if e := q.QueryRow("PRAGMA user_version").Scan(&version); e != nil {
		return nil, e
	}
	if version < 12 {
		return v.Definition.RequiredRoles, nil
	}
	r, e := reworkIn(q, t.ID)
	if errors.Is(e, ErrNotFound) {
		return v.Definition.RequiredRoles, nil
	}
	if e != nil {
		return nil, e
	}
	p, e := planIn(q, t.ID, t.PlanRevision)
	if e != nil || (v.Definition.Kind != "change" && v.Definition.Kind != "bugfix") || v.Design == nil || v.Approval == nil || r.PlanRevision != t.PlanRevision || r.PlanHash != p.Hash || r.Generation > t.Generation || r.DefinitionHash != v.Definition.Hash || r.DesignHash != v.Design.Snapshot.Hash || r.AcceptanceHash != v.Design.Snapshot.AcceptanceHash {
		return nil, ErrWorkflowGate
	}
	a, e := artifactReleasedIn(q, r.ReviewRunID)
	if e != nil || !validArtifactReview(a) || !a.Review.Valid || a.Review.Document.Verdict != "changes_required" || a.Reference.Binding.TaskID != t.ID || a.Reference.Binding.Generation != r.Generation || a.Reference.Binding.PlanRevision != r.PlanRevision || a.Review.TreeHash != r.TreeHash || a.Review.SpecHash != r.SpecHash || a.Review.TextHash != r.TextHash || a.Review.DesignHash != r.DesignHash || a.Review.AcceptanceHash != r.AcceptanceHash {
		return nil, ErrWorkflowGate
	}
	b, e := budgetIn(q, t.ID)
	if e != nil || b.UsedReworks != 1 || b.MaxReworks != 1 {
		return nil, ErrWorkflowGate
	}
	return []stageplan.Role{stageplan.Design, stageplan.Implementation, stageplan.Testing, stageplan.Review, stageplan.Implementation, stageplan.Testing, stageplan.Review, stageplan.Acceptance}, nil
}

func (s *Store) BeginReworkAuthorized(taskID string, expected TaskVersion, p ReworkEvidence, current func() bool) (ReworkRecord, error) {
	if p.owner != s || !p.Current() {
		return ReworkRecord{}, ErrWorkflowGate
	}
	var out ReworkRecord
	e := s.workflowTransaction(current, func(tx *sql.Tx) error {
		t, e := taskVersionIn(tx, taskID, expected)
		if e != nil {
			return e
		}
		if t.State != "needs_review" || expected.State != "needs_review" {
			return ErrWorkflowGate
		}
		v, e := workflowIn(tx, t)
		if e != nil || (v.Definition.Kind != "change" && v.Definition.Kind != "bugfix") || v.Design == nil || v.Approval == nil {
			return ErrWorkflowGate
		}
		if _, e = reworkIn(tx, t.ID); !errors.Is(e, ErrNotFound) {
			return ErrBudget
		}
		a, e := artifactReleasedIn(tx, p.artifact.Reference.Binding.RunID)
		if e != nil || !reflect.DeepEqual(a, p.artifact) || a.Reference.Binding.TaskID != t.ID || a.Reference.Binding.PlanRevision != t.PlanRevision || a.Reference.Binding.Generation != t.Generation || !reviewContextIn(tx, t, a) || !a.Review.Valid || a.Review.Document.Verdict != "changes_required" || !p.Current() {
			return ErrWorkflowGate
		}
		r, e := runIn(tx, a.Reference.Binding.RunID)
		if e != nil || r.Attempt != 1 {
			return ErrWorkflowGate
		}
		var count int
		if e = tx.QueryRow("SELECT COUNT(*) FROM stage_runs WHERE task_id=?", t.ID).Scan(&count); e != nil {
			return e
		}
		if count != 4 {
			return ErrWorkflowGate
		}
		b, e := budgetIn(tx, t.ID)
		if e != nil {
			return e
		}
		if b.MaxReworks != 1 || b.UsedReworks != 0 || b.UsedCalls >= b.MaxCalls {
			return ErrBudget
		}
		plan, e := planIn(tx, t.ID, t.PlanRevision)
		if e != nil {
			return e
		}
		out = ReworkRecord{TaskID: t.ID, ReviewRunID: r.ID, PlanRevision: t.PlanRevision, PlanHash: plan.Hash, Generation: t.Generation, DefinitionHash: v.Definition.Hash, TreeHash: a.Review.TreeHash, SpecHash: a.Review.SpecHash, DesignHash: a.Review.DesignHash, AcceptanceHash: a.Review.AcceptanceHash, TextHash: a.Review.TextHash}
		raw, _ := json.Marshal(out)
		if _, e = tx.Exec("INSERT INTO workflow_reworks VALUES(?,?,?,?)", t.ID, r.ID, string(raw), hash(raw)); e != nil {
			return e
		}
		if _, e = tx.Exec("UPDATE task_budgets SET used_reworks=used_reworks+1 WHERE task_id=?", t.ID); e != nil {
			return e
		}
		if _, e = tx.Exec("UPDATE tasks SET state='ready' WHERE id=?", t.ID); e != nil {
			return e
		}
		if e = event(tx, t.ID, "rework_started", r.ID, t.Generation); e != nil {
			return e
		}
		if !p.Current() {
			return ErrWorkflowGate
		}
		return nil
	})
	if e != nil {
		return ReworkRecord{}, e
	}
	return out, nil
}

// SuspendRework is a trusted denial transition after an owned continuation
// failed or was cancelled. It never launches, releases or grants permission.
func (s *Store) SuspendRework(taskID string, expected TaskVersion) error {
	return s.transaction(func(tx *sql.Tx) error {
		t, e := taskVersionIn(tx, taskID, expected)
		if e != nil {
			return e
		}
		if _, e = reworkIn(tx, t.ID); e != nil {
			return e
		}
		if t.State != "ready" || t.Generation == int64(1<<63-1) {
			return ErrConflict
		}
		var held int
		if e = tx.QueryRow("SELECT COUNT(*) FROM reservations JOIN stage_runs ON stage_runs.id=reservations.run_id WHERE stage_runs.task_id=? AND reservations.state='held'", t.ID).Scan(&held); e != nil {
			return e
		}
		if held != 0 {
			return ErrConflict
		}
		if _, e = tx.Exec("UPDATE tasks SET state='needs_review',generation=generation+1 WHERE id=?", t.ID); e != nil {
			return e
		}
		return event(tx, t.ID, "rework_stopped", "", t.Generation+1)
	})
}

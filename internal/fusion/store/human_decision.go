package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/fusion/evidence"
	"github.com/yetone/magpie/internal/fusion/handoff"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/workflow"
)

// FinalReport is presentation data only; it never authorizes human completion.
type FinalReport struct {
	RunID          string                      `json:"run_id"`
	TextHash       string                      `json:"text_hash"`
	TreeHash       string                      `json:"tree_hash"`
	SpecHash       string                      `json:"spec_hash"`
	DesignHash     string                      `json:"design_hash"`
	AcceptanceHash string                      `json:"acceptance_hash"`
	Model          workflow.AcceptanceDocument `json:"model"`
	ModelValid     bool                        `json:"model_valid"`
	Hard           evidence.Verdict            `json:"hard"`
}

// FinalEvidence cannot be constructed from JSON or presentation fields. Only
// the independently registered source/root/spec consumer may mint this value.
type FinalEvidence struct {
	owner    *Store
	artifact ArtifactRecord
	hard     evidence.Verdict
	bundles  []handoff.Bundle
	bindings []handoff.Binding
}

func (FinalEvidence) String() string { return "owned final evidence (redacted)" }
func (p FinalEvidence) Report() FinalReport {
	if p.owner == nil || p.artifact.Acceptance == nil {
		return FinalReport{}
	}
	a := p.artifact.Acceptance
	b, _ := json.Marshal(a.Document)
	var model workflow.AcceptanceDocument
	_ = json.Unmarshal(b, &model)
	return FinalReport{p.artifact.Reference.Binding.RunID, a.TextHash, a.TreeHash, a.SpecHash, a.DesignHash, a.AcceptanceHash, model, a.Valid, p.hard}
}
func (p FinalEvidence) filesCurrent() bool {
	if p.owner == nil || len(p.bundles) != 3 || len(p.bindings) != 3 {
		return false
	}
	for i, b := range p.bundles {
		if _, err := b.Read(p.bindings[i]); err != nil {
			return false
		}
	}
	return true
}

// Current only rechecks the owned filesystem witnesses; it grants no Store
// or Runtime authority and cannot be made true from presentation JSON.
func (p FinalEvidence) Current() bool { return p.filesCurrent() }

// FinalAcceptanceRun identifies metadata for the exact current released final
// stage. It is not a proof of current engineering evidence.
func (s *Store) FinalAcceptanceRun(taskID string, expected TaskVersion) (StageRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return StageRun{}, ErrClosed
	}
	t, err := taskVersionIn(s.db, taskID, expected)
	if err != nil {
		return StageRun{}, err
	}
	v, err := workflowIn(s.db, t)
	if err != nil {
		return StageRun{}, err
	}
	if v.Blocker != "workflow_complete" || len(v.Definition.RequiredRoles) == 0 || v.Definition.RequiredRoles[len(v.Definition.RequiredRoles)-1] != stageplan.Acceptance {
		return StageRun{}, ErrNotFound
	}
	var id string
	if err := s.db.QueryRow("SELECT id FROM stage_runs WHERE task_id=? AND generation=? AND plan_revision=? AND role='acceptance'", taskID, t.Generation, t.PlanRevision).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrNotFound
		}
		return StageRun{}, err
	}
	a, err := artifactReleasedIn(s.db, id)
	if err != nil || !validArtifactAcceptance(a) {
		return StageRun{}, ErrNotFound
	}
	return runIn(s.db, id)
}

func (s *Store) PrepareFinalEvidence(runID, source, stateRoot string, spec evidence.Spec) (FinalEvidence, error) {
	a, hard, err := s.AcceptanceArtifact(runID, source, stateRoot, spec)
	if err != nil {
		return FinalEvidence{}, err
	}
	task, err := s.Task(a.Reference.Binding.TaskID)
	if err != nil || task.PlanRevision != a.Reference.Binding.PlanRevision || task.Generation != a.Reference.Binding.Generation {
		return FinalEvidence{}, ErrConflict
	}
	p := FinalEvidence{owner: s, artifact: a, hard: hard}
	for _, id := range []string{runID, a.Acceptance.ReviewRunID, a.Acceptance.TestingRunID} {
		record, err := s.Artifact(id)
		if err != nil {
			return FinalEvidence{}, err
		}
		b, err := handoff.Restore(record.Reference, source, stateRoot)
		if err != nil {
			return FinalEvidence{}, ErrInvalid
		}
		p.bundles = append(p.bundles, b)
		p.bindings = append(p.bindings, record.Reference.Binding)
	}
	if !p.filesCurrent() {
		return FinalEvidence{}, ErrWorkflowGate
	}
	return p, nil
}

type HumanAcceptanceDecision struct {
	Version        int    `json:"version"`
	TaskID         string `json:"task_id"`
	PlanRevision   int64  `json:"plan_revision"`
	Generation     int64  `json:"generation"`
	RunID          string `json:"run_id"`
	TextHash       string `json:"text_hash"`
	TreeHash       string `json:"tree_hash"`
	SpecHash       string `json:"spec_hash"`
	DesignHash     string `json:"design_hash"`
	AcceptanceHash string `json:"acceptance_hash"`
	Action         string `json:"action"`
	Reason         string `json:"reason"`
	Authority      string `json:"authority"`
	At             string `json:"at"`
}

func humanReason(s string) bool {
	return utf8.ValidString(s) && strings.TrimSpace(s) != "" && len(s) <= 8192 && !strings.ContainsRune(s, 0)
}
func humanDecisionIn(q queryRow, taskID, runID string) (HumanAcceptanceDecision, error) {
	var raw, checksum string
	query := "SELECT decision_json,decision_hash FROM human_acceptance_decisions WHERE task_id=? ORDER BY generation DESC LIMIT 1"
	args := []any{taskID}
	if runID != "" {
		query = "SELECT decision_json,decision_hash FROM human_acceptance_decisions WHERE task_id=? AND run_id=?"
		args = append(args, runID)
	}
	if err := q.QueryRow(query, args...).Scan(&raw, &checksum); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return HumanAcceptanceDecision{}, ErrNotFound
		}
		return HumanAcceptanceDecision{}, err
	}
	var d HumanAcceptanceDecision
	if workflowJSON(raw, checksum, &d) != nil || d.Version != 1 || d.TaskID != taskID || d.Authority != "management" || !humanReason(d.Reason) || d.Action != "accept" && d.Action != "return" {
		return HumanAcceptanceDecision{}, ErrWorkflowGate
	}
	if at, err := time.Parse(time.RFC3339Nano, d.At); err != nil || at.Format(time.RFC3339Nano) != d.At {
		return HumanAcceptanceDecision{}, ErrWorkflowGate
	}
	a, err := artifactReleasedIn(q, d.RunID)
	if err != nil || !validArtifactAcceptance(a) {
		return HumanAcceptanceDecision{}, ErrWorkflowGate
	}
	b, v := a.Reference.Binding, a.Acceptance
	if b.TaskID != d.TaskID || b.PlanRevision != d.PlanRevision || b.Generation != d.Generation || v.TextHash != d.TextHash || v.TreeHash != d.TreeHash || v.SpecHash != d.SpecHash || v.DesignHash != d.DesignHash || v.AcceptanceHash != d.AcceptanceHash || d.Action == "accept" && (!v.Valid || v.Document.Verdict != "accepted") {
		return HumanAcceptanceDecision{}, ErrWorkflowGate
	}
	return d, nil
}

// HumanDecision reads a historical operator fact; fresh hard evidence is read
// separately. A past human accept does not make changed code/tests pass.
func (s *Store) HumanDecision(taskID string) (HumanAcceptanceDecision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return HumanAcceptanceDecision{}, ErrClosed
	}
	return humanDecisionIn(s.db, taskID, "")
}

// current must cover management and trusted verification-config authority,
// must not reenter Store, and is repeated before commit. Human actions never
// start a Runtime, reset a budget or replace actual Native observations.
func (s *Store) DecideHumanAcceptance(taskID string, expected TaskVersion, p FinalEvidence, action, reason string, current func() bool) (HumanAcceptanceDecision, error) {
	if p.owner != s || p.artifact.Reference.Binding.TaskID != taskID || action != "accept" && action != "return" || !humanReason(reason) {
		return HumanAcceptanceDecision{}, ErrInvalid
	}
	if action == "accept" && (p.hard.Status != evidence.Passed || !p.artifact.Acceptance.Valid || p.artifact.Acceptance.Document.Verdict != "accepted") {
		return HumanAcceptanceDecision{}, ErrWorkflowGate
	}
	var out HumanAcceptanceDecision
	err := s.workflowTransaction(func() bool { return current != nil && current() && p.filesCurrent() }, func(tx *sql.Tx) error {
		t, err := taskVersionIn(tx, taskID, expected)
		if err != nil {
			return err
		}
		b, v := p.artifact.Reference.Binding, p.artifact.Acceptance
		if t.PlanRevision != b.PlanRevision || t.Generation != b.Generation {
			return ErrConflict
		}
		a, err := artifactReleasedIn(tx, b.RunID)
		if err != nil || !reflect.DeepEqual(a, p.artifact) || !acceptanceContextIn(tx, t, a) {
			return ErrWorkflowGate
		}
		previous, err := humanDecisionIn(tx, taskID, b.RunID)
		if err == nil {
			if previous.Action != action || previous.Reason != reason || t.State != map[string]string{"accept": "completed", "return": "needs_review"}[action] {
				return ErrConflict
			}
			out = previous
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		if t.State != "advisory_only" && t.State != "needs_review" || action == "accept" && t.State != "advisory_only" {
			return ErrConflict
		}
		flow, err := workflowIn(tx, t)
		if err != nil || flow.Blocker != "workflow_complete" || b.Role != stageplan.Acceptance {
			return ErrWorkflowGate
		}
		out = HumanAcceptanceDecision{1, t.ID, t.PlanRevision, t.Generation, b.RunID, v.TextHash, v.TreeHash, v.SpecHash, v.DesignHash, v.AcceptanceHash, action, reason, "management", s.now().UTC().Format(time.RFC3339Nano)}
		raw, _ := json.Marshal(out)
		if _, err = tx.Exec("INSERT INTO human_acceptance_decisions VALUES(?,?,?,?,?)", b.RunID, t.ID, t.Generation, string(raw), hash(raw)); err != nil {
			return err
		}
		state, kind := "needs_review", "human_returned"
		if action == "accept" {
			state, kind = "completed", "human_accepted"
		}
		if _, err = tx.Exec("UPDATE tasks SET state=? WHERE id=?", state, t.ID); err != nil {
			return err
		}
		return event(tx, t.ID, kind, b.RunID, t.Generation)
	})
	if err != nil {
		return HumanAcceptanceDecision{}, err
	}
	return out, nil
}

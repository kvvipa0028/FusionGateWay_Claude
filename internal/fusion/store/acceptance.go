package store

import (
	"reflect"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/fusion/evidence"
	"github.com/yetone/magpie/internal/fusion/handoff"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/workflow"
)

// ArtifactAcceptance is an advisory model decision, never a human accepting
// delivery. Native output, hard tests and approved design remain distinct.
type ArtifactAcceptance struct {
	Text           string                      `json:"text"`
	TextHash       string                      `json:"text_hash"`
	Document       workflow.AcceptanceDocument `json:"document"`
	Valid          bool                        `json:"valid"`
	CriteriaCount  int                         `json:"criteria_count"`
	ReviewRunID    string                      `json:"review_run_id"`
	TestingRunID   string                      `json:"testing_run_id"`
	TreeHash       string                      `json:"tree_hash"`
	SpecHash       string                      `json:"spec_hash"`
	DesignHash     string                      `json:"design_hash"`
	AcceptanceHash string                      `json:"acceptance_hash"`
}

func acceptanceOpinion(raw string, count int) (workflow.AcceptanceDocument, bool) {
	d, err := workflow.ParseAcceptance(raw, count)
	if err != nil {
		return workflow.AcceptanceDocument{Version: 1, Verdict: "unverified", Criteria: []workflow.CriterionOpinion{}}, false
	}
	return d, true
}
func validArtifactAcceptance(a ArtifactRecord) bool {
	v := a.Acceptance
	if v == nil || a.Review != nil || a.Verification != nil || a.Reference.Binding.Role != stageplan.Acceptance || !utf8.ValidString(v.Text) || len(v.Text) > 1<<20 || v.TextHash != hash([]byte(v.Text)) || v.CriteriaCount < 1 || v.CriteriaCount > 128 || v.ReviewRunID != a.ParentRunID || !opaque(v.ReviewRunID) || !opaque(v.TestingRunID) || v.TreeHash != a.Reference.TreeHash || v.TreeHash != a.InputTreeHash || !validHash(v.SpecHash) || !validHash(v.DesignHash) || !validHash(v.AcceptanceHash) {
		return false
	}
	d, valid := acceptanceOpinion(v.Text, v.CriteriaCount)
	return valid == v.Valid && reflect.DeepEqual(d, v.Document)
}
func acceptanceContextIn(q workflowQuery, t Task, a ArtifactRecord) bool {
	if !validArtifactAcceptance(a) {
		return false
	}
	p, err := artifactReleasedIn(q, a.ParentRunID)
	if err != nil || p.Reference.Binding.TaskID != t.ID || !validArtifactReview(p) || !p.Review.Valid || p.Review.Document.Verdict != "approve" || p.Reference.TreeHash != a.Reference.TreeHash || !reviewContextIn(q, t, p) {
		return false
	}
	w, err := workflowIn(q, t)
	if err != nil || w.Design == nil || len(w.Design.Snapshot.Document.Acceptance) != a.Acceptance.CriteriaCount {
		return false
	}
	v := a.Acceptance
	return v.TestingRunID == p.Review.TestingRunID && v.SpecHash == p.Review.SpecHash && v.DesignHash == p.Review.DesignHash && v.AcceptanceHash == p.Review.AcceptanceHash
}

// Trusted producer only. raw must come from the same actually stopped Adapter
// Observation; public JSON cannot register decisions. current cannot reenter
// Store. Registration repeats run/parent/criteria checks and never releases.
func (s *Store) RecordAcceptanceArtifactAuthorized(a ArtifactRecord, raw string, current func() bool) error {
	if a.Acceptance != nil || a.Review != nil || a.Verification != nil || current == nil || !current() || a.Reference.Binding.Role != stageplan.Acceptance || len(raw) > 1<<20 || !utf8.ValidString(raw) {
		return ErrInvalid
	}
	p, err := s.Artifact(a.ParentRunID)
	if err != nil || !validArtifactReview(p) || !p.Review.Valid || p.Review.Document.Verdict != "approve" {
		return ErrWorkflowGate
	}
	w, err := s.Workflow(a.Reference.Binding.TaskID)
	if err != nil || w.Design == nil || w.Approval == nil {
		return ErrWorkflowGate
	}
	count := len(w.Design.Snapshot.Document.Acceptance)
	d, valid := acceptanceOpinion(raw, count)
	a.Acceptance = &ArtifactAcceptance{Text: raw, TextHash: hash([]byte(raw)), Document: d, Valid: valid, CriteriaCount: count, ReviewRunID: a.ParentRunID, TestingRunID: p.Review.TestingRunID, TreeHash: p.Reference.TreeHash, SpecHash: p.Review.SpecHash, DesignHash: p.Review.DesignHash, AcceptanceHash: p.Review.AcceptanceHash}
	if !validArtifactAcceptance(a) {
		return ErrInvalid
	}
	return s.recordArtifactAuthorized(a, current)
}

// AcceptanceArtifact independently reads released model opinion and current
// actual hard evidence. "accepted" remains advisory, not Task completion.
func (s *Store) AcceptanceArtifact(runID, source, stateRoot string, spec evidence.Spec) (ArtifactRecord, evidence.Verdict, error) {
	a, err := s.Artifact(runID)
	if err != nil {
		return ArtifactRecord{}, evidence.Verdict{}, err
	}
	if !validArtifactAcceptance(a) {
		return ArtifactRecord{}, evidence.Verdict{}, ErrInvalid
	}
	p, hard, err := s.ReviewedArtifact(a.Acceptance.ReviewRunID, source, stateRoot, spec)
	if err != nil {
		return ArtifactRecord{}, hard, err
	}
	if hard.Status != evidence.Passed {
		return a, hard, nil
	}
	v := a.Acceptance
	if !p.Review.Valid || p.Review.Document.Verdict != "approve" || p.Reference.TreeHash != a.Reference.TreeHash || p.Review.TestingRunID != v.TestingRunID || p.Review.SpecHash != v.SpecHash || p.Review.DesignHash != v.DesignHash || p.Review.AcceptanceHash != v.AcceptanceHash {
		return a, evidence.Verdict{Status: evidence.Superseded, Reason: "acceptance_inputs_changed"}, nil
	}
	if _, err := handoff.Restore(a.Reference, source, stateRoot); err != nil {
		return ArtifactRecord{}, evidence.Verdict{}, ErrInvalid
	}
	return a, hard, nil
}

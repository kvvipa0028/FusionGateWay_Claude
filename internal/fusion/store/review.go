package store

import (
	"reflect"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/fusion/evidence"
	"github.com/yetone/magpie/internal/fusion/handoff"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/workflow"
)

// ArtifactReview stores untrusted model opinion separately from actual tests
// and human approval. Only the trusted stopped Native producer can register it.
type ArtifactReview struct {
	Text           string                  `json:"text"`
	TextHash       string                  `json:"text_hash"`
	Document       workflow.ReviewDocument `json:"document"`
	Valid          bool                    `json:"valid"`
	TestingRunID   string                  `json:"testing_run_id"`
	TreeHash       string                  `json:"tree_hash"`
	SpecHash       string                  `json:"spec_hash"`
	DesignHash     string                  `json:"design_hash"`
	AcceptanceHash string                  `json:"acceptance_hash"`
}

func reviewOpinion(raw string) (workflow.ReviewDocument, bool) {
	d, err := workflow.ParseReview(raw)
	if err != nil {
		return workflow.ReviewDocument{Version: 1, Verdict: "unverified", Findings: []workflow.ReviewFinding{}}, false
	}
	return d, true
}
func validArtifactReview(a ArtifactRecord) bool {
	r := a.Review
	if r == nil || a.Verification != nil || a.Reference.Binding.Role != stageplan.Review || !utf8.ValidString(r.Text) || len(r.Text) > 1<<20 || r.TextHash != hash([]byte(r.Text)) || r.TestingRunID != a.ParentRunID || !opaque(r.TestingRunID) || r.TreeHash != a.Reference.TreeHash || r.TreeHash != a.InputTreeHash || !validHash(r.SpecHash) || !validHash(r.DesignHash) || !validHash(r.AcceptanceHash) {
		return false
	}
	d, valid := reviewOpinion(r.Text)
	return r.Valid == valid && reflect.DeepEqual(d, r.Document)
}
func reviewContextIn(q workflowQuery, t Task, a ArtifactRecord) bool {
	if !validArtifactReview(a) {
		return false
	}
	p, err := artifactReleasedIn(q, a.ParentRunID)
	if err != nil || p.Reference.Binding.TaskID != t.ID || !validArtifactVerification(p) || p.Verification.Data.Verdict.Status != evidence.Passed || p.Reference.TreeHash != a.Reference.TreeHash || !verificationContextIn(q, t, p.Verification) {
		return false
	}
	r := a.Review
	return r.SpecHash == p.Verification.Data.Record.SpecHash && r.DesignHash == p.Verification.DesignHash && r.AcceptanceHash == p.Verification.AcceptanceHash
}

// Trusted producer only: raw must be obtained from the same Adapter's actual
// Observation after owned Wait/StopProof verification. It never grants model
// authority. Public artifact writes reject declared Review. current cannot
// reenter Store; immutable run/parent/criteria fences are repeated in transaction.
func (s *Store) RecordReviewedArtifactAuthorized(a ArtifactRecord, raw string, current func() bool) error {
	if a.Review != nil || a.Verification != nil || current == nil || !current() || a.Reference.Binding.Role != stageplan.Review || len(raw) > 1<<20 || !utf8.ValidString(raw) {
		return ErrInvalid
	}
	p, err := s.Artifact(a.ParentRunID)
	if err != nil || !validArtifactVerification(p) || p.Verification.Data.Verdict.Status != evidence.Passed {
		return ErrWorkflowGate
	}
	d, valid := reviewOpinion(raw)
	a.Review = &ArtifactReview{Text: raw, TextHash: hash([]byte(raw)), Document: d, Valid: valid, TestingRunID: a.ParentRunID, TreeHash: p.Reference.TreeHash, SpecHash: p.Verification.Data.Record.SpecHash, DesignHash: p.Verification.DesignHash, AcceptanceHash: p.Verification.AcceptanceHash}
	if !validArtifactReview(a) {
		return ErrInvalid
	}
	return s.recordArtifactAuthorized(a, current)
}

// ReviewedArtifact preserves independent hard evidence and model opinion.
// A returned "approve" is advisory, never human acceptance or Task completion.
func (s *Store) ReviewedArtifact(runID, source, stateRoot string, spec evidence.Spec) (ArtifactRecord, evidence.Verdict, error) {
	a, err := s.Artifact(runID)
	if err != nil {
		return ArtifactRecord{}, evidence.Verdict{}, err
	}
	if !validArtifactReview(a) {
		return ArtifactRecord{}, evidence.Verdict{}, ErrInvalid
	}
	p, hard, err := s.VerifiedArtifact(a.Review.TestingRunID, source, stateRoot, spec)
	if err != nil {
		return ArtifactRecord{}, hard, err
	}
	if hard.Status != evidence.Passed {
		return a, hard, nil
	}
	if p.Reference.TreeHash != a.Reference.TreeHash || p.Verification.Data.Record.SpecHash != a.Review.SpecHash || p.Verification.DesignHash != a.Review.DesignHash || p.Verification.AcceptanceHash != a.Review.AcceptanceHash {
		return a, evidence.Verdict{Status: evidence.Superseded, Reason: "review_inputs_changed"}, nil
	}
	if _, err := handoff.Restore(a.Reference, source, stateRoot); err != nil {
		return ArtifactRecord{}, evidence.Verdict{}, ErrInvalid
	}
	return a, hard, nil
}

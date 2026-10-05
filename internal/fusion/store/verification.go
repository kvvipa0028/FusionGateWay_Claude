package store

import (
	"errors"

	"github.com/yetone/magpie/internal/fusion/evidence"
	"github.com/yetone/magpie/internal/fusion/handoff"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/workspace"
)

type ArtifactVerification struct {
	Data           evidence.Stored `json:"data"`
	DesignHash     string          `json:"design_hash"`
	AcceptanceHash string          `json:"acceptance_hash"`
}

func validArtifactVerification(a ArtifactRecord) bool {
	v := a.Verification
	return v != nil && a.Review == nil && a.Acceptance == nil && a.Reference.Binding.Role == stageplan.Testing && evidence.ValidStored(v.Data) && v.Data.ArtifactPath == a.Reference.Path && v.Data.Record.ArtifactHash == a.Reference.TreeHash && (v.DesignHash == "" && v.AcceptanceHash == "" || validHash(v.DesignHash) && validHash(v.AcceptanceHash))
}
func verificationContextIn(q workflowQuery, t Task, v *ArtifactVerification) bool {
	w, err := workflowIn(q, t)
	if errors.Is(err, ErrNotFound) {
		return v.DesignHash == "" && v.AcceptanceHash == ""
	}
	return err == nil && w.Design != nil && w.Approval != nil && v.DesignHash == w.Design.Snapshot.Hash && v.AcceptanceHash == w.Design.Snapshot.AcceptanceHash && w.Approval.DesignHash == v.DesignHash && w.Approval.AcceptanceHash == v.AcceptanceHash
}

// Only an owned, actually stopped executor Result can populate Verification.
// Native stop/Task/Plan/parent/source fences remain independent requirements.
// current must not reenter Store; registration never releases a reservation.
func (s *Store) RecordVerifiedArtifactAuthorized(a ArtifactRecord, r evidence.Result, frozen workspace.FrozenArtifact, spec evidence.Spec, current func() bool) error {
	if a.Verification != nil || a.Review != nil || a.Acceptance != nil || current == nil || !current() {
		return ErrInvalid
	}
	v, err := evidence.Export(r, frozen, spec)
	if err != nil || v.ArtifactPath != a.Reference.Path || v.Record.ArtifactHash != a.Reference.TreeHash || a.Reference.Binding.Role != stageplan.Testing {
		return ErrInvalid
	}
	metadata := ArtifactVerification{Data: v}
	w, err := s.Workflow(a.Reference.Binding.TaskID)
	if err == nil {
		if w.Design == nil || w.Approval == nil {
			return ErrConflict
		}
		metadata.DesignHash = w.Design.Snapshot.Hash
		metadata.AcceptanceHash = w.Design.Snapshot.AcceptanceHash
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	a.Verification = &metadata
	if !validArtifactVerification(a) {
		return ErrInvalid
	}
	return s.recordArtifactAuthorized(a, current)
}

// VerifiedArtifact is a controlled consumer: the exact private, hash-verified
// released-run receipt precedes independent source/root rehydration. Stored
// JSON alone does not establish report origin or authorize a next stage.
func (s *Store) VerifiedArtifact(runID, source, stateRoot string, spec evidence.Spec) (ArtifactRecord, evidence.Verdict, error) {
	a, err := s.Artifact(runID)
	if err != nil {
		return ArtifactRecord{}, evidence.Verdict{}, err
	}
	if !validArtifactVerification(a) {
		return ArtifactRecord{}, evidence.Verdict{Status: evidence.Unverified, Reason: "no_verified_execution"}, ErrInvalid
	}
	s.mu.Lock()
	if s.db == nil {
		s.mu.Unlock()
		return ArtifactRecord{}, evidence.Verdict{}, ErrClosed
	}
	t, err := taskIn(s.db, a.Reference.Binding.TaskID)
	context := err == nil && verificationContextIn(s.db, t, a.Verification)
	s.mu.Unlock()
	if !context {
		return a, evidence.Verdict{Status: evidence.Superseded, Reason: "design_or_standard_changed"}, nil
	}
	bundle, err := handoff.Restore(a.Reference, source, stateRoot)
	if err != nil {
		return ArtifactRecord{}, evidence.Verdict{}, ErrInvalid
	}
	doc, err := bundle.Read(a.Reference.Binding)
	if err != nil || !doc.Evidence.TestsExecuted || doc.Evidence.Status != string(a.Verification.Data.Verdict.Status) {
		return ArtifactRecord{}, evidence.Verdict{}, ErrInvalid
	}
	frozen, err := bundle.Artifact(a.Reference.Binding)
	if err != nil {
		return ArtifactRecord{}, evidence.Verdict{}, ErrInvalid
	}
	return a, evidence.EvaluateStored(a.Verification.Data, frozen, spec), nil
}

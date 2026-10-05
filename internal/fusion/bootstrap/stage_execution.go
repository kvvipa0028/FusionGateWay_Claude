package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"github.com/yetone/magpie/internal/fusion/control"
	"github.com/yetone/magpie/internal/fusion/evidence"
	"github.com/yetone/magpie/internal/fusion/handoff"
	"github.com/yetone/magpie/internal/fusion/policy"
	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
	"github.com/yetone/magpie/internal/fusion/workspace"
)

type stageExecutionConfig struct {
	ProjectID, ExecutionRoot string
	TestingWritePaths        []string
	Verification             *StageVerificationConfig
	Timeout                  time.Duration
}
type stageObservation struct{ State, SessionID string }

// stageExecutionRegistration owns the provider-independent frozen source,
// released artifact, hard verification, advisory review and rework contracts.
// current performs the full credential/Registry/epoch recheck and runs outside
// the Store lock only; registrationCurrent is the bounded local authority the
// Store may call inside its own authorization-lock transaction, so it must
// never read the Store or call external services.
func stageExecutionRegistration(ctx context.Context, e RuntimeEnvironment, p Project, route stageplan.Route, c stageExecutionConfig, current func(context.Context, stageplan.ExecutionTarget) bool, registrationCurrent func() bool, inspect func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (policy.Inspection, error), backend control.Backend, verifyStop func(policy.StopProof) bool, observe func(string, int64) (stageObservation, string, error), closeRuntime func(context.Context) error) RuntimeRegistration {
	resolve := func(call context.Context, task store.Task, role stageplan.Role, target stageplan.ExecutionTarget) (control.Launch, error) {
		plan, err := e.Store.Plan(task.ID, task.PlanRevision)
		if err != nil {
			return control.Launch{}, control.ErrUnsupported
		}
		binding := plan.Bindings[role]
		match := binding.Target != nil && reflect.DeepEqual(*binding.Target, target)
		for _, candidate := range binding.Candidates {
			match = match || reflect.DeepEqual(candidate, target)
		}
		live, err := e.Store.Task(task.ID)
		if err != nil || !reflect.DeepEqual(live, task) || !match {
			return control.Launch{}, control.ErrIdentity
		}
		stage, err := e.Store.StageArtifactInput(task.ID, store.TaskVersion{PlanRevision: task.PlanRevision, Generation: task.Generation, State: task.State}, role)
		if err != nil || glmHasMultipleRoles(stage) && !p.Write && (role == stageplan.Implementation || role == stageplan.Testing) {
			return control.Launch{}, control.ErrUnsupported
		}
		if role == stageplan.Testing && glmHasMultipleRoles(stage) && !glmVerificationMatches(c.Verification, stage) {
			return control.Launch{}, control.ErrUnsupported
		}
		write := p.Write && (role == stageplan.Implementation || role == stageplan.Testing)
		var writePaths []string
		if write {
			var ok bool
			writePaths, ok = glmStageWritePaths(role, stage, c.TestingWritePaths)
			if !ok {
				return control.Launch{}, control.ErrUnsupported
			}
		}
		in, err := inspect(call, task, role, target)
		if err != nil || !in.DataAllowed {
			return control.Launch{}, control.ErrForbidden
		}
		prompt := glmStagePrompt{Role: role, Goal: task.Goal, Workflow: stage.Workflow, WritePaths: writePaths}
		if role == stageplan.Implementation && stage.Parent != nil && stage.Parent.Reference.Binding.Role == stageplan.Review {
			if c.Verification == nil || !glmVerificationMatches(c.Verification, stage) {
				return control.Launch{}, control.ErrUnsupported
			}
			review, hard, err := e.Store.ReviewedArtifact(stage.Parent.Reference.Binding.RunID, p.Path, c.ExecutionRoot, c.Verification.Spec)
			if err != nil || hard.Status != evidence.Passed || review.Review == nil || !review.Review.Valid || review.Review.Document.Verdict != "changes_required" || !reflect.DeepEqual(review, *stage.Parent) {
				return control.Launch{}, control.ErrUnsupported
			}
			prompt.Review = &glmAcceptanceReview{RunID: review.Reference.Binding.RunID, TextHash: review.Review.TextHash, Document: review.Review.Document}
		}
		if role == stageplan.Review && glmHasMultipleRoles(stage) {
			if !glmVerificationMatches(c.Verification, stage) || stage.Parent == nil || stage.Parent.Reference.Binding.Role != stageplan.Testing {
				return control.Launch{}, control.ErrUnsupported
			}
			testing, hard, err := e.Store.VerifiedArtifact(stage.Parent.Reference.Binding.RunID, p.Path, c.ExecutionRoot, c.Verification.Spec)
			if err != nil || hard.Status != evidence.Passed || !reflect.DeepEqual(testing, *stage.Parent) {
				return control.Launch{}, control.ErrUnsupported
			}
			v := testing.Verification.Data.Record
			prompt.Verification = &glmReviewVerification{TestingRunID: testing.Reference.Binding.RunID, ArtifactHash: v.ArtifactHash, SuiteHash: v.SuiteHash, SpecHash: v.SpecHash, AcceptanceHash: testing.Verification.AcceptanceHash, ReportHash: v.ReportHash, ToolVersion: v.ToolVersion, ExitCode: v.ExitCode, Tests: hard.Tests, Skipped: hard.Skipped}
			prompt.ReviewResponseContract = `Return only one JSON object, at most 64 KiB: {"version":1,"verdict":"approve|changes_required|unverified","findings":[{"id":"unique finding id","severity":"blocking|nonblocking","summary":"concrete issue and required correction"}]}. No unknown keys. approve cannot include blocking findings; changes_required requires findings. Review the frozen code, approved design and actual test summary. Your opinion is advisory and cannot change hard test results, criteria, permissions, or human acceptance. Do not edit files.`
		}
		if role == stageplan.Acceptance && glmHasMultipleRoles(stage) {
			if !glmVerificationMatches(c.Verification, stage) || stage.Parent == nil || stage.Parent.Reference.Binding.Role != stageplan.Review {
				return control.Launch{}, control.ErrUnsupported
			}
			reviewed, hard, err := e.Store.ReviewedArtifact(stage.Parent.Reference.Binding.RunID, p.Path, c.ExecutionRoot, c.Verification.Spec)
			if err != nil || hard.Status != evidence.Passed || reviewed.Review == nil || !reviewed.Review.Valid || reviewed.Review.Document.Verdict != "approve" || !reflect.DeepEqual(reviewed, *stage.Parent) {
				return control.Launch{}, control.ErrUnsupported
			}
			testing, err := e.Store.Artifact(reviewed.Review.TestingRunID)
			if err != nil || testing.Verification == nil || testing.Reference.TreeHash != reviewed.Reference.TreeHash {
				return control.Launch{}, control.ErrUnsupported
			}
			v := testing.Verification.Data.Record
			prompt.Verification = &glmReviewVerification{TestingRunID: testing.Reference.Binding.RunID, ArtifactHash: v.ArtifactHash, SuiteHash: v.SuiteHash, SpecHash: v.SpecHash, AcceptanceHash: testing.Verification.AcceptanceHash, ReportHash: v.ReportHash, ToolVersion: v.ToolVersion, ExitCode: v.ExitCode, Tests: hard.Tests, Skipped: hard.Skipped}
			prompt.Review = &glmAcceptanceReview{RunID: reviewed.Reference.Binding.RunID, TextHash: reviewed.Review.TextHash, Document: reviewed.Review.Document}
			prompt.AcceptanceResponseContract = `Return only one JSON object, at most 64 KiB: {"version":1,"verdict":"accepted|rejected|unverified","criteria":[{"index":0,"status":"met|not_met|unverified","reason":"concrete evidence or missing evidence"}]}. Cover every frozen approved acceptance criterion once by its zero-based index, no unknown keys. accepted requires all criteria met; rejected requires a not_met criterion. Hard test results and review remain independent facts. This direct backend provides no HIL/WCET/hardware evidence; leave unsupported criteria unverified. Your opinion is advisory, not human acceptance, Task completion or deployment permission. Do not edit files or change criteria.`
		}
		var parent handoff.Bundle
		var inputEntries []workspace.ArtifactEntry
		if stage.Parent != nil {
			parent, err = handoff.Restore(stage.Parent.Reference, p.Path, c.ExecutionRoot)
			if err != nil {
				return control.Launch{}, control.ErrForbidden
			}
			doc, err := parent.Read(stage.Parent.Reference.Binding)
			if err != nil || doc.Task.Goal != task.Goal {
				return control.Launch{}, control.ErrIdentity
			}
			inputEntries = doc.Artifact.Entries
			prompt.Parent = &glmStageParent{Binding: doc.Binding, TreeHash: doc.Artifact.TreeHash, BaseHash: doc.Artifact.BaseTreeHash, ChangeHash: doc.Artifact.ChangeHash, HeaderHash: stage.Parent.Reference.HeaderHash, Evidence: doc.Evidence.Status, Pending: doc.Pending}
		}
		input, err := json.Marshal(prompt)
		if err != nil || len(input) > 64<<10 {
			return control.Launch{}, control.ErrUnsupported
		}
		// This directory and its copy belong only to this launch. Retain it
		// for later evidence/reconciliation; never delete an unknown run.
		launchRoot, err := os.MkdirTemp(c.ExecutionRoot, "launch-")
		if err != nil {
			return control.Launch{}, control.ErrForbidden
		}
		worker := filepath.Join(launchRoot, "worker")
		if os.Mkdir(worker, 0700) != nil {
			return control.Launch{}, control.ErrForbidden
		}
		var snapshot workspace.Snapshot
		if stage.Parent == nil {
			snapshot, err = workspace.Copy(p.Path, launchRoot, "workspace")
		} else {
			snapshot, err = parent.Copy(stage.Parent.Reference.Binding, launchRoot, "workspace")
		}
		if err != nil || !current(call, target) {
			return control.Launch{}, control.ErrForbidden
		}
		guard, err := snapshot.Guard()
		if err != nil {
			return control.Launch{}, control.ErrForbidden
		}
		// This backend owns one launch and one artifact, never a shared
		// mutable last-workspace pointer across tasks or runs.
		local := backend
		var handle control.Execution
		var handleMu, releaseMu sync.Mutex
		var bundle handoff.Bundle
		var verification evidence.Result
		published := false
		local.Start = func(ctx context.Context, run store.StageRun, spec managed.Spec) (control.Execution, error) {
			if run.TaskID != task.ID || run.Role != role || run.PlanRevision != plan.Revision || run.Generation != task.Generation+1 || !reflect.DeepEqual(run.Target, target) || spec.Root != worker || spec.Workspace != snapshot.Path || spec.Writable != write || !reflect.DeepEqual(spec.WritePaths, writePaths) || !bytes.Equal(spec.Input, input) || !current(ctx, target) || !guard.ValidFor(snapshot.Path) || !glmStageContextCurrent(e.Store, task, stage) {
				return nil, control.ErrIdentity
			}
			h, err := backend.Start(ctx, run, spec)
			handleMu.Lock()
			handle = h
			handleMu.Unlock()
			return h, err
		}
		local.Release = func(proof policy.StopProof) error {
			releaseMu.Lock()
			defer releaseMu.Unlock()
			if !verifyStop(proof) {
				return control.ErrReconcile
			}
			handleMu.Lock()
			h := handle
			handleMu.Unlock()
			if h == nil {
				return control.ErrReconcile
			}
			result, err := h.Wait(context.Background())
			run, runErr := e.Store.Run(proof.RunID)
			if err != nil || runErr != nil || !result.StoppedVerified || result.Proof != proof || run.TaskID != task.ID || run.Role != role || run.PlanRevision != plan.Revision || run.Generation != proof.Generation || run.NativeSessionID != proof.NativeSessionID || !reflect.DeepEqual(run.Target, target) {
				return control.ErrReconcile
			}
			if run.State != "succeeded" {
				return backend.Release(proof)
			}
			identityCurrent := func() bool {
				live, err := e.Store.Task(task.ID)
				return err == nil && live.PlanRevision == plan.Revision && live.Generation == run.Generation && live.Goal == task.Goal && current(ctx, target) && guard.ValidFor(snapshot.Path) && glmStageContextCurrent(e.Store, task, stage)
			}
			if result.State != "succeeded" || !identityCurrent() {
				return control.ErrReconcile
			}
			raw, _ := json.Marshal(target)
			binding := handoff.Binding{TaskID: task.ID, PlanRevision: plan.Revision, PlanHash: plan.Hash, RunID: run.ID, Generation: run.Generation, Role: role, TargetHash: handoff.Hash(raw)}
			if !published {
				artifact, err := workspace.Freeze(snapshot, launchRoot, "handoff")
				if err != nil {
					return control.ErrReconcile
				}
				if stage.Parent != nil && (write && len(writePaths) > 0 && !workspace.ChangesWithin(inputEntries, artifact.Manifest().Entries, writePaths) || !write && artifact.Manifest().TreeHash != stage.Parent.Reference.TreeHash) {
					return control.ErrReconcile
				}
				nativeEvidence := handoff.Evidence{OutputHash: result.OutputHash, StopProofHash: proof.ReportHash}
				if role == stageplan.Testing && c.Verification != nil {
					verificationCurrent := func() bool {
						live, err := e.Store.Task(task.ID)
						return err == nil && live.State == "ready" && identityCurrent()
					}
					verification, err = glmRunVerification(ctx, artifact, c.ExecutionRoot, c.Verification.Spec, verificationCurrent)
					if err != nil || !verification.Record().Stopped || !identityCurrent() {
						return control.ErrReconcile
					}
					bundle, err = handoff.PublishVerified(artifact, binding, task.Goal, nativeEvidence, verification, c.Verification.Spec)
				} else {
					bundle, err = handoff.Publish(artifact, binding, task.Goal, nativeEvidence)
				}
				if err != nil {
					return control.ErrReconcile
				}
				published = true
			}
			if _, err := bundle.Read(binding); err != nil || !identityCurrent() {
				return control.ErrReconcile
			}
			reference, err := bundle.Reference(c.ExecutionRoot)
			if err != nil {
				return control.ErrReconcile
			}
			// Never call the Store-reading identityCurrent under Store.mu.
			// Recheck its Task/Plan/Run fields inside RecordArtifact itself.
			record := store.ArtifactRecord{Reference: reference, InputTreeHash: reference.BaseTreeHash}
			if stage.Parent != nil {
				record.ParentRunID, record.InputTreeHash = stage.Parent.Reference.Binding.RunID, stage.Parent.Reference.TreeHash
			}
			// The full current() recheck (Registry/identity/credential) runs
			// above and below via identityCurrent; inside the Store
			// transaction only the bounded local authority is allowed.
			storeCurrent := registrationCurrent
			if role == stageplan.Testing && c.Verification != nil {
				frozen, getErr := bundle.Artifact(binding)
				if getErr != nil {
					return control.ErrReconcile
				}
				err = e.Store.RecordVerifiedArtifactAuthorized(record, verification, frozen, c.Verification.Spec, storeCurrent)
			} else if role == stageplan.Review && glmHasMultipleRoles(stage) {
				observed, raw, observeErr := observe(run.ID, run.Generation)
				if observeErr != nil || observed.State != "succeeded" || observed.SessionID != run.NativeSessionID || prompt.Verification == nil {
					return control.ErrReconcile
				}
				err = e.Store.RecordReviewedArtifactAuthorized(record, raw, storeCurrent)
			} else if role == stageplan.Acceptance && glmHasMultipleRoles(stage) {
				observed, raw, observeErr := observe(run.ID, run.Generation)
				if observeErr != nil || observed.State != "succeeded" || observed.SessionID != run.NativeSessionID || prompt.Verification == nil || prompt.Review == nil {
					return control.ErrReconcile
				}
				err = e.Store.RecordAcceptanceArtifactAuthorized(record, raw, storeCurrent)
			} else {
				err = e.Store.RecordArtifactAuthorized(record, storeCurrent)
			}
			if err != nil || !identityCurrent() {
				return control.ErrReconcile
			}
			// Indexed metadata is still pending until the exact released
			// StopProof matches. Failure retains reservations for reconciliation.
			return backend.Release(proof)
		}
		return control.Launch{Backend: local, Spec: managed.Spec{Root: worker, Workspace: snapshot.Path, Source: guard, Input: input, Timeout: c.Timeout, Writable: write, WritePaths: append([]string(nil), writePaths...)}}, nil
	}
	finalEvidence := func(call context.Context, task store.Task, run store.StageRun) (store.FinalEvidence, func() bool, error) {
		authority := func() bool { return task.ProjectID == p.ID && current(call, run.Target) }
		if c.Verification == nil || run.TaskID != task.ID || run.Role != stageplan.Acceptance || !authority() {
			return store.FinalEvidence{}, nil, control.ErrUnsupported
		}
		w, err := e.Store.Workflow(task.ID)
		if err != nil || w.Design == nil || w.Approval == nil || !reflect.DeepEqual(c.Verification.Acceptance, w.Design.Snapshot.Document.Acceptance) {
			return store.FinalEvidence{}, nil, control.ErrUnsupported
		}
		proof, err := e.Store.PrepareFinalEvidence(run.ID, p.Path, c.ExecutionRoot, c.Verification.Spec)
		if err != nil || !authority() {
			if err == nil {
				err = control.ErrUnsupported
			}
			return store.FinalEvidence{}, nil, err
		}
		return proof, authority, nil
	}
	afterRelease := func(call context.Context, run store.StageRun) (*control.Followup, error) {
		authority := func() bool { return e.Manager.ExecutionEnabled() && current(call, run.Target) }
		if !authority() {
			return nil, control.ErrForbidden
		}
		task, err := e.Store.Task(run.TaskID)
		if err != nil || task.ProjectID != p.ID || task.PlanRevision != run.PlanRevision || task.Generation != run.Generation || run.State != "succeeded" {
			return nil, control.ErrIdentity
		}
		round, err := e.Store.Rework(task.ID)
		if errors.Is(err, store.ErrNotFound) {
			if run.Role != stageplan.Review || run.Attempt != 1 || task.State != "needs_review" || c.Verification == nil {
				return nil, nil
			}
			var proof store.ReworkEvidence
			proof, err = e.Store.PrepareReworkEvidence(run.ID, p.Path, c.ExecutionRoot, c.Verification.Spec)
			if err != nil {
				return nil, err
			}
			round, err = e.Store.BeginReworkAuthorized(task.ID, store.TaskVersion{PlanRevision: task.PlanRevision, Generation: task.Generation, State: task.State}, proof, authority)
			if err != nil {
				return nil, err
			}
			task, err = e.Store.Task(task.ID)
		}
		if err != nil {
			return nil, err
		}
		if task.State != "ready" || run.Generation < round.Generation {
			return nil, nil
		}
		view, err := e.Store.Workflow(task.ID)
		if err != nil || view.Blocker != "" || view.Next == "" || !authority() {
			return nil, control.ErrUnsupported
		}
		return &control.Followup{Key: "rework-" + round.ReviewRunID + "-" + string(view.Next), Identity: store.StartIdentity{TaskID: task.ID, Role: view.Next, PlanRevision: task.PlanRevision, Generation: task.Generation}, Current: func(next context.Context) bool { return next != nil && next.Err() == nil && authority() }}, nil
	}
	return RuntimeRegistration{Routes: map[string][]stageplan.Route{p.ID: {route}}, Inspect: inspect, Resolve: resolve, FinalEvidence: finalEvidence, AfterRelease: afterRelease, Close: closeRuntime}

}

// Watch revocation independently of a pending Native or upstream request.
func watchStageExecution(backend control.Backend, current func(context.Context, stageplan.ExecutionTarget) bool) control.Backend {
	start := backend.Start
	backend.Start = func(parent context.Context, run store.StageRun, spec managed.Spec) (control.Execution, error) {
		owned, cancel := context.WithCancel(parent)
		done := make(chan struct{})
		// Source changes already have host/Controller fences. Independently
		// revoked route evidence, rotated credentials or replaced state
		// must also stop a Native process while its request is in flight.
		go func() {
			tick := time.NewTicker(100 * time.Millisecond)
			defer tick.Stop()
			for {
				select {
				case <-done:
					return
				case <-owned.Done():
					return
				case <-tick.C:
					if !current(owned, run.Target) {
						cancel()
						return
					}
				}
			}
		}()
		execution, err := start(owned, run, spec)
		if execution == nil {
			close(done)
			cancel()
			return nil, err
		}
		// Preserve any known handle on failure. Only its terminal Wait can
		// retire the watcher; Controller still owns stop proof and release.
		go func() {
			execution.Wait(context.Background())
			close(done)
			cancel()
		}()
		return execution, err
	}
	return backend
}

package grok

import (
	"context"
	"errors"
	"sort"

	"github.com/yetone/magpie/internal/fusion/store"
)

// Checkpoint accepts only an execution owned by this Adapter. A successful
// Native end, files on disk, or a caller's StopProof is never sufficient.
// It creates private recovery data, not a new execution or resume capability.
func (a *Adapter) Checkpoint(ctx context.Context, runID string, generation int64, archives *Archives) (CheckpointRef, error) {
	if a == nil || archives == nil || ctx.Err() != nil {
		return CheckpointRef{}, ErrUnverified
	}
	a.mu.Lock()
	v, ok := a.observations[runID]
	var record checkpointRecord
	if ok && v.checkpoint != nil {
		record = *v.checkpoint
		record.reads = append([]archiveRead(nil), record.reads...)
	}
	a.mu.Unlock()
	if !ok || v.generation != generation || v.handle == nil || v.checkpoint == nil || v.outcome.State != "succeeded" {
		return CheckpointRef{}, ErrIdentity
	}
	result, e := v.handle.Wait(ctx)
	if e != nil || result.State != "succeeded" || !result.StoppedVerified || !a.VerifyStop(result.Proof) || result.Proof != v.released {
		return CheckpointRef{}, ErrUnverified
	}
	run, e := a.config.Scheduler.Store.Run(runID)
	// Finish clears the lease owner. Ownership is established by this Adapter's
	// retained Handle and exact Supervisor proof, not a stale active lease.
	if e != nil || run.Generation != generation || run.State != "succeeded" || run.Owner != "" || record.owner == "" || run.TaskID != record.run.TaskID || run.Role != record.run.Role || run.Attempt != record.run.Attempt || run.PlanRevision != record.run.PlanRevision || !sameTarget(run.Target, record.run.Target) || run.NativeSessionID != v.outcome.SessionID || run.NativeSessionID != result.Proof.NativeSessionID {
		return CheckpointRef{}, ErrIdentity
	}
	if _, e = a.config.Scheduler.Store.Reservation(runID); !errors.Is(e, store.ErrNotFound) {
		return CheckpointRef{}, ErrUnverified
	}
	task, e := a.config.Scheduler.Store.Task(run.TaskID)
	if e != nil || task.ProjectID != record.projectID || task.State == "cancelled" || task.State == "cancelling" {
		return CheckpointRef{}, ErrIdentity
	}
	record.run, record.proof = run, result.Proof
	return archives.capture(ctx, record)
}

// Snapshot only already-authorized, completed and correlated Read records
// while the original live scope is still valid. Restoring transcript bytes
// cannot manufacture these grants. Raw text stays in the sealed private data.
func (r *ReadTools) checkpointReads() ([]archiveRead, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.check() != nil || r.pending != "" {
		return nil, ErrIdentity
	}
	ids := make([]string, 0, len(r.records))
	for id := range r.records {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	reads := make([]archiveRead, 0, len(ids))
	for _, id := range ids {
		v := r.records[id]
		identity, ok := archiveIdentityOf(v.info)
		if !ok || !v.started || !v.located || !v.done || !v.continued {
			return nil, ErrUnverified
		}
		reads = append(reads, archiveRead{Call: v.call, Argument: v.argument, Path: v.path, Relative: v.relative, Raw: v.raw, Identity: identity, Mode: uint32(v.info.Mode()), Bytes: v.info.Size(), Modified: v.info.ModTime().UnixNano()})
	}
	return reads, nil
}

package control

import (
	"context"
	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/runtime/grok"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

// BindGrokCheckpoint wires trusted private archives and the owning Adapter.
// The reference never supplies executable, source permissions or a stop proof.
func BindGrokCheckpoint(a *grok.Adapter, archives *grok.Archives) Backend {
	if a == nil || archives == nil {
		return Backend{}
	}
	b := BindAdapter(a)
	b.Checkpoint = func(ctx context.Context, run store.StageRun) (CheckpointRef, error) {
		ref, e := a.Checkpoint(ctx, run.ID, run.Generation, archives)
		return CheckpointRef{ID: ref.ID, Digest: ref.Digest}, e
	}
	b.CheckRestore = func(ctx context.Context, origin store.StageRun, target stageplan.ExecutionTarget, spec managed.Spec, identity store.RestoreIdentity) error {
		if ctx == nil || ctx.Err() != nil || identity.OriginRunID != origin.ID {
			return ErrIdentity
		}
		info, e := archives.Info(grok.CheckpointRef{ID: identity.CheckpointID, Digest: identity.CheckpointDigest})
		if e != nil || ctx.Err() != nil || info.RunID != origin.ID || info.TaskID != origin.TaskID ||
			info.Generation != origin.Generation || info.PlanRevision != origin.PlanRevision || info.Role != origin.Role ||
			info.NativeSessionID != origin.NativeSessionID || info.Workspace != spec.Workspace || !equal(info.Target, target) {
			return ErrIdentity
		}
		return nil
	}
	b.Restore = func(ctx context.Context, run store.StageRun, spec managed.Spec, identity store.RestoreIdentity) (Execution, error) {
		h, e := a.ResumeCheckpoint(ctx, run, spec, archives, grok.CheckpointRef{ID: identity.CheckpointID, Digest: identity.CheckpointDigest})
		if h == nil {
			return nil, e
		}
		return h, e
	}
	return b
}

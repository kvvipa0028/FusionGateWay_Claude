package control

import (
	"context"
	"path/filepath"

	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/runtime/codexadapter"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

// BindCodexCheckpoint wires trusted private archives and the owning Adapter.
// The reference never supplies executable, source permissions or a stop proof.
func BindCodexCheckpoint(a *codexadapter.Adapter, archives *codexadapter.Archives) Backend {
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
		// The manifest seals the resolved binding cwd (a /var workspace is
		// /private/var to the kernel), so compare against the same spelling.
		resolved := spec.Workspace
		if p, e := filepath.EvalSymlinks(spec.Workspace); e == nil {
			resolved = p
		}
		info, e := archives.Info(codexadapter.CheckpointRef{ID: identity.CheckpointID, Digest: identity.CheckpointDigest})
		if e != nil || ctx.Err() != nil || info.RunID != origin.ID || info.TaskID != origin.TaskID ||
			info.Generation != origin.Generation || info.PlanRevision != origin.PlanRevision || info.Role != origin.Role ||
			info.NativeSessionID != origin.NativeSessionID || info.Workspace != resolved || !equal(info.Target, target) {
			return ErrIdentity
		}
		return nil
	}
	b.Restore = func(ctx context.Context, run store.StageRun, spec managed.Spec, identity store.RestoreIdentity) (Execution, error) {
		h, e := a.ResumeCheckpoint(ctx, run, spec, archives, codexadapter.CheckpointRef{ID: identity.CheckpointID, Digest: identity.CheckpointDigest})
		if h == nil {
			return nil, e
		}
		return h, e
	}
	return b
}

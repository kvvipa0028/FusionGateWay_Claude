package control

import (
	"context"
	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

// Restore is explicit checkpoint execution, not Continue or an old Start retry.
// Its caller must authenticate task/project authority before invoking it.
func (c *Controller) Restore(ctx context.Context, key string, in store.StartIdentity) (store.StartReceipt, error) {
	if in.Restore == nil {
		return store.StartReceipt{}, ErrUnsupported
	}
	return c.start(ctx, key, in, nil)
}
func (c *Controller) RestoreAuthorized(ctx context.Context, key string, in store.StartIdentity, current func(context.Context) bool) (store.StartReceipt, error) {
	if current == nil {
		return store.StartReceipt{}, ErrForbidden
	}
	if in.Restore == nil {
		return store.StartReceipt{}, ErrUnsupported
	}
	return c.start(ctx, key, in, current)
}
func validRestoreLaunch(l Launch, role stageplan.Role) bool {
	if l.Backend.CheckRestore == nil || l.Backend.Restore == nil || l.Spec.Writable {
		return false
	}
	// Reuse the existing argv/channel/path/input/probe/release boundary without
	// treating ordinary Start as authority to perform a checkpoint restore.
	l.Backend.Start = func(context.Context, store.StageRun, managed.Spec) (Execution, error) { return nil, ErrUnsupported }
	return validLaunch(l, role)
}

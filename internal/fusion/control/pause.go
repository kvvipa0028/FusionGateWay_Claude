package control

import (
	"context"

	"github.com/yetone/magpie/internal/fusion/store"
)

func (c *Controller) Pause(ctx context.Context, id string, expected store.TaskVersion) (store.TaskControlReceipt, error) {
	return c.taskControl(ctx, id, expected, true, nil)
}
func (c *Controller) Continue(ctx context.Context, id string, expected store.TaskVersion) (store.TaskControlReceipt, error) {
	return c.taskControl(ctx, id, expected, false, nil)
}
func (c *Controller) PauseAuthorized(ctx context.Context, id string, expected store.TaskVersion, current func(context.Context) bool) (store.TaskControlReceipt, error) {
	if current == nil {
		return store.TaskControlReceipt{}, ErrForbidden
	}
	return c.taskControl(ctx, id, expected, true, current)
}
func (c *Controller) ContinueAuthorized(ctx context.Context, id string, expected store.TaskVersion, current func(context.Context) bool) (store.TaskControlReceipt, error) {
	if current == nil {
		return store.TaskControlReceipt{}, ErrForbidden
	}
	return c.taskControl(ctx, id, expected, false, current)
}

// The durable intent precedes cancellation. Request cancellation cannot undo
// it, cancel another owner's job or replace actual Wait/StopProof processing.
func (c *Controller) taskControl(ctx context.Context, id string, expected store.TaskVersion, pause bool, current func(context.Context) bool) (store.TaskControlReceipt, error) {
	if c == nil || ctx == nil {
		return store.TaskControlReceipt{}, ErrUnsupported
	}
	if ctx.Err() != nil || current != nil && !current(ctx) {
		return store.TaskControlReceipt{}, ErrForbidden
	}
	c.mu.Lock()
	closed := c.closed || c.lifetime.Err() != nil
	c.mu.Unlock()
	if closed {
		return store.TaskControlReceipt{}, ErrClosed
	}
	if _, e := c.config.Scheduler.Store.Task(id); e != nil {
		return store.TaskControlReceipt{}, e
	}
	if ctx.Err() != nil || current != nil && !current(ctx) {
		return store.TaskControlReceipt{}, ErrForbidden
	}
	if !pause {
		return c.config.Scheduler.Store.ContinueTask(id, expected)
	}
	out, e := c.config.Scheduler.Store.PauseTask(id, expected, c.owner)
	if e != nil || out.Run == nil {
		return out, e
	}
	c.mu.Lock()
	j := c.jobs[out.Run.ID]
	c.mu.Unlock()
	if j == nil || j.run.TaskID != id || j.run.Generation != out.Run.Generation {
		return out, ErrReconcile
	}
	select {
	case <-j.done:
		if !j.completion.StoppedVerified || !j.completion.Released {
			return out, ErrReconcile
		}
		return out, nil
	default:
	}
	j.cancel()
	j.mu.Lock()
	h := j.handle
	j.mu.Unlock()
	if h != nil {
		_ = h.Cancel()
	}
	return out, nil
}

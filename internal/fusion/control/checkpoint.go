package control

import (
	"context"
	"errors"
	"github.com/yetone/magpie/internal/fusion/store"
)

type CheckpointRef struct {
	ID     string `json:"id"`
	Digest string `json:"digest"`
}

func validCheckpointRef(ref CheckpointRef) bool {
	for _, value := range []string{ref.ID, ref.Digest} {
		if len(value) != 64 {
			return false
		}
		for _, c := range value {
			if !(c >= 'a' && c <= 'f' || c >= '0' && c <= '9') {
				return false
			}
		}
	}
	return true
}
func (c *Controller) Checkpoint(ctx context.Context, taskID, runID string, expected store.TaskVersion) (CheckpointRef, error) {
	return c.checkpoint(ctx, taskID, runID, expected, nil)
}
func (c *Controller) CheckpointAuthorized(ctx context.Context, taskID, runID string, expected store.TaskVersion, current func(context.Context) bool) (CheckpointRef, error) {
	if current == nil {
		return CheckpointRef{}, ErrForbidden
	}
	return c.checkpoint(ctx, taskID, runID, expected, current)
}
func (c *Controller) checkpoint(ctx context.Context, taskID, runID string, expected store.TaskVersion, current func(context.Context) bool) (CheckpointRef, error) {
	if c == nil || ctx == nil {
		return CheckpointRef{}, ErrUnsupported
	}
	c.mu.Lock()
	closed := c.closed || c.lifetime.Err() != nil
	j := c.jobs[runID]
	c.mu.Unlock()
	if closed {
		return CheckpointRef{}, ErrClosed
	}
	check := func() (store.StageRun, error) {
		c.mu.Lock()
		closing := c.closed || c.lifetime.Err() != nil
		c.mu.Unlock()
		if closing {
			return store.StageRun{}, ErrClosed
		}
		if ctx.Err() != nil {
			return store.StageRun{}, ctx.Err()
		}
		if current != nil && !current(ctx) {
			return store.StageRun{}, ErrForbidden
		}
		if j != nil && j.source.Present() && !j.source.ValidFor(j.workspace) {
			return store.StageRun{}, ErrReconcile
		}
		task, e := c.config.Scheduler.Store.Task(taskID)
		if e != nil {
			return store.StageRun{}, e
		}
		if task.PlanRevision != expected.PlanRevision || task.Generation != expected.Generation || task.State != expected.State || task.State != "ready" {
			return store.StageRun{}, store.ErrConflict
		}
		run, e := c.config.Scheduler.Store.Run(runID)
		if e != nil || run.TaskID != taskID {
			return store.StageRun{}, ErrIdentity
		}
		if run.Generation != task.Generation {
			return store.StageRun{}, store.ErrConflict
		}
		return run, nil
	}
	run, e := check()
	if e != nil {
		return CheckpointRef{}, e
	}
	if j == nil || j.run.TaskID != taskID || j.run.Generation != run.Generation {
		return CheckpointRef{}, ErrReconcile
	}
	select {
	case <-j.done:
	default:
		return CheckpointRef{}, ErrReconcile
	}
	if j.completion.State != "succeeded" || !j.completion.StoppedVerified || !j.completion.Released ||
		run.State != "succeeded" || !equal(run, j.stoppedRun) {
		return CheckpointRef{}, ErrReconcile
	}
	if j.checkpoint == nil {
		return CheckpointRef{}, ErrUnsupported
	}
	if _, e = c.config.Scheduler.Store.Reservation(run.ID); !errors.Is(e, store.ErrNotFound) {
		return CheckpointRef{}, ErrReconcile
	}
	if e = c.acquire(); e != nil {
		return CheckpointRef{}, e
	}
	defer func() {
		c.mu.Lock()
		c.pending--
		c.mu.Unlock()
		<-c.slots
		c.wg.Done()
	}()
	operation, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(c.lifetime, cancel)
	defer stop()
	defer cancel()
	ref, e := j.checkpoint(operation, clone(run))
	if e != nil || !validCheckpointRef(ref) {
		return CheckpointRef{}, ErrReconcile
	}
	if operation.Err() != nil {
		return CheckpointRef{}, operation.Err()
	}
	after, e := check()
	if e != nil {
		return CheckpointRef{}, e
	}
	if !equal(after, run) {
		return CheckpointRef{}, ErrReconcile
	}
	return ref, nil
}

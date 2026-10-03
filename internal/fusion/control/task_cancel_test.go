package control

import (
	"context"
	"errors"
	"github.com/yetone/magpie/internal/fusion/policy"
	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/store"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestControllerCancelTaskIdleNeverInvokesRuntime(t *testing.T) {
	for _, paused := range []bool{false, true} {
		t.Run(map[bool]string{false: "ready", true: "paused"}[paused], func(t *testing.T) {
			f := newControlFixture(t)
			c := f.controller(t)
			task, e := f.st.Task(f.in.TaskID)
			if e != nil {
				t.Fatal(e)
			}
			if paused {
				x, e := c.Pause(context.Background(), task.ID, controlVersion(task))
				if e != nil {
					t.Fatal(e)
				}
				task = x.Task
			}
			x, e := c.CancelTask(context.Background(), task.ID, controlVersion(task))
			if e != nil || !x.Changed || x.Task.State != "cancelled" || x.Run != nil || x.Task.Generation != task.Generation+1 {
				t.Fatal("idle cancel", e)
			}
			repeat, e := c.CancelTask(context.Background(), task.ID, controlVersion(x.Task))
			if e != nil || repeat.Changed || repeat.Task != x.Task {
				t.Fatal("repeat cancel", e)
			}
			if f.started.Load() != 0 || f.resolved.Load() != 0 || f.released.Load() != 0 {
				t.Fatal("idle cancel invoked Runtime")
			}
		})
	}
}

func TestControllerCancelTaskOwnedRunStopsAndClosesWithoutReplay(t *testing.T) {
	f := newControlFixture(t)
	c := f.controller(t)
	first, e := c.Start(context.Background(), "fixture-pause-start", f.in)
	if e != nil {
		t.Fatal(e)
	}
	task, e := f.st.Task(f.in.TaskID)
	if e != nil {
		t.Fatal(e)
	}
	request, disconnect := context.WithCancel(context.Background())
	paused, e := c.CancelTask(request, task.ID, controlVersion(task))
	if e != nil || !paused.Changed || paused.Task.State != "cancelling" {
		t.Fatal("pause owned run", e)
	}
	disconnect()
	done := waitControl(t, c, first.Run.ID)
	if done.State != "cancelled" || !done.StoppedVerified || !done.Released {
		t.Fatal("owned stop did not close", done)
	}
	task, e = f.st.Task(task.ID)
	if e != nil || task.State != "cancelled" {
		t.Fatal("cancelled effect replayable", e)
	}
	if _, e = c.Continue(context.Background(), task.ID, controlVersion(task)); !errors.Is(e, store.ErrConflict) {
		t.Fatal("blind continue", e)
	}
	retry, e := c.Start(context.Background(), "fixture-pause-start", f.in)
	if e != nil || retry.Created || retry.Run.ID != first.Run.ID {
		t.Fatal("old start replay", e)
	}
	if f.started.Load() != 1 || f.released.Load() != 1 {
		t.Fatal("pause replayed/released twice")
	}
}
func TestControllerCancelTaskUnverifiedOrFailedReleaseKeepsHeld(t *testing.T) {
	for _, mode := range []string{"no_stop", "release_error"} {
		t.Run(mode, func(t *testing.T) {
			f := newControlFixture(t)
			f.mode = mode
			c := f.controller(t)
			run, e := c.Start(context.Background(), "fixture-pause-held", f.in)
			if e != nil {
				t.Fatal(e)
			}
			task, e := f.st.Task(f.in.TaskID)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = c.CancelTask(context.Background(), task.ID, controlVersion(task)); e != nil {
				t.Fatal(e)
			}
			done := waitControl(t, c, run.Run.ID)
			if done.Released {
				t.Fatal("false release")
			}
			task, e = f.st.Task(task.ID)
			if e != nil || task.State != "cancelling" {
				t.Fatal("false pause proof", e)
			}
			receipt, e := c.CancelTask(context.Background(), task.ID, controlVersion(task))
			if !errors.Is(e, ErrReconcile) || receipt.Task.ID != task.ID || receipt.Changed {
				t.Fatal("uncertain receipt lost", e)
			}
			if _, e = c.Continue(context.Background(), task.ID, controlVersion(task)); !errors.Is(e, store.ErrConflict) {
				t.Fatal("held run resumed", e)
			}
			if _, e = f.st.Reservation(run.Run.ID); e != nil {
				t.Fatal("held freed", e)
			}
		})
	}
}
func TestControllerCancelTaskForeignOwnershipAndStaleVersionCannotCancel(t *testing.T) {
	f := newControlFixture(t)
	c := f.controller(t)
	first, e := c.Start(context.Background(), "fixture-pause-scope", f.in)
	if e != nil {
		t.Fatal(e)
	}
	other := f.controller(t)
	task, e := f.st.Task(f.in.TaskID)
	if e != nil {
		t.Fatal(e)
	}
	before, e := f.st.Events(task.ID, 0)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = other.CancelTask(context.Background(), task.ID, controlVersion(task)); !errors.Is(e, store.ErrFenced) {
		t.Fatal("foreign controller adopted", e)
	}
	stale := controlVersion(task)
	stale.PlanRevision++
	if _, e = c.CancelTask(context.Background(), task.ID, stale); !errors.Is(e, store.ErrConflict) {
		t.Fatal("stale version cancelled", e)
	}
	run, e := f.st.Run(first.Run.ID)
	if e != nil || run.State != "running" {
		t.Fatal("scope failure stopped run", e)
	}
	after, e := f.st.Events(task.ID, 0)
	if e != nil || len(before) != len(after) {
		t.Fatal("scope failure wrote event", e)
	}
}
func TestControllerCancelTaskCancelsHandleReturnedAfterIntent(t *testing.T) {
	f := newControlFixture(t)
	entered, allow := make(chan struct{}), make(chan struct{})
	h := &fixtureExecution{end: make(chan struct{}), done: make(chan struct{})}
	f.launch.Backend.Start = func(_ context.Context, r store.StageRun, _ managed.Spec) (Execution, error) {
		f.started.Add(1)
		if e := f.st.ConfirmStarted(r.ID, r.Generation, r.Owner, "fixture-late-session"); e != nil {
			return nil, e
		}
		go func() {
			<-h.end
			if e := f.st.Finish(r.ID, r.Generation, r.Owner, "cancelled"); e != nil {
				t.Error(e)
			}
			h.result = managed.Result{State: "cancelled", StoppedVerified: true, Proof: policy.StopProof{RunID: r.ID, Generation: r.Generation, NativeSessionID: "fixture-late-session", ProcessIdentityHash: strings.Repeat("b", 64), ReportHash: strings.Repeat("c", 64), DescendantsStopped: true}}
			close(h.done)
		}()
		close(entered)
		<-allow
		return h, nil
	}
	c := f.controller(t)
	// Ensure the synthetic job can be reaped even when the RED assertion fails.
	t.Cleanup(func() { _ = h.Cancel() })
	type result struct {
		receipt store.StartReceipt
		e       error
	}
	started := make(chan result, 1)
	go func() { r, e := c.Start(context.Background(), "fixture-late-start", f.in); started <- result{r, e} }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("late start never entered")
	}
	task, e := f.st.Task(f.in.TaskID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.CancelTask(context.Background(), task.ID, controlVersion(task)); e != nil {
		t.Fatal(e)
	}
	close(allow)
	first := <-started
	if first.e != nil {
		t.Fatal(first.e)
	}
	select {
	case <-h.end:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("known handle returned after pause was not cancelled")
	}
	done := waitControl(t, c, first.receipt.Run.ID)
	if !done.StoppedVerified || !done.Released {
		t.Fatal("late handle lost", done)
	}
}

func TestControllerCancelTaskRechecksManagementBeforeMutation(t *testing.T) {
	f := newControlFixture(t)
	c := f.controller(t)
	task, e := f.st.Task(f.in.TaskID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.CancelTaskAuthorized(context.Background(), task.ID, controlVersion(task), nil); !errors.Is(e, ErrForbidden) {
		t.Fatal("nil authority", e)
	}
	var checks atomic.Int64
	current := func(context.Context) bool { return checks.Add(1) == 1 }
	if _, e = c.CancelTaskAuthorized(context.Background(), task.ID, controlVersion(task), current); !errors.Is(e, ErrForbidden) {
		t.Fatal("revoked authority", e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = c.CancelTask(ctx, task.ID, controlVersion(task)); !errors.Is(e, ErrForbidden) {
		t.Fatal("cancelled request", e)
	}
	got, e := f.st.Task(task.ID)
	if e != nil || got != task {
		t.Fatal("unauthorized mutation", e)
	}
}

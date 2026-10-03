package control

import (
	"context"
	"errors"
	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func restoreControlFixture(t *testing.T) (*controlFixture, *Controller, store.StartIdentity, *atomic.Int64) {
	t.Helper()
	f := newControlFixture(t)
	c := f.controller(t)
	old, e := c.Start(context.Background(), "fixture-origin-start", f.in)
	if e != nil {
		t.Fatal(e)
	}
	close(f.finish)
	if done := waitControl(t, c, old.Run.ID); !done.Released {
		t.Fatal("origin not released")
	}
	task, e := f.st.Task(f.in.TaskID)
	if e != nil {
		t.Fatal(e)
	}
	in := f.in
	in.Generation = task.Generation
	in.Restore = &store.RestoreIdentity{OriginRunID: old.Run.ID, CheckpointID: strings.Repeat("b", 64), CheckpointDigest: strings.Repeat("c", 64)}
	checks := &atomic.Int64{}
	start := f.launch.Backend.Start
	f.launch.Backend.Restore = func(ctx context.Context, r store.StageRun, spec managed.Spec, ref store.RestoreIdentity) (Execution, error) {
		if ref != *in.Restore {
			t.Error("restore reference changed")
		}
		return start(ctx, r, spec)
	}
	f.launch.Backend.CheckRestore = func(ctx context.Context, r store.StageRun, target stageplan.ExecutionTarget, spec managed.Spec, ref store.RestoreIdentity) error {
		checks.Add(1)
		if r.ID != old.Run.ID || ref != *in.Restore || !equal(r.Target, target) {
			return ErrIdentity
		}
		return nil
	}
	return f, c, in, checks
}
func TestControllerRestoreOnceDoesNotBecomeStartOrReplay(t *testing.T) {
	f, c, in, checks := restoreControlFixture(t)
	if _, e := c.Start(context.Background(), "fixture-wrong-start", in); !errors.Is(e, ErrUnsupported) {
		t.Fatal("start accepted restore body", e)
	}
	first, e := c.Restore(context.Background(), "fixture-resume", in)
	if e != nil || !first.Created {
		t.Fatal(e)
	}
	if done := waitControl(t, c, first.Run.ID); !done.Released {
		t.Fatal("restore not released")
	}
	if first.Run.ID == in.Restore.OriginRunID || first.Run.Generation != 2 || first.Run.Attempt != 2 {
		t.Fatal("origin run reused")
	}
	var wg sync.WaitGroup
	for n := 0; n < 10; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, e := c.Restore(context.Background(), "fixture-resume", in)
			if e != nil || got.Created || got.Run.ID != first.Run.ID {
				t.Error("restore replay", e)
			}
		}()
	}
	wg.Wait()
	if checks.Load() != 1 || f.started.Load() != 2 || f.released.Load() != 2 {
		t.Fatal("duplicate restore or reinspection")
	}
	bad := in
	ref := *in.Restore
	bad.Restore = &ref
	ref.CheckpointDigest = strings.Repeat("e", 64)
	if _, e := c.Restore(context.Background(), "fixture-resume", bad); !errors.Is(e, store.ErrConflict) {
		t.Fatal("changed checkpoint replayed", e)
	}
	plain := in
	plain.Restore = nil
	if _, e := c.Start(context.Background(), "fixture-resume", plain); !errors.Is(e, store.ErrConflict) {
		t.Fatal("restore mapping became ordinary start", e)
	}
}
func TestControllerRestorePreflightAndAuthorityLeaveNoIntent(t *testing.T) {
	for _, mode := range []string{"nil_ref", "unbound", "bad_ref", "foreign_origin", "role", "writable", "revoke"} {
		t.Run(mode, func(t *testing.T) {
			f, c, in, _ := restoreControlFixture(t)
			task, _ := f.st.Task(in.TaskID)
			events, _ := f.st.Events(in.TaskID, 0)
			authorized := true
			switch mode {
			case "nil_ref":
				in.Restore = nil
			case "unbound":
				f.launch.Backend.Restore = nil
			case "bad_ref":
				f.launch.Backend.CheckRestore = func(context.Context, store.StageRun, stageplan.ExecutionTarget, managed.Spec, store.RestoreIdentity) error {
					return ErrIdentity
				}
			case "foreign_origin":
				in.Restore.OriginRunID = "fixture-unrelated"
			case "role":
				in.Role = stageplan.Testing
			case "writable":
				f.launch.Spec.Writable = true
			case "revoke":
				base := f.launch.Backend.CheckRestore
				f.launch.Backend.CheckRestore = func(ctx context.Context, r store.StageRun, tar stageplan.ExecutionTarget, spec managed.Spec, ref store.RestoreIdentity) error {
					e := base(ctx, r, tar, spec, ref)
					authorized = false
					return e
				}
			}
			got, e := c.RestoreAuthorized(context.Background(), "fixture-refuse", in, func(context.Context) bool { return authorized })
			if e == nil || got.Created || f.started.Load() != 1 {
				t.Fatal("restore preflight did not refuse")
			}
			after, _ := f.st.Task(in.TaskID)
			afterEvents, _ := f.st.Events(in.TaskID, 0)
			if task.Generation != after.Generation || task.State != after.State || len(events) != len(afterEvents) {
				t.Fatal("refusal wrote intent")
			}
		})
	}
}
func TestControllerRestoreUnknownIntentIsReadOnlyAfterRestart(t *testing.T) {
	f, c, in, _ := restoreControlFixture(t)
	f.mode = "no_handle"
	first, e := c.Restore(context.Background(), "fixture-uncertain-restore", in)
	if !errors.Is(e, ErrLaunch) || !first.Created {
		t.Fatal("uncertain restore lost receipt", e)
	}
	done := waitControl(t, c, first.Run.ID)
	if done.Released || done.StoppedVerified {
		t.Fatal("unknown launch freed capacity")
	}
	// Reopen the actual Store. Interrupted/held does not become runnable.
	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if e = c.Close(stopCtx); e != nil {
		t.Fatal(e)
	}
	if e = f.st.Close(); e != nil {
		t.Fatal(e)
	}
	reopened, e := store.Open(f.root)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { reopened.Close() })
	f.st = reopened
	f.config.Scheduler.Store = reopened
	other := f.controller(t)
	got, e := other.Restore(context.Background(), "fixture-uncertain-restore", in)
	if e != nil || got.Created || got.Run.ID != first.Run.ID || f.started.Load() != 2 {
		t.Fatal("restart repeated restore", e)
	}
	if _, e = f.st.Reservation(first.Run.ID); e != nil {
		t.Fatal("unknown reservation lost")
	}
	if r, e := other.Cancel(in.TaskID, first.Run.ID, first.Run.Generation); e != nil || r.State != "interrupted" {
		t.Fatal("terminal retry changed state", e)
	}
	if _, e = f.st.Reservation(first.Run.ID); e != nil {
		t.Fatal("terminal cancel read released uncertain capacity")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if e = other.Close(ctx); e != nil {
		t.Fatal(e)
	}
}

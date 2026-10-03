package control

import (
	"context"
	"errors"
	"github.com/yetone/magpie/internal/fusion/store"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestControllerCheckpointRejectsClosingBeforeCancellationCallback(t *testing.T) {
	f := newControlFixture(t)
	var c *Controller
	f.launch.Backend.Checkpoint = func(context.Context, store.StageRun) (CheckpointRef, error) {
		// Model the interval after Close sets its flag and before the asynchronous
		// lifetime cancellation callback reaches this producer's context.
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		return CheckpointRef{ID: strings.Repeat("b", 64), Digest: strings.Repeat("c", 64)}, nil
	}
	c = f.controller(t)
	defer func() { c.mu.Lock(); c.closed = false; c.mu.Unlock() }()
	first, e := c.Start(context.Background(), "fixture-start", f.in)
	if e != nil {
		t.Fatal(e)
	}
	close(f.finish)
	waitControl(t, c, first.Run.ID)
	task, _ := f.st.Task(f.in.TaskID)
	ref, e := c.Checkpoint(context.Background(), task.ID, first.Run.ID, controlVersion(task))
	if !errors.Is(e, ErrClosed) || ref != (CheckpointRef{}) {
		t.Fatal("closing controller published checkpoint")
	}
}

func TestControllerCheckpointUsesOriginalOwnedSuccessfulBackend(t *testing.T) {
	f := newControlFixture(t)
	calls := &atomic.Int64{}
	expected := CheckpointRef{ID: strings.Repeat("b", 64), Digest: strings.Repeat("c", 64)}
	f.launch.Backend.Checkpoint = func(ctx context.Context, r store.StageRun) (CheckpointRef, error) {
		calls.Add(1)
		if r.State != "succeeded" || r.Owner != "" || r.TaskID != f.in.TaskID {
			t.Error("checkpoint got unverified run")
		}
		return expected, nil
	}
	c := f.controller(t)
	first, e := c.Start(context.Background(), "fixture-start", f.in)
	if e != nil {
		t.Fatal(e)
	}
	task, _ := f.st.Task(f.in.TaskID)
	if _, e = c.Checkpoint(context.Background(), task.ID, first.Run.ID, controlVersion(task)); e == nil || calls.Load() != 0 {
		t.Fatal("running checkpoint accepted")
	}
	close(f.finish)
	if !waitControl(t, c, first.Run.ID).Released {
		t.Fatal("origin not released")
	}
	task, _ = f.st.Task(f.in.TaskID)
	// A later resolver change must not replace the producer belonging to this job.
	f.launch.Backend.Checkpoint = func(context.Context, store.StageRun) (CheckpointRef, error) {
		t.Error("new backend captured old run")
		return CheckpointRef{}, ErrUnsupported
	}
	for n := 0; n < 2; n++ {
		got, e := c.Checkpoint(context.Background(), task.ID, first.Run.ID, controlVersion(task))
		if e != nil || got != expected {
			t.Fatal("owned checkpoint absent", e)
		}
	}
	if calls.Load() != 2 || f.started.Load() != 1 {
		t.Fatal("checkpoint launched Native")
	}
	other := f.controller(t)
	if _, e = other.Checkpoint(context.Background(), task.ID, first.Run.ID, controlVersion(task)); !errors.Is(e, ErrReconcile) {
		t.Fatal("unowned controller captured", e)
	}
}
func TestControllerCheckpointRejectsFailureScopeAndRevocation(t *testing.T) {
	for _, mode := range []string{"no_stop", "release_error", "no_handle", "unbound", "wrong_task", "stale", "revoked_before", "revoked_during", "malformed", "backend_error"} {
		t.Run(mode, func(t *testing.T) {
			f := newControlFixture(t)
			if mode == "no_stop" || mode == "release_error" || mode == "no_handle" {
				f.mode = mode
			}
			allowed := true
			var called atomic.Int64
			f.launch.Backend.Checkpoint = func(context.Context, store.StageRun) (CheckpointRef, error) {
				called.Add(1)
				if mode == "revoked_during" {
					allowed = false
				}
				if mode == "backend_error" {
					return CheckpointRef{}, errors.New("fixture-sensitive")
				}
				if mode == "malformed" {
					return CheckpointRef{ID: "bad", Digest: "bad"}, nil
				}
				return CheckpointRef{ID: strings.Repeat("b", 64), Digest: strings.Repeat("c", 64)}, nil
			}
			if mode == "unbound" {
				f.launch.Backend.Checkpoint = nil
			}
			c := f.controller(t)
			first, _ := c.Start(context.Background(), "fixture-start", f.in)
			close(f.finish)
			waitControl(t, c, first.Run.ID)
			task, _ := f.st.Task(f.in.TaskID)
			before, _ := f.st.Budget(task.ID)
			events, _ := f.st.Events(task.ID, 0)
			id := task.ID
			version := controlVersion(task)
			if mode == "wrong_task" {
				id = "fixture-other-task"
			}
			if mode == "stale" {
				version.Generation++
			}
			if mode == "revoked_before" {
				allowed = false
			}
			ref, e := c.CheckpointAuthorized(context.Background(), id, first.Run.ID, version, func(context.Context) bool { return allowed })
			if e == nil || ref != (CheckpointRef{}) {
				t.Fatal("invalid checkpoint published")
			}
			after, _ := f.st.Budget(task.ID)
			ev, _ := f.st.Events(task.ID, 0)
			if before != after || len(ev) != len(events) {
				t.Fatal("checkpoint changed budget/events")
			}
			if mode != "revoked_during" && mode != "malformed" && mode != "backend_error" && called.Load() != 0 {
				t.Fatal("refusal invoked backend")
			}
			if mode == "no_stop" || mode == "release_error" || mode == "no_handle" {
				if _, e = f.st.Reservation(first.Run.ID); e != nil {
					t.Fatal("failed checkpoint released uncertain capacity")
				}
			}
		})
	}
}

func TestControllerCheckpointCloseWaitsAndSuppressesProducerOutput(t *testing.T) {
	for _, cooperate := range []bool{true, false} {
		t.Run(map[bool]string{true: "cancel", false: "wait"}[cooperate], func(t *testing.T) {
			f := newControlFixture(t)
			entered, release := make(chan struct{}), make(chan struct{})
			f.launch.Backend.Checkpoint = func(ctx context.Context, r store.StageRun) (CheckpointRef, error) {
				close(entered)
				if cooperate {
					<-ctx.Done()
				} else {
					<-release
				}
				return CheckpointRef{ID: strings.Repeat("b", 64), Digest: strings.Repeat("c", 64)}, nil
			}
			c := f.controller(t)
			first, e := c.Start(context.Background(), "fixture-start", f.in)
			if e != nil {
				t.Fatal(e)
			}
			close(f.finish)
			waitControl(t, c, first.Run.ID)
			task, _ := f.st.Task(f.in.TaskID)
			result := make(chan error, 1)
			go func() {
				ref, e := c.Checkpoint(context.Background(), task.ID, first.Run.ID, controlVersion(task))
				if ref != (CheckpointRef{}) {
					t.Error("closed operation exposed checkpoint")
				}
				result <- e
			}()
			<-entered
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			e = c.Close(ctx)
			if !cooperate {
				if !errors.Is(e, context.DeadlineExceeded) {
					t.Fatal("Close did not wait for producer", e)
				}
				close(release)
			} else if e != nil {
				t.Fatal(e)
			}
			if e = <-result; e == nil {
				t.Fatal("closed producer succeeded")
			}
			finish, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			if e = c.Close(finish); e != nil {
				t.Fatal(e)
			}
		})
	}
}

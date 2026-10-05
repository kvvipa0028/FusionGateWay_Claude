package control

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/store"
)

func TestControllerFollowupReleasesOldSlotAtCapacityOneAndCloseWaits(t *testing.T) {
	f := newControlFixture(t)
	if e := f.st.ConfigureCapacity(1); e != nil {
		t.Fatal(e)
	}
	close(f.finish)
	second := make(chan struct{})
	f.config.AfterRelease = func(ctx context.Context, r store.StageRun) (*Followup, error) {
		if r.Attempt == 2 {
			close(second)
			return nil, nil
		}
		return &Followup{Key: "owned-second", Identity: store.StartIdentity{TaskID: r.TaskID, Role: r.Role, PlanRevision: r.PlanRevision, Generation: r.Generation}, Current: func(c context.Context) bool { return c.Err() == nil && ctx.Err() == nil }}, nil
	}
	c := f.controller(t)
	if _, e := c.Start(context.Background(), "owned-first", f.in); e != nil {
		t.Fatal(e)
	}
	select {
	case <-second:
	case <-time.After(2 * time.Second):
		t.Fatal("followup kept old capacity slot or failed to start")
	}
	if e := c.Close(context.Background()); e != nil {
		t.Fatal(e)
	}
	if f.started.Load() != 2 || f.released.Load() != 2 {
		t.Fatal("Close returned before exact followup/release")
	}
}

func TestControllerCloseCancelsOwnedFollowupHookWithoutDetachedStart(t *testing.T) {
	f := newControlFixture(t)
	close(f.finish)
	entered := make(chan struct{})
	f.config.AfterRelease = func(ctx context.Context, r store.StageRun) (*Followup, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	c := f.controller(t)
	if _, e := c.Start(context.Background(), "owned-first", f.in); e != nil {
		t.Fatal(e)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("release hook not entered")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if e := c.Close(ctx); e != nil {
		t.Fatal("Close did not wait/cancel owned hook", e)
	}
	if f.started.Load() != 1 || f.released.Load() != 1 {
		t.Fatal("Close permitted a detached launch")
	}
}

func TestControllerUnverifiedReleaseCannotInvokeFollowup(t *testing.T) {
	for _, mode := range []string{"no_stop", "release_error"} {
		t.Run(mode, func(t *testing.T) {
			f := newControlFixture(t)
			f.mode = mode
			close(f.finish)
			f.config.AfterRelease = func(context.Context, store.StageRun) (*Followup, error) {
				t.Error("unverified release reached followup")
				return nil, errors.New("not permitted")
			}
			c := f.controller(t)
			r, e := c.Start(context.Background(), "owned-first", f.in)
			if e != nil {
				t.Fatal(e)
			}
			if done := waitControl(t, c, r.Run.ID); done.Released {
				t.Fatal("unverified stop released")
			}
		})
	}
}

package control

import (
	"context"
	"errors"
	"testing"

	"github.com/yetone/magpie/internal/fusion/store"
)

func TestControllerStageAttemptLimitStopsBeforeResolverAndRuntimeAndAllowsReplay(t *testing.T) {
	f := newControlFixture(t)
	close(f.finish)
	var last store.StartReceipt
	var lastIdentity store.StartIdentity
	for _, key := range []string{"attempt-first", "attempt-remedial"} {
		c := f.controller(t)
		lastIdentity = f.in
		var e error
		last, e = c.Start(context.Background(), key, f.in)
		if e != nil || !last.Created {
			t.Fatal(e)
		}
		if got := waitControl(t, c, last.Run.ID); !got.StoppedVerified || !got.Released {
			t.Fatal("prior attempt not released")
		}
		if e = c.Close(context.Background()); e != nil {
			t.Fatal(e)
		}
		f.in.Generation = last.Run.Generation
	}
	c := f.controller(t)
	if _, e := c.Start(context.Background(), "attempt-third", f.in); !errors.Is(e, store.ErrStageLimit) {
		t.Fatal("third Runtime attempt admitted", e)
	}
	if f.resolved.Load() != 2 || f.started.Load() != 2 || f.released.Load() != 2 {
		t.Fatal("cap prepared Runtime or changed prior release")
	}
	task, e := f.st.Task(f.in.TaskID)
	if e != nil || task.State != "needs_review" || task.Generation != 3 {
		t.Fatal("controller cap not durable", e)
	}
	got, e := c.Start(context.Background(), "attempt-remedial", lastIdentity)
	if e != nil || got.Created || got.Run.ID != last.Run.ID || f.started.Load() != 2 {
		t.Fatal("receipt replay relaunched Runtime", e)
	}
}

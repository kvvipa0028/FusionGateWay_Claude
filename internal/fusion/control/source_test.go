package control

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/fusion/policy"
	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
	"github.com/yetone/magpie/internal/fusion/workspace"
)

func controlSourceFixture(t *testing.T, f *controlFixture) string {
	t.Helper()
	source := fixturePrivate(t)
	p := filepath.Join(source, "source.txt")
	if e := os.WriteFile(p, []byte("original"), 0600); e != nil {
		t.Fatal(e)
	}
	snapshot, e := workspace.Copy(source, fixturePrivate(t), "copy")
	if e != nil {
		t.Fatal(e)
	}
	guard, e := snapshot.Guard()
	if e != nil {
		t.Fatal(e)
	}
	f.launch.Spec.Workspace = snapshot.Path
	f.launch.Spec.Source = guard
	f.config.RequireSource = true
	return p
}

func TestControllerRequiresSourceBeforeNewIntent(t *testing.T) {
	for _, kind := range []string{"absent", "changed", "wrong_cwd"} {
		t.Run(kind, func(t *testing.T) {
			f := newControlFixture(t)
			if kind == "absent" {
				f.config.RequireSource = true
			} else {
				p := controlSourceFixture(t, f)
				if kind == "changed" {
					if e := os.WriteFile(p, []byte("changed"), 0600); e != nil {
						t.Fatal(e)
					}
				} else {
					f.launch.Spec.Workspace = fixturePrivate(t)
				}
			}
			c := f.controller(t)
			if receipt, e := c.Start(context.Background(), "source-start", f.in); e == nil || receipt.Created || f.started.Load() != 0 {
				t.Fatal("unapproved source launched", e)
			}
			task, e := f.st.Task(f.in.TaskID)
			if e != nil || task.State != "ready" || task.Generation != 0 {
				t.Fatal("source rejection wrote intent", e)
			}
		})
	}
}

func TestControllerRechecksSourceAfterSlowPreflight(t *testing.T) {
	for _, kind := range []string{"probe", "inspection"} {
		t.Run(kind, func(t *testing.T) {
			f := newControlFixture(t)
			p := controlSourceFixture(t, f)
			change := func() {
				if e := os.WriteFile(p, []byte("changed"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			if kind == "probe" {
				old := f.launch.Backend.Probe
				f.launch.Backend.Probe = func(ctx context.Context) (managed.Capabilities, error) { change(); return old(ctx) }
			} else {
				old := f.config.Scheduler.Inspect
				f.config.Scheduler.Inspect = func(ctx context.Context, task store.Task, role stageplan.Role, target stageplan.ExecutionTarget) (policy.Inspection, error) {
					out, e := old(ctx, task, role, target)
					change()
					return out, e
				}
			}
			c := f.controller(t)
			if receipt, e := c.Start(context.Background(), "source-start", f.in); e == nil || receipt.Created || f.started.Load() != 0 {
				t.Fatal("preflight source drift launched", e)
			}
			task, e := f.st.Task(f.in.TaskID)
			if e != nil || task.State != "ready" || task.Generation != 0 {
				t.Fatal("source rejection wrote intent", e)
			}
		})
	}
}

func TestControllerPreparedSourceDriftDoesNotInventStopProof(t *testing.T) {
	f := newControlFixture(t)
	p := controlSourceFixture(t, f)
	old := f.config.Scheduler.Inspect
	calls := 0
	f.config.Scheduler.Inspect = func(ctx context.Context, task store.Task, role stageplan.Role, target stageplan.ExecutionTarget) (policy.Inspection, error) {
		out, e := old(ctx, task, role, target)
		calls++
		if calls == 2 {
			if e := os.WriteFile(p, []byte("changed"), 0600); e != nil {
				t.Fatal(e)
			}
		}
		return out, e
	}
	c := f.controller(t)
	receipt, e := c.Start(context.Background(), "source-start", f.in)
	if e == nil || !receipt.Created || f.started.Load() != 0 {
		t.Fatal("prepared source drift launched", e)
	}
	done := waitControl(t, c, receipt.Run.ID)
	if done.State != "execution_uncertain" || done.StoppedVerified || done.Released || f.released.Load() != 0 {
		t.Fatal("invented source stop proof", done)
	}
	run, e := f.st.Run(receipt.Run.ID)
	if e != nil || run.State != "interrupted" {
		t.Fatal("source launch not fenced", e)
	}
	if _, e = f.st.Reservation(receipt.Run.ID); e != nil {
		t.Fatal("unproved source reservation released", e)
	}
	budget, e := f.st.Budget(f.in.TaskID)
	if e != nil || budget.UsedCalls != 0 {
		t.Fatal("source refusal spent model budget", e)
	}
}

func TestControllerCheckpointRechecksOriginalSourceGuard(t *testing.T) {
	for _, kind := range []string{"before", "during"} {
		t.Run(kind, func(t *testing.T) {
			f := newControlFixture(t)
			p := controlSourceFixture(t, f)
			calls := 0
			change := func() {
				if e := os.WriteFile(p, []byte("changed"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			f.launch.Backend.Checkpoint = func(context.Context, store.StageRun) (CheckpointRef, error) {
				calls++
				if kind == "during" {
					change()
				}
				return CheckpointRef{ID: strings.Repeat("b", 64), Digest: strings.Repeat("c", 64)}, nil
			}
			c := f.controller(t)
			first, e := c.Start(context.Background(), "source-start", f.in)
			if e != nil {
				t.Fatal(e)
			}
			close(f.finish)
			if !waitControl(t, c, first.Run.ID).Released {
				t.Fatal("source origin not stopped/released")
			}
			if kind == "before" {
				change()
			}
			task, e := f.st.Task(f.in.TaskID)
			if e != nil {
				t.Fatal(e)
			}
			if ref, e := c.Checkpoint(context.Background(), task.ID, first.Run.ID, controlVersion(task)); e == nil || ref != (CheckpointRef{}) {
				t.Fatal("changed source reference disclosed", e)
			}
			if kind == "before" && calls != 0 || kind == "during" && calls != 1 {
				t.Fatal("wrong producer count", calls)
			}
		})
	}
}

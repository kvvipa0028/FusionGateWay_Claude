package codexadapter

import (
	"context"
	"errors"
	"testing"
	"time"

	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/runtime/codex"
)

// Checkpoint only ever captures this Adapter's own succeeded and released
// runs; unknown ids, foreign generations and never-launched runs are refused
// without touching disk or budget.
func TestCodexAdapterCheckpointRequiresOwnSucceededRun(t *testing.T) {
	c, r, in, calls, _, _ := adapterFixture(t)
	a, e := NewAdapter(c)
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	if _, e := a.Checkpoint(ctx, "missing-run", 1); !errors.Is(e, codex.ErrIdentity) {
		t.Fatal("unknown run checkpointed", e)
	}
	if calls.Load() != 0 {
		t.Fatal("checkpoint spent calls")
	}
	// A refused launch never leaves an observation to checkpoint.
	failed := in
	failed.Timeout = 5 * time.Second
	if _, e := a.Start(ctx, r, failed); e == nil {
		t.Fatal("phantom executable launched")
	}
	if _, e := a.Checkpoint(ctx, r.ID, r.Generation); !errors.Is(e, codex.ErrIdentity) {
		t.Fatal("failed run checkpointed", e)
	}
	if _, e := a.Checkpoint(ctx, r.ID, r.Generation+1); !errors.Is(e, codex.ErrIdentity) {
		t.Fatal("foreign generation checkpointed", e)
	}
	if calls.Load() != 0 {
		t.Fatal("negative checkpoints spent calls")
	}
}

// A resume launch is refused before any intent when the reference does not
// exactly match this run's frozen target.
func TestCodexAdapterStartResumedValidatesReference(t *testing.T) {
	c, r, in, _, _, _ := adapterFixture(t)
	a, e := NewAdapter(c)
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	valid := CheckpointRef{ThreadID: "12345678-1234-1234-1234-123456789abc", Cwd: "/fixture/workspace", Target: r.Target, RuntimeVersion: codex.CLIVersion, ExecutableSHA256: managed.CodexExecutableSHA256, Files: map[string][]byte{"sessions/2026/10/06/rollout-x.jsonl": []byte("{}")}}
	for _, mode := range []string{"thread", "files", "version", "hash", "target"} {
		drifted := valid
		switch mode {
		case "thread":
			drifted.ThreadID = ""
		case "files":
			drifted.Files = nil
		case "version":
			drifted.RuntimeVersion = "0.0.0"
		case "hash":
			drifted.ExecutableSHA256 = "other"
		case "target":
			drifted.Target.Account = "fixture-other"
		}
		if h, e := a.StartResumed(ctx, r, in, drifted); h != nil || e == nil {
			h.Cancel()
			t.Fatal("drifted reference launched", mode)
		}
	}
	if h, e := a.StartResumed(ctx, r, in, valid); h != nil || e == nil {
		h.Cancel()
		t.Fatal("resume launched without a pinned executable")
	}
}

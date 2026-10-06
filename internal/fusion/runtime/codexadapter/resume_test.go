package codexadapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/runtime/codex"
)

func refDigest(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }

// Checkpoint only ever seals this Adapter's own succeeded and released runs;
// unknown ids, foreign generations, never-launched runs and missing archives
// are refused without touching disk or budget.
func TestCodexAdapterCheckpointRequiresOwnSucceededRun(t *testing.T) {
	c, r, in, calls, _, _ := adapterFixture(t)
	a, e := NewAdapter(c)
	if e != nil {
		t.Fatal(e)
	}
	archives, e := NewArchives(privateDir(t))
	if e != nil {
		t.Fatal(e)
	}
	defer archives.Close()
	ctx := context.Background()
	if _, e := a.Checkpoint(ctx, "missing-run", 1, archives); !errors.Is(e, codex.ErrIdentity) {
		t.Fatal("unknown run checkpointed", e)
	}
	if _, e := a.Checkpoint(ctx, r.ID, r.Generation, nil); e == nil {
		t.Fatal("missing archives accepted")
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
	if _, e := a.Checkpoint(ctx, r.ID, r.Generation, archives); !errors.Is(e, codex.ErrIdentity) {
		t.Fatal("failed run checkpointed", e)
	}
	if _, e := a.Checkpoint(ctx, r.ID, r.Generation+1, archives); !errors.Is(e, codex.ErrIdentity) {
		t.Fatal("foreign generation checkpointed", e)
	}
	if calls.Load() != 0 {
		t.Fatal("negative checkpoints spent calls")
	}
}

// A resume launch is refused before any intent when the sealed reference
// cannot be opened under this controller's archive key.
func TestCodexAdapterResumeCheckpointValidatesSeal(t *testing.T) {
	c, r, in, _, _, _ := adapterFixture(t)
	a, e := NewAdapter(c)
	if e != nil {
		t.Fatal(e)
	}
	archives, e := NewArchives(privateDir(t))
	if e != nil {
		t.Fatal(e)
	}
	defer archives.Close()
	foreign, e := NewArchives(privateDir(t))
	if e != nil {
		t.Fatal(e)
	}
	defer foreign.Close()
	ctx := context.Background()
	valid := CheckpointRef{ID: refDigest("id"), Digest: refDigest("digest")}
	for _, mode := range []string{"nil_archives", "shape", "unknown_id", "foreign_key"} {
		use, ref := archives, valid
		switch mode {
		case "nil_archives":
			use = nil
		case "shape":
			ref = CheckpointRef{ID: "short", Digest: "short"}
		case "unknown_id":
			ref = CheckpointRef{ID: refDigest("other"), Digest: refDigest("other")}
		case "foreign_key":
			use = foreign
		}
		if h, e := a.ResumeCheckpoint(ctx, r, in, use, ref); h != nil || e == nil {
			h.Cancel()
			t.Fatal("unsealed resume launched", mode)
		}
	}
}

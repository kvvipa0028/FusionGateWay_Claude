package grok

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestoreReceiptPersistsExactPreparedMappingWithoutPromptOrGrant(t *testing.T) {
	a, ref, record, next, _, scheduler := restoreFixture(t)
	c, e := a.prepareRestore(context.Background(), ref, scheduler, next, record.cwd)
	if e != nil {
		t.Fatal(e)
	}
	root := privateAdapterDir(t)
	prompt := []byte("PRIVATE_NEXT_PROMPT_MARKER")
	if e = a.recordRestore(context.Background(), c, root, prompt); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(filepath.Join(a.path, restoreReceiptName(next.ID)))
	if e != nil || strings.Contains(string(raw), string(prompt)) {
		t.Fatal("receipt disclosed prompt")
	}
	info, e := a.RestoreInfo(next.ID, next.Generation)
	if e != nil || info.RunID != next.ID || info.OriginRunID != record.run.ID || info.Generation != next.Generation || info.NativeSessionID != record.run.NativeSessionID || info.Checkpoint != ref {
		t.Fatal("receipt mismatch", e)
	}
	if e = a.recordRestore(context.Background(), c, root, prompt); e != nil {
		t.Fatal("same prepared mapping retry failed", e)
	}
	if e = a.recordRestore(context.Background(), c, root, []byte("CHANGED_PROMPT")); e == nil {
		t.Fatal("mapping prompt changed")
	}
	if e = a.recordRestore(context.Background(), c, privateAdapterDir(t), prompt); e == nil {
		t.Fatal("mapping root changed")
	}
	dir := a.path
	a.Close()
	reopened, e := NewArchives(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	again, e := reopened.RestoreInfo(next.ID, next.Generation)
	if e != nil || again != info {
		t.Fatal("persistent mapping lost", e)
	}
	if _, e = reopened.RestoreInfo(next.ID, next.Generation+1); e == nil {
		t.Fatal("foreign generation read")
	}
}

func TestRestoreReceiptRejectsTamperingLinksAndPrivateStateDrift(t *testing.T) {
	for _, mode := range []string{"payload", "mac", "duplicate", "symlink", "hardlink", "mode", "key", "closed", "nonfresh", "overlap", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			a, ref, record, next, _, scheduler := restoreFixture(t)
			c, e := a.prepareRestore(context.Background(), ref, scheduler, next, record.cwd)
			if e != nil {
				t.Fatal(e)
			}
			root := privateAdapterDir(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "nonfresh" || mode == "overlap" || mode == "cancel" {
				switch mode {
				case "nonfresh":
					os.WriteFile(filepath.Join(root, "caller-config"), []byte("data"), 0600)
				case "overlap":
					root = a.path
				case "cancel":
					cancel()
				}
				if e = a.recordRestore(ctx, c, root, []byte("private prompt")); e == nil {
					t.Fatal("unsafe mapping published")
				}
				if _, e = a.RestoreInfo(next.ID, next.Generation); e == nil {
					t.Fatal("failed preparation left receipt")
				}
				return
			}
			if e = a.recordRestore(ctx, c, root, []byte("private prompt")); e != nil {
				t.Fatal(e)
			}
			path := filepath.Join(a.path, restoreReceiptName(next.ID))
			raw, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			switch mode {
			case "payload", "mac":
				var sealed sealedCheckpoint
				if archiveJSON(raw, &sealed) != nil {
					t.Fatal("valid receipt decode failed")
				}
				if mode == "payload" {
					var receipt restoreReceipt
					if archiveJSON(sealed.Payload, &receipt) != nil {
						t.Fatal("valid payload decode failed")
					}
					receipt.Run.Target.Account = "caller-account"
					sealed.Payload, _ = json.Marshal(receipt)
				} else {
					sealed.MAC = strings.Repeat("a", 64)
				}
				raw, _ = json.Marshal(sealed)
				os.WriteFile(path, raw, 0600)
			case "duplicate":
				os.WriteFile(path, []byte(`{"payload":{},"PAYLOAD":{},"mac":"a"}`), 0600)
			case "symlink":
				os.Rename(path, path+".old")
				os.Symlink(path+".old", path)
			case "hardlink":
				os.Link(path, path+".link")
			case "mode":
				os.Chmod(path, 0644)
			case "key":
				os.WriteFile(filepath.Join(a.path, "archive.key"), []byte(strings.Repeat("x", 32)), 0600)
			case "closed":
				a.Close()
			}
			if _, e = a.RestoreInfo(next.ID, next.Generation); e == nil {
				t.Fatal("tampered mapping accepted")
			}
		})
	}
}

func TestRestoreReceiptDoesNotReconcileOrRelaunchUnknownRun(t *testing.T) {
	a, ref, record, next, _, scheduler := restoreFixture(t)
	c, e := a.prepareRestore(context.Background(), ref, scheduler, next, record.cwd)
	if e != nil {
		t.Fatal(e)
	}
	if e = a.recordRestore(context.Background(), c, privateAdapterDir(t), []byte("private prompt")); e != nil {
		t.Fatal(e)
	}
	if e = scheduler.Store.Finish(next.ID, next.Generation, next.Owner, "interrupted"); e != nil {
		t.Fatal(e)
	}
	if _, e = a.prepareRestore(context.Background(), ref, scheduler, next, record.cwd); e == nil {
		t.Fatal("receipt restored interrupted run")
	}
	if _, e = a.RestoreInfo(next.ID, next.Generation); e != nil {
		t.Fatal("receipt must preserve planned mapping", e)
	}
	task, e := scheduler.Store.Task(next.TaskID)
	if e != nil || task.State != "needs_review" {
		t.Fatal("receipt reconciled task state")
	}
	if _, e = scheduler.Store.Reservation(next.ID); e != nil {
		t.Fatal("receipt released unproven capacity")
	}
}

func TestReadHistoryNewGrantReflectionIsRejected(t *testing.T) {
	_, r, _, _ := restoreReadFixture(t)
	if !r.containsPrivate([]byte("ORIGINAL_APPROVED_READ")) || r.containsPrivate([]byte("different-private-grant")) {
		t.Fatal("imported history marker check failed")
	}
}

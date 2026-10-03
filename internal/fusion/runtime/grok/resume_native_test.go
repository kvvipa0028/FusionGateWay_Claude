//go:build darwin

package grok

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/yetone/magpie/internal/fusion/policy"
	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

func TestGrokAdapterPinnedNativeResume(t *testing.T) {
	if *nativeGateCLI == "" {
		t.Skip("explicit pinned Native resume fixture only")
	}
	for _, mode := range []string{"text", "read", "new_read", "unicode_cwd", "reopen", "store_reopen", "cancel_read", "bad_ref", "source_drift", "target_drift", "root_drift", "quota_drift"} {
		t.Run(mode, func(t *testing.T) { nativeResumeGrok(t, mode) })
	}
}

func nativeResumeGrok(t *testing.T, mode string) {
	t.Helper()
	exe, e := filepath.EvalSymlinks(*nativeGateCLI)
	if e != nil {
		t.Fatal(e)
	}
	hash, e := managed.FileHash(exe)
	if e != nil || hash != NativeExecutableSHA256 {
		t.Fatal("Native pin mismatch")
	}
	f, s, inspection, old, pending, scheduler := storedGrokFixture(t, 8, false)
	pending.Cancel()
	root, cwd, nextRoot := privateAdapterDir(t), privateAdapterDir(t), privateAdapterDir(t)
	if mode == "unicode_cwd" {
		cwd = filepath.Join(cwd, "工程 空格")
		if os.Mkdir(cwd, 0700) != nil {
			t.Fatal("unicode cwd fixture failed")
		}
	}
	oldRead := mode == "read" || mode == "new_read" || mode == "cancel_read" || mode == "source_drift" || mode == "quota_drift"
	entered, transportDone := make(chan struct{}, 1), make(chan struct{}, 1)
	var drifted atomic.Bool
	path := filepath.Join(cwd, "approved.txt")
	owned := []byte("ORIGINAL_APPROVED_READ\n")
	if e = os.WriteFile(path, owned, 0600); e != nil {
		t.Fatal(e)
	}
	var resumed, oldMain, resumedMain atomic.Int64
	var historyOK, readHistoryOK atomic.Bool
	forward := fakeForwarder(func(ctx context.Context, target stageplan.ExecutionTarget, raw []byte) (ForwardResponse, error) {
		f.calls.Add(1)
		if !sameTarget(target, old.Target) {
			return ForwardResponse{}, ErrIdentity
		}
		var request struct {
			Tools    []struct{ Function struct{ Name string } }
			Messages []json.RawMessage
		}
		if decode(raw, &request) != nil {
			return ForwardResponse{}, ErrProtocol
		}
		main := false
		for _, tool := range request.Tools {
			if tool.Function.Name == "read_file" {
				main = true
			}
		}
		if main {
			if resumed.Load() == 0 {
				if oldMain.Add(1) == 1 && oldRead {
					return ForwardResponse{StatusCode: 200, ContentType: "text/event-stream", Body: io.NopCloser(strings.NewReader(toolSSE(t, path, "call_old_read")))}, nil
				}
			} else {
				ordinal := resumedMain.Add(1)
				historyOK.Store(bytes.Contains(raw, []byte("FIRST_PROMPT_MARKER")) && bytes.Contains(raw, []byte("RESUME_PROMPT_MARKER")) && bytes.Contains(raw, []byte("Fixture ready.")))
				for _, message := range request.Messages {
					var m struct{ Role, Content, ToolCallID string }
					// Native tool_call_id uses an underscore; inspect the raw pair too.
					if decode(message, &m) == nil && m.Role == "tool" && m.Content == "1→ORIGINAL_APPROVED_READ\n" && bytes.Contains(message, []byte("call_old_read")) {
						readHistoryOK.Store(true)
					}
				}
				if mode == "cancel_read" {
					entered <- struct{}{}
					<-ctx.Done()
					transportDone <- struct{}{}
					return ForwardResponse{}, ctx.Err()
				}
				if mode == "quota_drift" {
					inspection.Quota.Status = "unknown"
					return ForwardResponse{StatusCode: 200, ContentType: "text/event-stream", Body: io.NopCloser(strings.NewReader(toolSSE(t, path, "call_next_read")))}, nil
				}
				if mode == "new_read" && ordinal == 1 {
					return ForwardResponse{StatusCode: 200, ContentType: "text/event-stream", Body: io.NopCloser(strings.NewReader(toolSSE(t, path, "call_next_read")))}, nil
				}
			}
		}
		reply := strings.Replace(gateSSE, `"content":"fixture"`, `"content":"Fixture ready."`, 1)
		return ForwardResponse{StatusCode: 200, ContentType: "text/event-stream", Body: io.NopCloser(strings.NewReader(reply))}, nil
	})
	a, e := NewAdapter(AdapterConfig{Scheduler: *scheduler, Manager: f.manager, Executable: exe, Current: func(GateBinding) bool {
		if mode == "root_drift" && resumed.Load() != 0 {
			prompt := filepath.Join(nextRoot, "grok-prompt.txt")
			if raw, e := os.ReadFile(prompt); e == nil && drifted.CompareAndSwap(false, true) {
				if os.Rename(nextRoot, nextRoot+"-original") != nil || os.Mkdir(nextRoot, 0700) != nil || os.WriteFile(prompt, raw, 0600) != nil {
					t.Error("root drift fixture failed")
				}
			}
		}
		return f.current.Load()
	}, Forwarder: forward})
	if e != nil {
		t.Fatal(e)
	}
	h, e := a.Start(context.Background(), old, managed.Spec{Root: root, Workspace: cwd, Timeout: 20 * time.Second, Input: []byte("FIRST_PROMPT_MARKER Reply Fixture ready.")})
	if e != nil {
		t.Fatal("original launch refused", e)
	}
	defer h.Cancel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, e := h.Wait(ctx)
	if e != nil || result.State != "succeeded" || !a.VerifyStop(result.Proof) {
		t.Fatal("original Native failed", e, result.State)
	}
	if e = a.Release(result.Proof); e != nil {
		t.Fatal(e)
	}
	archives, e := NewArchives(privateAdapterDir(t))
	if e != nil {
		t.Fatal(e)
	}
	defer archives.Close()
	ref, e := a.Checkpoint(ctx, old.ID, old.Generation, archives)
	if e != nil {
		t.Fatal("original checkpoint refused", e)
	}
	if mode == "reopen" || mode == "store_reopen" {
		dir := archives.path
		archives.Close()
		archives, e = NewArchives(dir)
		if e != nil {
			t.Fatal(e)
		}
		defer archives.Close()
		if mode == "store_reopen" {
			if e = s.Close(); e != nil {
				t.Fatal(e)
			}
			s, e = store.Open(f.storeRoot)
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			scheduler.Store = s
			f.manager = policy.NewManager("fixture-management-reopened", policy.StoreValidator(s), nil)
		}
		a, e = NewAdapter(AdapterConfig{Scheduler: *scheduler, Manager: f.manager, Executable: exe, Current: func(GateBinding) bool { return f.current.Load() }, Forwarder: forward})
		if e != nil {
			t.Fatal(e)
		}
	}
	oldBudget, _ := s.Budget(old.TaskID)
	oldConfig := resumeConfigKey(t, root)
	oldPort := resumeConfigBase(t, root)
	next, e := scheduler.Prepare(ctx, store.StartRequest{TaskID: old.TaskID, Role: old.Role, PlanRevision: old.PlanRevision, Owner: "fixture-resume-owner", TTL: time.Minute, Target: old.Target})
	if e != nil {
		t.Fatal(e)
	}
	resumed.Store(1)
	nextSpec := managed.Spec{Root: nextRoot, Workspace: cwd, Timeout: 20 * time.Second, Input: []byte("RESUME_PROMPT_MARKER Reply Fixture ready.")}
	switch mode {
	case "bad_ref":
		ref.Digest = strings.Repeat("f", 64)
	case "source_drift":
		if os.WriteFile(path, []byte("CHANGED_SOURCE"), 0600) != nil {
			t.Fatal("source drift failed")
		}
	case "target_drift":
		next.Target.Account = "caller-other-account"
	}
	nextHandle, e := a.ResumeCheckpoint(ctx, next, nextSpec, archives, ref)
	if mode == "bad_ref" || mode == "source_drift" || mode == "target_drift" || mode == "root_drift" {
		if nextHandle != nil {
			nextHandle.Cancel()
			stopped, _ := nextHandle.Wait(ctx)
			a.Release(stopped.Proof)
			t.Fatal("drifted restore launched Native")
		}
		if e == nil || resumedMain.Load() != 0 {
			t.Fatal("drifted restore accepted")
		}
		budget, _ := s.Budget(old.TaskID)
		if budget.UsedCalls != oldBudget.UsedCalls {
			t.Fatal("rejected restore spent/refunded budget")
		}
		if _, e := os.Stat(filepath.Join(nextRoot, "launch.json")); !os.IsNotExist(e) {
			t.Fatal("rejected restore published launch")
		}
		t.Logf("actual original Native; restore refused before new launch mode=%s originalHTTP=%d resumedHTTP=0 nativeLaunches=1", mode, oldBudget.UsedCalls)
		return
	}
	if e != nil {
		t.Fatal("managed resume refused", e)
	}
	defer nextHandle.Cancel()
	if mode == "cancel_read" {
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			t.Fatal("resumed request absent")
		}
		if e = nextHandle.Cancel(); e != nil {
			t.Fatal(e)
		}
		select {
		case <-transportDone:
		case <-time.After(2 * time.Second):
			t.Fatal("resume HTTP not revoked")
		}
	}
	if again, e := a.ResumeCheckpoint(ctx, next, nextSpec, archives, ref); e == nil || again != nil {
		t.Fatal("duplicate prepared run relaunched")
	}
	nextResult, e := nextHandle.Wait(ctx)
	expected := "succeeded"
	if mode == "cancel_read" {
		expected = "cancelled"
	}
	if mode == "quota_drift" {
		expected = "failed"
	}
	if e != nil || nextResult.State != expected || !nextResult.StoppedVerified || !a.VerifyStop(nextResult.Proof) || nextResult.Proof.NativeSessionID != result.Proof.NativeSessionID {
		t.Fatal("resumed Native failed/changed UUID", e, nextResult.State, nextResult.ExitCode)
	}
	if e = a.Release(nextResult.Proof); e != nil {
		t.Fatal(e)
	}
	out, text, e := a.Observation(next.ID, next.Generation)
	expectedHTTP := int64(1)
	if mode == "new_read" {
		expectedHTTP = 2
	}
	if e != nil || out.State != expected || expected == "succeeded" && text != "Fixture ready." || expected != "succeeded" && text != "" || !historyOK.Load() || oldRead && !readHistoryOK.Load() || resumedMain.Load() != expectedHTTP {
		t.Fatal("resumed context/Read/output not proven", e, out.State, historyOK.Load(), readHistoryOK.Load(), resumedMain.Load())
	}
	if out.StrictLockVerified || out.BillingVerified || out.QuotaVerified || out.StoppedVerified {
		t.Fatal("independent evidence promoted")
	}
	budget, e := s.Budget(old.TaskID)
	if e != nil || int64(budget.UsedCalls-oldBudget.UsedCalls) != expectedHTTP || int64(budget.UsedCalls) != f.calls.Load() {
		t.Fatal("resume budget mismatch", e, budget.UsedCalls, f.calls.Load())
	}
	if resumeConfigKey(t, nextRoot) == oldConfig || resumeConfigBase(t, nextRoot) == oldPort {
		t.Fatal("old grant/port reused")
	}
	info, e := archives.RestoreInfo(next.ID, next.Generation)
	if e != nil || info.RunID != next.ID || info.OriginRunID != old.ID || info.NativeSessionID != result.Proof.NativeSessionID || info.Checkpoint != ref {
		t.Fatal("persistent restore mapping mismatch", e)
	}
	if _, e = a.Checkpoint(ctx, next.ID, next.Generation, archives); expected == "succeeded" && e != nil || expected != "succeeded" && e == nil {
		t.Fatal("resumed successful run cannot be rearchived", e)
	}
	if raw, e := os.ReadFile(path); e != nil || !bytes.Equal(raw, owned) {
		t.Fatal("resume wrote Source")
	}
	t.Logf("actual managed resume: mode=%s originalHTTP=%d resumedHTTP=%d originalRead=%t state=%s exactUUID=true freshGrant/ports=true wait/proof/release=true nativeLaunches=2; admission synthetic", mode, oldBudget.UsedCalls, expectedHTTP, oldRead, nextResult.State)
}

func resumeConfig(t *testing.T, root string) map[string]any {
	t.Helper()
	raw, e := os.ReadFile(filepath.Join(root, "config", "grok", "config.toml"))
	var config map[string]any
	if e != nil || toml.Unmarshal(raw, &config) != nil {
		t.Fatal("private config read failed")
	}
	return config["model"].(map[string]any)["fusion"].(map[string]any)
}
func resumeConfigKey(t *testing.T, root string) string {
	return resumeConfig(t, root)["api_key"].(string)
}
func resumeConfigBase(t *testing.T, root string) string {
	return resumeConfig(t, root)["base_url"].(string)
}

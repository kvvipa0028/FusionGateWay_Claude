//go:build darwin

package codexadapter

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/quota"
	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/runtime/codex"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

var nativeCLI = flag.String("fusion-native-codex-adapter", "", "explicit pinned private Codex Adapter; synthetic upstream/identity/quota")

// Actual immutable CLI/Supervisor/OS stop; no genuine subscription admission.
func TestCodexAdapterPinnedNative(t *testing.T) {
	if *nativeCLI == "" {
		t.Skip("explicit pinned private Codex Adapter only")
	}
	for _, mode := range []string{"success", "rate_limit", "unsafe503", "cancel", "parent_cancel", "timeout", "source_drift", "identity_drift", "quota_before_send", "activation_refused", "snapshot_prompt", "snapshot_target", "duplicate"} {
		t.Run(mode, func(t *testing.T) { adapterNative(t, mode) })
	}
}
func adapterNative(t *testing.T, mode string) {
	c, r, in, calls, _, origin := adapterFixture(t)
	frozenTarget := cloneTarget(r.Target)
	exe, e := filepath.EvalSymlinks(*nativeCLI)
	if e != nil {
		t.Fatal(e)
	}
	c.Executable = exe
	var epoch atomic.Int64
	epoch.Store(1)
	baseIdentity := c.Identity
	c.Identity = func(target stageplan.ExecutionTarget) codex.Identity {
		x := baseIdentity(target)
		x.Generation = epoch.Load()
		return x
	}
	baseInspect := c.Scheduler.Inspect
	c.Scheduler.Inspect = func(ctx context.Context, task store.Task, role stageplan.Role, target stageplan.ExecutionTarget) (policy.Inspection, error) {
		x, e := baseInspect(ctx, task, role, target)
		if mode == "quota_before_send" {
			active, _ := c.Scheduler.Store.Run(r.ID)
			if active.State == "running" {
				x.Quota.Status = quota.Unknown
			}
		}
		if mode == "snapshot_prompt" {
			for i := range in.Input {
				in.Input[i] = 'X'
			}
		}
		if mode == "snapshot_target" {
			*r.Target.Effort.Value = "high"
		}
		return x, e
	}
	entered, exited := make(chan struct{}), make(chan struct{})
	if mode == "activation_refused" {
		c.Manager = policy.NewManager("fixture-management", func(policy.Claims) bool { return false }, nil)
	}
	c.Forwarder = fakeForwarder(func(ctx context.Context, target stageplan.ExecutionTarget, raw []byte) (codex.ForwardResponse, error) {
		if calls.Add(1) != 1 {
			t.Error("unexpected additional upstream send")
		}
		if !same(target, frozenTarget) {
			t.Error("changed frozen target")
		}
		if mode == "snapshot_prompt" {
			var v any
			json.Unmarshal(raw, &v)
			if !jsonStringContains(v, "fixture private prompt") {
				t.Error("prompt snapshot lost")
			}
		}
		if mode == "cancel" || mode == "parent_cancel" || mode == "timeout" || mode == "duplicate" {
			close(entered)
			<-ctx.Done()
			close(exited)
			return codex.ForwardResponse{}, ctx.Err()
		}
		if mode == "source_drift" {
			if os.WriteFile(filepath.Join(origin, "fixture.txt"), []byte("changed"), 0600) != nil {
				t.Error("source drift failed")
			}
		}
		if mode == "identity_drift" {
			epoch.Add(1)
		}
		status := 200
		if mode == "rate_limit" {
			status = 429
		}
		if mode == "unsafe503" {
			status = 503
		}
		return codex.ForwardResponse{StatusCode: status, ContentType: "text/event-stream", ReportedModel: target.ResolvedModel, Body: io.NopCloser(strings.NewReader(textSSE(target.ResolvedModel)))}, nil
	})
	a, e := NewAdapter(c)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if mode == "timeout" {
		in.Timeout = 2 * time.Second
	}
	h, startErr := a.Start(ctx, r, in)
	if h == nil {
		t.Fatal("actual launch refused", startErr)
	}
	defer func() { h.Cancel(); h.Wait(context.Background()) }()
	if mode == "activation_refused" {
		if startErr == nil {
			t.Fatal("activation refusal lost known handle")
		}
	} else if startErr != nil {
		t.Fatal(startErr)
	}
	if mode == "cancel" || mode == "parent_cancel" || mode == "timeout" || mode == "duplicate" {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("no inflight request")
		}
		if _, text, e := a.Observation(r.ID, r.Generation); e == nil || text != "" {
			t.Fatal("early output exposed")
		}
		if mode == "duplicate" {
			if duplicate, e := a.Start(ctx, r, in); duplicate != nil || e == nil {
				t.Fatal("duplicate launched")
			}
		}
		if mode == "parent_cancel" {
			cancel()
		} else if mode != "timeout" {
			if e := h.Cancel(); e != nil {
				t.Fatal(e)
			}
		}
	}
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer waitCancel()
	result, e := h.Wait(waitCtx)
	if e != nil || !result.StoppedVerified || !a.VerifyStop(result.Proof) {
		t.Fatal("missing actual stop", e, result.State)
	}
	want := "failed"
	switch mode {
	case "success", "snapshot_prompt", "snapshot_target":
		want = "succeeded"
	case "cancel", "parent_cancel", "timeout", "duplicate", "activation_refused", "source_drift", "identity_drift":
		want = "cancelled"
	}
	if result.State != want {
		t.Fatal("unexpected stage result", result.State, want, result.ExitCode)
	}
	out, text, e := a.Observation(r.ID, r.Generation)
	if e != nil || out.State != want {
		t.Fatal("observation disagrees with stage", e, out.State, want)
	}
	if want == "succeeded" {
		if text != "fixture" || out.ThreadID == "" || out.TurnID == "" {
			t.Fatal("missing verified terminal text")
		}
	} else if text != "" {
		t.Fatal("failed output exposed")
	}
	wantCalls := int64(1)
	if mode == "activation_refused" || mode == "quota_before_send" {
		wantCalls = 0
	}
	budget, e := c.Scheduler.Store.Budget(r.TaskID)
	if e != nil || int64(budget.UsedCalls) != wantCalls || calls.Load() != wantCalls {
		t.Fatal("send/budget mismatch", e, budget.UsedCalls, calls.Load(), wantCalls)
	}
	if e = a.Release(result.Proof); e != nil {
		t.Fatal("release refused", e)
	}
	if _, e = c.Scheduler.Store.Reservation(r.ID); !errors.Is(e, store.ErrNotFound) {
		t.Fatal("reservation held after real stop")
	}
	if e = a.Release(policy.StopProof{}); e == nil {
		t.Fatal("foreign stop proof admitted")
	}
	if _, e = os.Stat(filepath.Join(in.Root, "config", "codex", "auth.json")); !os.IsNotExist(e) {
		t.Fatal("ambient auth imported")
	}
	if mode == "cancel" || mode == "parent_cancel" || mode == "timeout" || mode == "duplicate" {
		select {
		case <-exited:
		case <-time.After(time.Second):
			t.Fatal("inflight HTTP not joined")
		}
	}
	t.Log("pinned Native Adapter; stage", result.State, "send/budget", wantCalls, "actual wait/StopProof/release; synthetic subscription admission")
}

func TestCodexAdapterPinnedPrelaunchAdmission(t *testing.T) {
	if *nativeCLI == "" {
		t.Skip("explicit pinned private Codex prelaunch only")
	}
	for _, mode := range []string{"current_false", "identity_generation", "identity_account", "identity_workspace", "identity_credential", "callback_revokes_identity"} {
		t.Run(mode, func(t *testing.T) {
			c, r, in, calls, _, _ := adapterFixture(t)
			exe, e := filepath.EvalSymlinks(*nativeCLI)
			if e != nil {
				t.Fatal(e)
			}
			c.Executable = exe
			var revoked atomic.Bool
			base := c.Identity
			c.Identity = func(target stageplan.ExecutionTarget) codex.Identity {
				x := base(target)
				switch mode {
				case "identity_generation":
					x.Generation = 0
				case "identity_account":
					x.Account = "other"
				case "identity_workspace":
					x.Workspace = "other"
				case "identity_credential":
					x.CredentialIdentity = "other"
				}
				if revoked.Load() {
					x.Generation++
				}
				return x
			}
			c.Current = func(codex.Binding) bool {
				if mode == "callback_revokes_identity" {
					revoked.Store(true)
				}
				return mode != "current_false"
			}
			a, e := NewAdapter(c)
			if e != nil {
				t.Fatal(e)
			}
			if h, e := a.Start(context.Background(), r, in); h != nil || e == nil {
				t.Fatal("unadmitted identity launched")
			}
			b, e := c.Scheduler.Store.Budget(r.TaskID)
			if e != nil || b.UsedCalls != 0 || calls.Load() != 0 {
				t.Fatal("prelaunch refusal spent")
			}
			if _, e := os.Stat(filepath.Join(in.Root, "launch.json")); !os.IsNotExist(e) {
				t.Fatal("prelaunch refusal created intent")
			}
		})
	}
}
func jsonStringContains(v any, want string) bool {
	switch x := v.(type) {
	case string:
		return x == want
	case []any:
		for _, e := range x {
			if jsonStringContains(e, want) {
				return true
			}
		}
	case map[string]any:
		for _, e := range x {
			if jsonStringContains(e, want) {
				return true
			}
		}
	}
	return false
}
func textSSE(model string) string {
	item := map[string]any{"type": "message", "id": "msg_fixture", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "fixture", "annotations": []any{}}}}
	events := []map[string]any{{"type": "response.created", "response": map[string]any{"id": "resp_fixture", "status": "in_progress", "model": model, "output": []any{}}}, {"type": "response.output_item.done", "output_index": 0, "item": item}, {"type": "response.completed", "response": map[string]any{"id": "resp_fixture", "status": "completed", "model": model, "output": []any{item}, "usage": map[string]any{"input_tokens": 3, "output_tokens": 2, "total_tokens": 5}}}}
	var b strings.Builder
	for i, x := range events {
		x["sequence_number"] = i
		raw, _ := json.Marshal(x)
		fmt.Fprintf(&b, "event: %s\ndata: %s\n\n", x["type"], raw)
	}
	return b.String()
}

var _ managed.Adapter = (*Adapter)(nil)

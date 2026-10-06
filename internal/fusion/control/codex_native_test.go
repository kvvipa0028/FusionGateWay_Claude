//go:build darwin

package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/runtime/codex"
	"github.com/yetone/magpie/internal/fusion/runtime/codexadapter"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

var nativeCodexControlCLI = flag.String("fusion-control-native-codex", "", "explicit pinned private Codex Controller; synthetic upstream/identity/quota")

type controlCodexForwarder func(context.Context, stageplan.ExecutionTarget, []byte) (codex.ForwardResponse, error)

func (f controlCodexForwarder) Send(ctx context.Context, t stageplan.ExecutionTarget, b []byte) (codex.ForwardResponse, error) {
	return f(ctx, t, b)
}

func TestControllerPinnedCodexNativeLifecycle(t *testing.T) {
	if *nativeCodexControlCLI == "" {
		t.Skip("explicit pinned Codex Controller only")
	}
	for _, mode := range []string{"success", "restore", "rate_limit", "unsafe503", "disconnect", "pause", "task_cancel", "stage_cancel", "close", "source_drift", "identity_drift", "activation_refused"} {
		t.Run(mode, func(t *testing.T) { controllerCodexNative(t, mode) })
	}
}
func controllerCodexNative(t *testing.T, mode string) {
	t.Helper()
	exe, e := filepath.EvalSymlinks(*nativeCodexControlCLI)
	if e != nil {
		t.Fatal(e)
	}
	effort := "medium"
	route := stageplan.Route{ID: "fixture-codex", Revision: 1, Model: "fixture-model", Account: "fixture-account", Workspace: "fixture-workspace", CredentialIdentity: "fixture-credential", RuntimeVersion: codex.CLIVersion, BillingPath: "subscription", BillingKnown: true, Admitted: true, Efforts: []string{effort}, DefaultEffort: &effort, Capabilities: []string{"text"}, LockEnforcement: stageplan.ControlledCalls}
	f := controlRouteFixture(t, route, stageplan.EffortSelection{Mode: stageplan.EffortDefault})
	sourceFile := controlSourceFixture(t, f)
	original, e := os.ReadFile(sourceFile)
	if e != nil {
		t.Fatal(e)
	}
	copyFile := filepath.Join(f.launch.Spec.Workspace, "source.txt")
	f.launch.Spec.Timeout = 15 * time.Second
	f.launch.Spec.Input = []byte("Remember the codeword KUMQUAT-7. Reply fixture. Do not use tools.")
	manager := policy.NewManager("fixture-management", policy.StoreValidator(f.st), nil)
	grantManager := manager
	if mode == "activation_refused" {
		grantManager = policy.NewManager("fixture-management", func(policy.Claims) bool { return false }, nil)
	}
	var calls atomic.Int64
	var secondBodyMu sync.Mutex
	var secondBody string
	var epoch atomic.Int64
	epoch.Store(1)
	entered, ended, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var enterOnce, endOnce sync.Once
	blocked := mode == "disconnect" || mode == "pause" || mode == "task_cancel" || mode == "stage_cancel" || mode == "close"
	adapter, e := codexadapter.NewAdapter(codexadapter.AdapterConfig{Scheduler: f.config.Scheduler, Manager: grantManager, Executable: exe, Identity: func(target stageplan.ExecutionTarget) codex.Identity {
		return codex.Identity{Account: target.Account, Workspace: target.Workspace, CredentialIdentity: target.CredentialIdentity, Generation: epoch.Load()}
	}, Current: func(codex.Binding) bool { return true }, Forwarder: controlCodexForwarder(func(ctx context.Context, target stageplan.ExecutionTarget, raw []byte) (codex.ForwardResponse, error) {
		n := calls.Add(1)
		if n > 2 {
			t.Error("receipt or SDK repeated upstream send")
		}
		if n == 2 {
			secondBodyMu.Lock()
			secondBody = string(raw)
			secondBodyMu.Unlock()
		}
		if target.RequestedModel != route.Model || target.ResolvedModel != route.Model || target.Account != route.Account || target.CredentialIdentity != route.CredentialIdentity || target.Effort.Value == nil || *target.Effort.Value != effort {
			t.Error("frozen target changed")
		}
		if blocked {
			enterOnce.Do(func() { close(entered) })
			select {
			case <-ctx.Done():
				endOnce.Do(func() { close(ended) })
				return codex.ForwardResponse{}, ctx.Err()
			case <-release:
			}
		}
		if mode == "source_drift" {
			if e := os.WriteFile(sourceFile, []byte("explicit source drift"), 0600); e != nil {
				t.Error(e)
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
		return codex.ForwardResponse{StatusCode: status, ContentType: "text/event-stream", ReportedModel: target.ResolvedModel, Body: io.NopCloser(strings.NewReader(controlCodexSSE(target.ResolvedModel)))}, nil
	})})
	if e != nil {
		t.Fatal(e)
	}
	var archives *codexadapter.Archives
	if mode == "restore" {
		archives, e = codexadapter.NewArchives(fixturePrivate(t))
		if e != nil {
			t.Fatal(e)
		}
		defer archives.Close()
		f.launch.Backend = BindCodexCheckpoint(adapter, archives)
	} else {
		f.launch.Backend = BindAdapter(adapter)
	}
	if f.launch.Backend.ValidateLaunch == nil {
		t.Fatal("Codex preflight hook absent")
	}
	c := f.controller(t)
	request, disconnect := context.WithCancel(context.Background())
	defer disconnect()
	key := "fixture-codex-control-start"
	var first store.StartReceipt
	authorized := manager.Management(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		first, e = c.StartAuthorized(r.Context(), key, f.in, manager.ManagementCurrent)
		if e != nil {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(204)
	}))
	req := httptest.NewRequest("POST", "/internal/fixture", nil).WithContext(request)
	req.Header.Set("Authorization", "Bearer fixture-management")
	record := httptest.NewRecorder()
	authorized.ServeHTTP(record, req)
	if !first.Created || first.Run.ID == "" {
		t.Fatal("owned intent missing", e, record.Code)
	}
	if mode == "activation_refused" {
		if e == nil || record.Code != 500 {
			t.Fatal("known handle error hidden")
		}
	} else if e != nil || record.Code != 204 {
		t.Fatal("authenticated start failed", e, record.Code)
	}
	if mode != "close" {
		repeat, e := c.Start(context.Background(), key, f.in)
		if e != nil || repeat.Created || repeat.Run.ID != first.Run.ID || f.resolved.Load() != 1 {
			t.Fatal("receipt replayed", e)
		}
	}
	if blocked {
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			t.Fatal("controlled HTTP absent")
		}
		if _, text, e := adapter.Observation(first.Run.ID, first.Run.Generation); e == nil || text != "" {
			t.Fatal("running text exposed")
		}
		task, e := f.st.Task(f.in.TaskID)
		if e != nil {
			t.Fatal(e)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		switch mode {
		case "disconnect":
			disconnect()
			select {
			case <-ended:
				t.Fatal("HTTP disconnect cancelled owned run")
			case <-time.After(30 * time.Millisecond):
			}
			close(release)
		case "pause":
			out, e := c.Pause(context.Background(), task.ID, controlVersion(task))
			if e != nil || out.Task.State != "pausing" {
				t.Fatal("pause intent failed", e)
			}
		case "task_cancel":
			out, e := c.CancelTask(context.Background(), task.ID, controlVersion(task))
			if e != nil || out.Task.State != "cancelling" {
				t.Fatal("task cancel failed", e)
			}
		case "stage_cancel":
			if _, e := c.Cancel(task.ID, first.Run.ID, first.Run.Generation); e != nil {
				t.Fatal(e)
			}
		case "close":
			if e := c.Close(ctx); e != nil {
				t.Fatal("owned close failed", e)
			}
		}
		if mode != "disconnect" {
			select {
			case <-ended:
			case <-time.After(2 * time.Second):
				t.Fatal("HTTP context stayed alive")
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	done, e := c.Wait(ctx, first.Run.ID)
	want := "succeeded"
	if mode == "rate_limit" || mode == "unsafe503" {
		want = "failed"
	}
	if blocked && mode != "disconnect" || mode == "source_drift" || mode == "identity_drift" || mode == "activation_refused" {
		want = "cancelled"
	}
	if e != nil || done.State != want || !done.StoppedVerified || !done.Released {
		t.Fatal("owned lifecycle not closed", done, e)
	}
	wantCalls := int64(1)
	if mode == "activation_refused" {
		wantCalls = 0
	}
	budget, e := f.st.Budget(f.in.TaskID)
	if e != nil || int64(budget.UsedCalls) != wantCalls || calls.Load() != wantCalls {
		t.Fatal("budget mismatch", e, budget.UsedCalls, calls.Load())
	}
	if _, e := f.st.Reservation(first.Run.ID); !errors.Is(e, store.ErrNotFound) {
		t.Fatal("reservation not released", e)
	}
	out, text, e := adapter.Observation(first.Run.ID, first.Run.Generation)
	if e != nil || out.State != want {
		t.Fatal("wrong terminal observation", e, out.State)
	}
	if want == "succeeded" && text != "fixture" || want != "succeeded" && text != "" {
		t.Fatal("wrong output visibility")
	}
	if mode == "pause" {
		task, e := f.st.Task(f.in.TaskID)
		if e != nil || task.State != "needs_review" {
			t.Fatal("pause auto-resumed")
		}
		if _, e := c.Continue(context.Background(), task.ID, controlVersion(task)); !errors.Is(e, store.ErrPauseReconcile) {
			t.Fatal("pause blindly continued", e)
		}
	}
	if mode == "task_cancel" {
		task, e := f.st.Task(f.in.TaskID)
		if e != nil || task.State != "cancelled" {
			t.Fatal("task not cancelled")
		}
		if _, e := c.Continue(context.Background(), task.ID, controlVersion(task)); !errors.Is(e, store.ErrConflict) {
			t.Fatal("cancelled task continued", e)
		}
	}
	if mode != "close" {
		repeat, e := c.Start(context.Background(), key, f.in)
		if e != nil || repeat.Created || repeat.Run.ID != first.Run.ID || f.resolved.Load() != 1 || calls.Load() != wantCalls {
			t.Fatal("terminal receipt replayed", e)
		}
	}
	after, e := os.ReadFile(copyFile)
	if e != nil || !bytes.Equal(after, original) {
		t.Fatal("readonly copy changed")
	}
	if mode != "source_drift" {
		after, e = os.ReadFile(sourceFile)
		if e != nil || !bytes.Equal(after, original) {
			t.Fatal("original source changed")
		}
	}
	if mode == "restore" {
		checkpointTask, e := f.st.Task(f.in.TaskID)
		if e != nil {
			t.Fatal(e)
		}
		ref, e := c.Checkpoint(ctx, f.in.TaskID, first.Run.ID, controlVersion(checkpointTask))
		if e != nil {
			t.Fatal("owned Controller checkpoint failed", e)
		}
		again, e := c.Checkpoint(ctx, f.in.TaskID, first.Run.ID, controlVersion(checkpointTask))
		if e != nil || again != ref || calls.Load() != wantCalls {
			t.Fatal("checkpoint recreated execution or changed ref", e)
		}
		task, e := f.st.Task(f.in.TaskID)
		if e != nil {
			t.Fatal(e)
		}
		in := f.in
		in.Generation = task.Generation
		in.Restore = &store.RestoreIdentity{OriginRunID: first.Run.ID, CheckpointID: ref.ID, CheckpointDigest: ref.Digest}
		f.launch.Spec.Root = fixturePrivate(t)
		f.launch.Spec.Input = []byte("Reply with only the codeword I gave you earlier. Do not use tools.")
		f.launch.Backend = BindCodexCheckpoint(adapter, archives)
		got, e := c.Restore(ctx, "fixture-codex-control-resume", in)
		if e != nil || !got.Created {
			t.Fatal("native restore failed", e)
		}
		restored, e := c.Wait(ctx, got.Run.ID)
		if e != nil || !restored.StoppedVerified || !restored.Released || restored.State != "succeeded" {
			t.Fatal("restore stop/release failed", e)
		}
		secondBodyMu.Lock()
		history := strings.Contains(secondBody, "KUMQUAT-7")
		secondBodyMu.Unlock()
		b, e := f.st.Budget(f.in.TaskID)
		if e != nil || int64(b.UsedCalls) != wantCalls+1 || calls.Load() != wantCalls+1 || !history {
			t.Fatal("restore accounting/history wrong", e, b.UsedCalls, calls.Load(), history)
		}
		oldRun, _ := f.st.Run(first.Run.ID)
		newRun, _ := f.st.Run(got.Run.ID)
		if newRun.ID == oldRun.ID || newRun.Generation < oldRun.Generation {
			t.Fatal("wrong restore run identity")
		}
		if _, e := f.st.Reservation(got.Run.ID); !errors.Is(e, store.ErrNotFound) {
			t.Fatal("restore capacity held after real stop")
		}
		retry, e := c.Restore(ctx, "fixture-codex-control-resume", in)
		if e != nil || retry.Created || retry.Run.ID != got.Run.ID || calls.Load() != wantCalls+1 {
			t.Fatal("restore receipt replayed", e)
		}
		t.Logf("actual Controller sealed checkpoint/restore: historyCarried=true HTTP2 budget2 sealedRef opaque; upstream/quota synthetic")
	}
	encoded, _ := json.Marshal(done)
	if bytes.Contains(encoded, []byte(f.launch.Spec.Root)) || bytes.Contains(encoded, []byte("fixture-management")) {
		t.Fatal("Completion leaked private data")
	}
	t.Log("actual pinned Codex Controller; mode", mode, "state", done.State, "send/budget", wantCalls, "real StopProof/release; identity/quota/upstream synthetic")
}
func controlCodexSSE(model string) string {
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

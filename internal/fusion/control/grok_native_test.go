//go:build darwin

package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/runtime/grok"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

var nativeGrokControlCLI = flag.String("fusion-control-native-grok", "", "explicit pinned Grok CLI for synthetic Controller/Adapter lifecycle; no real credentials")

type controlGrokForwarder func(context.Context, stageplan.ExecutionTarget, []byte) (grok.ForwardResponse, error)

func (f controlGrokForwarder) Send(ctx context.Context, t stageplan.ExecutionTarget, b []byte) (grok.ForwardResponse, error) {
	return f(ctx, t, b)
}

const controlGrokSSE = "data: {\"id\":\"fixture-response\",\"object\":\"chat.completion.chunk\",\"created\":1780000000,\"model\":\"fixture-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Fixture ready.\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"fixture-response\",\"object\":\"chat.completion.chunk\",\"created\":1780000000,\"model\":\"fixture-model\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\ndata: [DONE]\n\n"

func TestControllerPinnedGrokNativeLifecycle(t *testing.T) {
	if *nativeGrokControlCLI == "" {
		t.Skip("explicit pinned Grok Controller fixture only")
	}
	for _, mode := range []string{"success", "read", "disconnect", "pause", "task_cancel", "stage_cancel", "close"} {
		t.Run(mode, func(t *testing.T) { controllerGrokNative(t, mode) })
	}
}
func controllerGrokNative(t *testing.T, mode string) {
	t.Helper()
	exe, e := filepath.EvalSymlinks(*nativeGrokControlCLI)
	if e != nil {
		t.Fatal(e)
	}
	hash, e := managed.FileHash(exe)
	if e != nil || hash != grok.NativeExecutableSHA256 {
		t.Fatal("Native pin changed")
	}
	route := stageplan.Route{ID: "fixture-grok", Revision: 1, Model: "fixture-model", Account: "fixture-account", Workspace: "fixture-workspace", CredentialIdentity: "fixture-credential", RuntimeVersion: grok.CLIVersion, BillingPath: "subscription", BillingKnown: true, Admitted: true, NoEffort: true, Capabilities: []string{"text"}, LockEnforcement: stageplan.ControlledCalls}
	f := controlRouteFixture(t, route, stageplan.EffortSelection{Mode: stageplan.EffortNone})
	f.launch.Spec.Timeout = 15 * time.Second
	f.launch.Spec.Input = []byte("Reply Fixture ready.")
	owned := filepath.Join(f.launch.Spec.Workspace, "fixture.txt")
	ownedText := []byte("FUSION_CONTROLLER_OWNED\nline2\nline3\n")
	if os.WriteFile(owned, ownedText, 0600) != nil {
		t.Fatal("owned fixture failed")
	}
	manager := policy.NewManager("fixture-management", policy.StoreValidator(f.st), nil)
	var calls, mainCalls atomic.Int64
	entered, ended, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once, exitOnce sync.Once
	blocked := mode == "disconnect" || mode == "pause" || mode == "task_cancel" || mode == "stage_cancel" || mode == "close"
	adapter, e := grok.NewAdapter(grok.AdapterConfig{Scheduler: f.config.Scheduler, Manager: manager, Executable: exe, Current: func(grok.GateBinding) bool { return true }, Forwarder: controlGrokForwarder(func(ctx context.Context, target stageplan.ExecutionTarget, raw []byte) (grok.ForwardResponse, error) {
		calls.Add(1)
		if target.ResolvedModel != route.Model || target.Account != route.Account || target.CredentialIdentity != route.CredentialIdentity {
			t.Error("frozen controller route changed")
		}
		var req struct {
			Tools []struct{ Function struct{ Name string } }
		}
		if json.Unmarshal(raw, &req) != nil {
			return grok.ForwardResponse{}, grok.ErrProtocol
		}
		main := false
		for _, tool := range req.Tools {
			if tool.Function.Name == "read_file" {
				main = true
			}
		}
		if main {
			ordinal := mainCalls.Add(1)
			if blocked {
				once.Do(func() { close(entered) })
				select {
				case <-ctx.Done():
					exitOnce.Do(func() { close(ended) })
					return grok.ForwardResponse{}, ctx.Err()
				case <-release:
				}
			}
			if mode == "read" && ordinal == 1 {
				args, _ := json.Marshal(map[string]string{"target_file": owned})
				delta := `"content":null,"tool_calls":[{"index":0,"id":"call_controller_read","type":"function","function":{"name":"read_file","arguments":` + strconv.Quote(string(args)) + `}}]`
				reply := strings.Replace(controlGrokSSE, `"content":"Fixture ready."`, delta, 1)
				reply = strings.Replace(reply, `"finish_reason":"stop"`, `"finish_reason":"tool_calls"`, 1)
				return grok.ForwardResponse{StatusCode: 200, ContentType: "text/event-stream", Body: io.NopCloser(strings.NewReader(reply))}, nil
			}
		}
		return grok.ForwardResponse{StatusCode: 200, ContentType: "text/event-stream", Body: io.NopCloser(strings.NewReader(controlGrokSSE))}, nil
	})})
	if e != nil {
		t.Fatal(e)
	}
	f.launch.Backend = BindAdapter(adapter)
	c := f.controller(t)
	request, disconnect := context.WithCancel(context.Background())
	defer disconnect()
	key := "fixture-grok-control-start"
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
	if e != nil || record.Code != 204 || !first.Created {
		t.Fatal("authenticated controller start failed", e, record.Code)
	}
	// Reconnect with the same receipt must not resolve or start a second process.
	if mode != "close" {
		repeat, e := c.Start(context.Background(), key, f.in)
		if e != nil || repeat.Created || repeat.Run.ID != first.Run.ID || f.resolved.Load() != 1 {
			t.Fatal("durable Native receipt replayed", e)
		}
	}
	if blocked {
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			t.Fatal("Native controlled request absent")
		}
		if _, text, e := adapter.Observation(first.Run.ID, first.Run.Generation); e == nil || text != "" {
			t.Fatal("running observation exposed")
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
				t.Fatal("HTTP disconnect cancelled owned Native")
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
				t.Fatal("task cancel intent failed", e)
			}
		case "stage_cancel":
			if _, e := c.Cancel(task.ID, first.Run.ID, first.Run.Generation); e != nil {
				t.Fatal(e)
			}
		case "close":
			if e := c.Close(ctx); e != nil {
				t.Fatal("owned shutdown failed", e)
			}
		}
		if mode != "disconnect" {
			select {
			case <-ended:
			case <-time.After(2 * time.Second):
				t.Fatal("transport context stayed alive after cancel")
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	done, e := c.Wait(ctx, first.Run.ID)
	want := "succeeded"
	if blocked && mode != "disconnect" {
		want = "cancelled"
	}
	if e != nil || done.State != want || !done.StoppedVerified || !done.Released {
		t.Fatal("controller Native lifecycle not closed", done, e)
	}
	budget, e := f.st.Budget(f.in.TaskID)
	wantCalls := int64(2)
	if mode == "read" {
		wantCalls = 3
	}
	if e != nil || int64(budget.UsedCalls) != wantCalls || calls.Load() != wantCalls {
		t.Fatal("controller Native budget mismatch", budget.UsedCalls, calls.Load(), e)
	}
	if _, e := f.st.Reservation(first.Run.ID); !errors.Is(e, store.ErrNotFound) {
		t.Fatal("actual proof did not release reservation", e)
	}
	outcome, text, e := adapter.Observation(first.Run.ID, first.Run.Generation)
	if e != nil || outcome.State != want || outcome.StrictLockVerified || outcome.BillingVerified || outcome.QuotaVerified || outcome.StoppedVerified {
		t.Fatal("Native observation promoted evidence", e)
	}
	if want == "succeeded" && text != "Fixture ready." || want != "succeeded" && text != "" {
		t.Fatal("wrong terminal text")
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
			t.Fatal("task cancellation not closed")
		}
		if _, e := c.Continue(context.Background(), task.ID, controlVersion(task)); !errors.Is(e, store.ErrConflict) {
			t.Fatal("cancelled task continued", e)
		}
	}
	if mode != "close" {
		repeat, e := c.Start(context.Background(), key, f.in)
		if e != nil || repeat.Created || repeat.Run.ID != first.Run.ID || f.resolved.Load() != 1 {
			t.Fatal("terminal receipt replayed", e)
		}
	}
	after, e := os.ReadFile(owned)
	if e != nil || !bytes.Equal(after, ownedText) {
		t.Fatal("readonly project changed")
	}
	encoded, _ := json.Marshal(done)
	if bytes.Contains(encoded, []byte(f.launch.Spec.Root)) || bytes.Contains(encoded, []byte("Fixture ready.")) {
		t.Fatal("public Completion contains private data")
	}
	t.Logf("synthetic Controller mode=%s state=%s HTTP/budget=%d actualStop/release=true sourceUnchanged=true; Native/route/billing/quota proof independent", mode, done.State, wantCalls)
}

//go:build darwin

package control

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/runtime/glm"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

var nativeControlCLI = flag.String("fusion-control-native-claude", "", "explicit pinned Claude CLI for synthetic Controller/Adapter lifecycle; no real credentials")

type controlTransport func(*http.Request) (*http.Response, error)

func (f controlTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestControllerPinnedNativeRunsOnceAndReleasesActualStopProof(t *testing.T) {
	if *nativeControlCLI == "" {
		t.Skip("explicit pinned Controller Native fixture only")
	}
	exe, e := filepath.EvalSymlinks(*nativeControlCLI)
	if e != nil {
		t.Fatal(e)
	}
	f := newControlFixture(t)
	var calls atomic.Int64
	adapter, e := glm.NewAdapter(glm.AdapterConfig{Scheduler: f.config.Scheduler, Manager: policy.NewManager("fixture-management", policy.StoreValidator(f.st), nil), Executable: exe, LoadCredential: func(_ context.Context, target stageplan.ExecutionTarget) (glm.Credential, error) {
		return glm.Credential{Identity: target.CredentialIdentity, Key: "fixture-control-key"}, nil
	}, Current: func(glm.Binding) bool { return true }, Transport: controlTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.String() != "https://open.bigmodel.cn/api/anthropic/v1/messages?beta=true" || r.Header.Get("Authorization") != "Bearer fixture-control-key" {
			t.Error("unexpected Controller route")
		}
		frames := []map[string]any{
			{"type": "message_start", "message": map[string]any{"id": "msg_fixture", "type": "message", "role": "assistant", "content": []any{}, "model": "glm-5.3", "stop_reason": nil, "stop_sequence": nil, "usage": map[string]any{"input_tokens": 1, "output_tokens": 0}}},
			{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}},
			{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": "FUSION_FIXTURE_OK"}},
			{"type": "content_block_stop", "index": 0},
			{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]any{"output_tokens": 3}},
			{"type": "message_stop"},
		}
		var body strings.Builder
		for _, frame := range frames {
			raw, _ := json.Marshal(frame)
			fmt.Fprintf(&body, "event: %s\ndata: %s\n\n", frame["type"], raw)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body.String()))}, nil
	})})
	if e != nil {
		t.Fatal(e)
	}
	f.launch.Backend = BindAdapter(adapter)
	f.launch.Spec.Timeout = 15 * time.Second
	c := f.controller(t)
	request, disconnect := context.WithCancel(context.Background())
	first, e := c.Start(request, "fixture-native-start", f.in)
	if e != nil {
		t.Fatal(e)
	}
	disconnect()
	retry, e := c.Start(context.Background(), "fixture-native-start", f.in)
	if e != nil || retry.Created || retry.Run.ID != first.Run.ID {
		t.Fatal("Native replayed", e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	done, e := c.Wait(ctx, first.Run.ID)
	if e != nil || done.State != "succeeded" || !done.StoppedVerified || !done.Released {
		t.Fatal("actual Native lifecycle did not close", done, e)
	}
	budget, e := f.st.Budget(f.in.TaskID)
	if e != nil || budget.UsedCalls != 1 || calls.Load() != 1 {
		t.Fatal("Native retry spent another call", e, calls.Load())
	}
	if _, e = f.st.Reservation(first.Run.ID); e != store.ErrNotFound {
		t.Fatal("actual stop proof did not release reservation", e)
	}
	out, text, e := adapter.Observation(first.Run.ID, first.Run.Generation)
	if e != nil || out.State != "succeeded" || text != "FUSION_FIXTURE_OK" {
		t.Fatal("Native result unavailable", e)
	}
	t.Log("Controller -> PrepareOnce/CheckPrepared -> pinned GLM Adapter/owned loopback -> one synthetic HTTP/Permit -> validated Native result -> actual Supervisor wait/stop proof -> released; real model calls=0; product route/admission/billing/quota unverified")
}

func TestControllerPinnedNativePauseStopsInFlightAndRequiresReview(t *testing.T) {
	if *nativeControlCLI == "" {
		t.Skip("explicit pinned Controller Native pause fixture only")
	}
	exe, e := filepath.EvalSymlinks(*nativeControlCLI)
	if e != nil {
		t.Fatal(e)
	}
	f := newControlFixture(t)
	entered := make(chan struct{})
	var calls atomic.Int64
	adapter, e := glm.NewAdapter(glm.AdapterConfig{Scheduler: f.config.Scheduler, Manager: policy.NewManager("fixture-management", policy.StoreValidator(f.st), nil), Executable: exe, LoadCredential: func(_ context.Context, target stageplan.ExecutionTarget) (glm.Credential, error) {
		return glm.Credential{Identity: target.CredentialIdentity, Key: "fixture-control-key"}, nil
	}, Current: func(glm.Binding) bool { return true }, Transport: controlTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://open.bigmodel.cn/api/anthropic/v1/messages?beta=true" || r.Header.Get("Authorization") != "Bearer fixture-control-key" {
			t.Error("pause fixture route drift")
		}
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})})
	if e != nil {
		t.Fatal(e)
	}
	f.launch.Backend = BindAdapter(adapter)
	f.launch.Spec.Timeout = 15 * time.Second
	c := f.controller(t)
	first, e := c.Start(context.Background(), "fixture-native-pause-start", f.in)
	if e != nil {
		t.Fatal(e)
	}
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("Native never reached inflight fixture")
	}
	task, e := f.st.Task(f.in.TaskID)
	if e != nil {
		t.Fatal(e)
	}
	paused, e := c.Pause(context.Background(), task.ID, controlVersion(task))
	if e != nil || paused.Task.State != "pausing" {
		t.Fatal("Native pause not accepted", e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	done, e := c.Wait(ctx, first.Run.ID)
	if e != nil || done.State != "cancelled" || !done.StoppedVerified || !done.Released {
		t.Fatal("Native pause did not actually stop/release", done, e)
	}
	task, e = f.st.Task(task.ID)
	if e != nil || task.State != "needs_review" {
		t.Fatal("Native cancellation effects auto-resumed", e)
	}
	if _, e = c.Continue(context.Background(), task.ID, controlVersion(task)); !errors.Is(e, store.ErrPauseReconcile) {
		t.Fatal("Native blindly continued", e)
	}
	retry, e := c.Start(context.Background(), "fixture-native-pause-start", f.in)
	if e != nil || retry.Created || retry.Run.ID != first.Run.ID {
		t.Fatal("Native pause retry replayed", e)
	}
	budget, e := f.st.Budget(task.ID)
	if e != nil || budget.UsedCalls != 1 || calls.Load() != 1 {
		t.Fatal("pause retried/refunded model call", e, calls.Load())
	}
	if _, e = f.st.Reservation(first.Run.ID); !errors.Is(e, store.ErrNotFound) {
		t.Fatal("actual pause stop proof not released", e)
	}
	t.Log("Controller Pause -> persisted cancelling/pausing -> owned Native cancel -> actual wait/StopProof/release -> needs_review; one synthetic HTTP/Permit; continue and old start cannot replay; real model/quota calls=0")
}

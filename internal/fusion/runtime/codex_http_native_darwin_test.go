//go:build darwin

package runtime_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/runtime/codex"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

// Actual immutable Native and managed stop; the stage provider is not a real
// ChatGPT login, quota or subscription admission. This tests activation order.
func TestManagedPinnedCodexHTTPActivationBeforeDriver(t *testing.T) {
	if *codexNativeCLI == "" {
		t.Skip("explicit pinned Native HTTP activation test")
	}
	exe, e := filepath.EvalSymlinks(*codexNativeCLI)
	if e != nil {
		t.Fatal(e)
	}
	s, r := codexPrepared(t)
	root, cwd := codexPrivate(t), codexPrivate(t)
	claims := policy.Claims{TaskID: r.TaskID, RunID: r.ID, Role: r.Role, Attempt: r.Attempt, PlanRevision: r.PlanRevision, Generation: r.Generation, ProjectID: "fixture-project", Audience: policy.ModelAudience}
	driverStarted := make(chan struct{})
	var activated, validated atomic.Bool
	m := policy.NewManager("fixture-management", func(c policy.Claims) bool {
		select {
		case <-driverStarted:
		case <-time.After(100 * time.Millisecond):
		}
		activated.Store(true)
		return policy.StoreValidator(s)(c)
	}, nil)
	grant, e := m.PrepareModel(claims, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan struct{})
	driver := func(ctx context.Context, stream io.ReadWriteCloser) error {
		defer close(done)
		close(driverStarted)
		if !activated.Load() {
			return errors.New("driver preceded activation")
		}
		peer, e := codex.NewStdioPeer(stream, func() bool { return true })
		if e != nil {
			return e
		}
		defer peer.Close()
		raw, _ := json.Marshal(map[string]any{"clientInfo": map[string]string{"name": "fusion_gateway", "title": "Fusion Gateway", "version": "0.1.0"}, "capabilities": map[string]bool{"explicitGatewayOauth": true, "experimentalApi": false}})
		reply, e := peer.Call(ctx, 1, "initialize", raw)
		if e != nil {
			return e
		}
		var out struct {
			CodexHome string `json:"codexHome"`
		}
		if json.Unmarshal(reply, &out) != nil || out.CodexHome != filepath.Join(root, "config", "codex") {
			return codex.ErrProtocol
		}
		if e = peer.Notify(ctx, "initialized", json.RawMessage(`{}`)); e != nil {
			return e
		}
		validated.Store(true)
		return nil
	}
	ch, e := managed.NewCodexHTTPChannel(r, root, cwd, "fixture-native-http", driver, func() bool { return true }, m.Stage(claims, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) })), grant)
	if e != nil {
		t.Fatal(e)
	}
	defer ch.Close()
	spec := managed.Spec{Root: root, Workspace: cwd, Executable: exe, ExecutableHash: managed.CodexExecutableSHA256, Args: []string{"app-server", "--listen", "stdio://", "--strict-config"}, NativeSessionID: "fixture-native-http", CodexChannel: ch, Timeout: 10 * time.Second, ValidateOutcome: func([]byte) bool { return validated.Load() }}
	sup := managed.NewSupervisor(s)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	h, e := sup.Start(ctx, r, spec)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { h.Cancel(); h.Wait(context.Background()) }()
	result, e := h.Wait(ctx)
	if e != nil || !result.StoppedVerified || !sup.VerifyStop(result.Proof) {
		t.Fatal("missing actual stop", e)
	}
	if result.State != "succeeded" || !validated.Load() {
		t.Fatal("driver ran before model grant activation", result.State, result.ExitCode)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("driver did not join")
	}
	if e = (&policy.Scheduler{Store: s, VerifyStop: sup.VerifyStop}).Release(result.Proof); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Reservation(r.ID); !errors.Is(e, store.ErrNotFound) {
		t.Fatal("held after stop")
	}
	if _, e = os.Stat(filepath.Join(root, "config", "codex", "auth.json")); !os.IsNotExist(e) {
		t.Fatal("auth imported")
	}
	t.Log("pinned Native; activation before driver; wait/StopProof/release; no real login/model")
}

// Test-only raw Peer exercises an unauthenticated local stage provider. It does
// not bypass Client's production generation admission or invent ChatGPT account
// metadata. This characterizes actual HTTP shape and complete Native turn.
func TestManagedPinnedCodexHTTPText(t *testing.T) {
	for _, mode := range []string{"text", "rate_limit", "retry_rejected", "budget", "cancel"} {
		t.Run(mode, func(t *testing.T) { codexManagedHTTPText(t, mode, false) })
	}
}
func codexManagedHTTPText(t *testing.T, mode string, gateway bool) {
	if *codexNativeCLI == "" {
		t.Skip("explicit pinned Native stage HTTP test")
	}
	exe, e := filepath.EvalSymlinks(*codexNativeCLI)
	if e != nil {
		t.Fatal(e)
	}
	s, r := codexPrepared(t)
	root, cwd := codexPrivate(t), codexPrivate(t)
	claims := policy.Claims{TaskID: r.TaskID, RunID: r.ID, Role: r.Role, Attempt: r.Attempt, PlanRevision: r.PlanRevision, Generation: r.Generation, ProjectID: "fixture-project", Audience: policy.ModelAudience}
	m := policy.NewManager("fixture-management", policy.StoreValidator(s), nil)
	grant, e := m.PrepareModel(claims, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	identity := codex.Identity{Account: r.Target.Account, Workspace: r.Target.Workspace, CredentialIdentity: r.Target.CredentialIdentity, Generation: 1}
	binding := codex.Binding{Scope: codex.Scope{RunID: r.ID, Generation: r.Generation, Role: r.Role}, Identity: identity, Target: r.Target, Cwd: cwd, CodexHome: filepath.Join(root, "config", "codex")}
	entered, exited := make(chan struct{}), make(chan struct{})
	var forwarded, requests atomic.Int64
	var terminal atomic.Bool
	var completedTurns atomic.Int64
	g, e := codex.NewCallGate(codex.CallGateConfig{Binding: binding, Manager: m, Claims: claims, Current: func(codex.Binding) bool { return true }, Permit: func(ctx context.Context, c policy.Claims, target stageplan.ExecutionTarget, n int) error {
		if !m.ModelCurrent(ctx, c) || n != 1 {
			return codex.ErrIdentity
		}
		return s.ReserveCall(c.RunID, c.Generation)
	}, Forwarder: codexHTTPFakeForward(func(ctx context.Context, _ stageplan.ExecutionTarget, _ []byte) (codex.ForwardResponse, error) {
		forwarded.Add(1)
		if mode == "cancel" || mode == "interrupt" {
			close(entered)
			<-ctx.Done()
			close(exited)
			return codex.ForwardResponse{}, ctx.Err()
		}
		if mode == "rate_limit" {
			return codex.ForwardResponse{StatusCode: 429, Body: io.NopCloser(strings.NewReader("private upstream rate limit"))}, nil
		}
		if mode == "retry_rejected" {
			return codex.ForwardResponse{StatusCode: 503, Body: io.NopCloser(strings.NewReader("private upstream failure"))}, nil
		}
		return codex.ForwardResponse{StatusCode: 200, ContentType: "text/event-stream", ReportedModel: r.Target.ResolvedModel, Body: io.NopCloser(strings.NewReader(codexHTTPTextSSE(r.Target.ResolvedModel)))}, nil
	})})
	if e != nil {
		t.Fatal(e)
	}
	var shape map[string]any
	var shapeMu sync.Mutex
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests.Add(1)
		raw, _ := io.ReadAll(io.LimitReader(req.Body, (1<<20)+1))
		req.Body.Close()
		req.Body = io.NopCloser(bytes.NewReader(raw))
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		var keys, toolNames, toolTypes, metadataKeys, reasoningKeys []string
		for k := range fields {
			keys = append(keys, k)
		}
		var tools []map[string]json.RawMessage
		_ = json.Unmarshal(fields["tools"], &tools)
		for _, x := range tools {
			var name, typ string
			_ = json.Unmarshal(x["name"], &name)
			_ = json.Unmarshal(x["type"], &typ)
			toolNames = append(toolNames, name)
			toolTypes = append(toolTypes, typ)
		}
		var meta map[string]json.RawMessage
		_ = json.Unmarshal(fields["client_metadata"], &meta)
		for k := range meta {
			metadataKeys = append(metadataKeys, k)
		}
		var reason map[string]json.RawMessage
		_ = json.Unmarshal(fields["reasoning"], &reason)
		for k := range reason {
			reasoningKeys = append(reasoningKeys, k)
		}
		sort.Strings(keys)
		sort.Strings(metadataKeys)
		sort.Strings(reasoningKeys)
		shapeMu.Lock()
		shape = map[string]any{"method": req.Method, "path": req.URL.Path, "content_encoding": req.Header.Get("Content-Encoding"), "keys": keys, "tool_names": toolNames, "tool_types": toolTypes, "metadata_keys": metadataKeys, "reasoning_keys": reasoningKeys}
		shapeMu.Unlock()
		g.ServeHTTP(w, req)
	})
	driver := func(ctx context.Context, stream io.ReadWriteCloser) error {
		peer, e := codex.NewStdioPeer(stream, func() bool { return true })
		if e != nil {
			return e
		}
		defer peer.Close()
		if gateway {
			client, e := codex.NewGateway(codexGatewayShapePeer{Peer: peer, t: t}, binding, func(scope codex.Scope) bool { return scope == binding.Scope }, func() codex.Identity { return identity }, func() bool { return true })
			if e != nil {
				return e
			}
			if e = client.Initialize(ctx); e != nil {
				return e
			}
			if e = client.ReadAccount(ctx); e != nil {
				return e
			}
			if _, e = client.StartThread(ctx); e != nil {
				t.Log("typed thread refusal", e)
				return e
			}
			if _, e = client.StartTurn(ctx, "Reply with fixture. Do not use tools."); e != nil {
				return e
			}
			if mode == "interrupt" {
				select {
				case <-entered:
				case <-ctx.Done():
					return ctx.Err()
				}
				if e := client.Interrupt(ctx); e != nil {
					return e
				}
			}
			var methods []string
			defer func() { t.Log("typed Native observed methods", methods) }()
			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case raw, ok := <-peer.Notifications():
					if !ok {
						return codex.ErrNative
					}
					var envelope struct {
						Method string `json:"method"`
					}
					if json.Unmarshal(raw, &envelope) != nil {
						return codex.ErrProtocol
					}
					methods = append(methods, envelope.Method)
					state, e := client.Event(binding.Scope, raw)
					if e != nil {
						var shape struct{ Params map[string]json.RawMessage }
						json.Unmarshal(raw, &shape)
						projection := map[string]any{}
						for k, v := range shape.Params {
							var str string
							if json.Unmarshal(v, &str) == nil && !bytes.Equal(v, []byte("null")) {
								projection[k] = fmt.Sprintf("string bytes=%d", len(str))
							} else {
								projection[k] = fmt.Sprintf("JSON bytes=%d", len(v))
							}
						}
						t.Log("typed event refusal", envelope.Method, e, projection)
						return e
					}
					switch state {
					case "succeeded", "failed", "interrupted":
						expected := "succeeded"
						if mode == "rate_limit" || mode == "retry_rejected" {
							expected = "failed"
						}
						if mode == "interrupt" {
							expected = "interrupted"
						}
						if state != expected {
							return codex.ErrNative
						}
						completedTurns.Add(1)
						terminal.Store(true)
						return nil
					}
				}
			}
		}
		call := func(id uint64, method string, params any) (json.RawMessage, error) {
			raw, _ := json.Marshal(params)
			return peer.Call(ctx, id, method, raw)
		}
		if _, e = call(1, "initialize", map[string]any{"clientInfo": map[string]string{"name": "fusion_gateway", "title": "Fusion Gateway", "version": "0.1.0"}, "capabilities": map[string]bool{"explicitGatewayOauth": true, "experimentalApi": false}}); e != nil {
			return e
		}
		if e = peer.Notify(ctx, "initialized", json.RawMessage(`{}`)); e != nil {
			return e
		}
		account, e := call(2, "account/read", map[string]bool{"refreshToken": false})
		var a struct {
			Account  json.RawMessage `json:"account"`
			Requires *bool           `json:"requiresOpenaiAuth"`
		}
		if e != nil || json.Unmarshal(account, &a) != nil || a.Requires == nil || *a.Requires || string(a.Account) != "null" {
			return codex.ErrProtocol
		}
		rounds := 1
		if mode == "budget" {
			rounds = 3
		}
		for round := 0; round < rounds; round++ {
			reply, e := call(uint64(3+round*2), "thread/start", map[string]any{"model": r.Target.ResolvedModel, "modelProvider": managed.CodexStageProvider, "cwd": cwd, "approvalPolicy": "never", "sandbox": "read-only", "ephemeral": true, "config": map[string]any{"model_reasoning_effort": *r.Target.Effort.Value, "model_reasoning_summary": "none"}})
			if e != nil {
				return e
			}
			var thread struct {
				Thread struct {
					ID string `json:"id"`
				} `json:"thread"`
				Model    string `json:"model"`
				Provider string `json:"modelProvider"`
			}
			if json.Unmarshal(reply, &thread) != nil || thread.Thread.ID == "" || thread.Model != r.Target.ResolvedModel || thread.Provider != managed.CodexStageProvider {
				return codex.ErrProtocol
			}
			reply, e = call(uint64(4+round*2), "turn/start", map[string]any{"threadId": thread.Thread.ID, "model": r.Target.ResolvedModel, "effort": *r.Target.Effort.Value, "cwd": cwd, "approvalPolicy": "never", "sandboxPolicy": map[string]any{"type": "readOnly", "networkAccess": false}, "summary": "none", "input": []any{map[string]any{"type": "text", "text": "Reply with fixture. Do not use tools."}}})
			if e != nil {
				return e
			}
			var turn struct {
				Turn struct {
					ID     string `json:"id"`
					Status string `json:"status"`
				} `json:"turn"`
			}
			if json.Unmarshal(reply, &turn) != nil || turn.Turn.ID == "" || turn.Turn.Status != "inProgress" {
				return codex.ErrProtocol
			}
		notifications:
			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case raw, ok := <-peer.Notifications():
					if !ok {
						return codex.ErrProtocol
					}
					var n struct {
						Method string `json:"method"`
						Params struct {
							ThreadID string `json:"threadId"`
							Turn     struct {
								ID     string `json:"id"`
								Status string `json:"status"`
							} `json:"turn"`
						} `json:"params"`
					}
					if json.Unmarshal(raw, &n) != nil {
						return codex.ErrProtocol
					}
					if n.Method == "turn/completed" {
						expected := "completed"
						if mode == "rate_limit" || mode == "retry_rejected" || mode == "budget" && round == rounds-1 {
							expected = "failed"
						}
						if n.Params.ThreadID != thread.Thread.ID || n.Params.Turn.ID != turn.Turn.ID || n.Params.Turn.Status != expected {
							return codex.ErrNative
						}
						completedTurns.Add(1)
						break notifications
					}
				}
			}
		}
		terminal.Store(true)
		return nil
	}
	ch, e := managed.NewCodexHTTPChannel(r, root, cwd, "fixture-native-http", driver, func() bool { return true }, handler, grant)
	if e != nil {
		t.Fatal(e)
	}
	defer ch.Close()
	sup := managed.NewSupervisor(s)
	spec := managed.Spec{Root: root, Workspace: cwd, Executable: exe, ExecutableHash: managed.CodexExecutableSHA256, Args: []string{"app-server", "--listen", "stdio://", "--strict-config"}, NativeSessionID: "fixture-native-http", CodexChannel: ch, Timeout: 15 * time.Second, ValidateOutcome: func([]byte) bool {
		return mode == "text" && terminal.Load() && g.Audit().Completed == 1 && !g.Audit().Uncertain
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Second)
	defer cancel()
	h, e := sup.Start(ctx, r, spec)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { h.Cancel(); h.Wait(context.Background()) }()
	if mode == "cancel" {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("native did not reach inflight HTTP")
		}
		if ch.Close() == nil {
			t.Fatal("active channel released its ports")
		}
		if e = h.Cancel(); e != nil {
			t.Fatal(e)
		}
	}
	result, e := h.Wait(ctx)
	if e != nil || !result.StoppedVerified || !sup.VerifyStop(result.Proof) {
		t.Fatal("actual stop missing", e)
	}
	if e = (&policy.Scheduler{Store: s, VerifyStop: sup.VerifyStop}).Release(result.Proof); e != nil {
		t.Fatal(e)
	}
	shapeMu.Lock()
	t.Log(shape)
	shapeMu.Unlock()
	b, e := s.Budget(r.TaskID)
	wantState, wantRequests, wantForwarded := "succeeded", int64(1), int64(1)
	if mode == "rate_limit" || mode == "interrupt" {
		wantState = "failed"
	}
	if mode == "retry_rejected" {
		wantState, wantRequests, wantForwarded = "failed", 4, 1
	}
	if mode == "budget" {
		wantState, wantRequests, wantForwarded = "failed", 5, 2
	}
	wantTurns := int64(1)
	if mode == "budget" {
		wantTurns = 3
	}
	if mode == "cancel" {
		wantState, wantTurns = "cancelled", 0
		select {
		case <-exited:
		case <-time.After(time.Second):
			t.Fatal("inflight HTTP did not join")
		}
	}
	if e != nil || completedTurns.Load() != wantTurns || result.State != wantState || forwarded.Load() != wantForwarded || requests.Load() != wantRequests || b.UsedCalls != int(wantForwarded) || terminal.Load() != (mode != "cancel") {
		t.Fatal("pinned Native HTTP not accepted", result.State, result.ExitCode, "requests", requests.Load(), "forwarded", forwarded.Load(), "budget", b.UsedCalls, "audit", g.Audit())
	}
	t.Log("pinned Native scoped HTTP", mode, "requests", requests.Load(), "forwarded", forwarded.Load(), "budget", b.UsedCalls, "turns", completedTurns.Load(), "audit", g.Audit(), "; synthetic upstream/account; actual wait/StopProof/release")
}

type codexHTTPFakeForward func(context.Context, stageplan.ExecutionTarget, []byte) (codex.ForwardResponse, error)

func (f codexHTTPFakeForward) Send(ctx context.Context, target stageplan.ExecutionTarget, raw []byte) (codex.ForwardResponse, error) {
	return f(ctx, target, raw)
}
func codexHTTPTextSSE(model string) string {
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

func TestManagedPinnedCodexHTTPActivationFailureStopsWithoutDriver(t *testing.T) {
	if *codexNativeCLI == "" {
		t.Skip("explicit pinned Native activation failure")
	}
	exe, e := filepath.EvalSymlinks(*codexNativeCLI)
	if e != nil {
		t.Fatal(e)
	}
	s, r := codexPrepared(t)
	root, cwd := codexPrivate(t), codexPrivate(t)
	claims := policy.Claims{TaskID: r.TaskID, RunID: r.ID, Role: r.Role, Attempt: r.Attempt, PlanRevision: r.PlanRevision, Generation: r.Generation, ProjectID: "fixture-project", Audience: policy.ModelAudience}
	m := policy.NewManager("fixture-management", func(policy.Claims) bool { return false }, nil)
	grant, e := m.PrepareModel(claims, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	var driverCalls, httpCalls atomic.Int64
	ch, e := managed.NewCodexHTTPChannel(r, root, cwd, "fixture-native-http", func(context.Context, io.ReadWriteCloser) error { driverCalls.Add(1); return nil }, func() bool { return true }, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { httpCalls.Add(1); w.WriteHeader(403) }), grant)
	if e != nil {
		t.Fatal(e)
	}
	defer ch.Close()
	sup := managed.NewSupervisor(s)
	spec := managed.Spec{Root: root, Workspace: cwd, Executable: exe, ExecutableHash: managed.CodexExecutableSHA256, Args: []string{"app-server", "--listen", "stdio://", "--strict-config"}, NativeSessionID: "fixture-native-http", CodexChannel: ch, Timeout: 10 * time.Second, ValidateOutcome: func([]byte) bool { return true }}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	h, e := sup.Start(ctx, r, spec)
	if !errors.Is(e, managed.ErrLaunch) || h == nil {
		t.Fatal("failed activation did not retain owned handle", e)
	}
	defer func() { h.Cancel(); h.Wait(context.Background()) }()
	result, e := h.Wait(ctx)
	if e != nil || result.State != "cancelled" || !result.StoppedVerified || !sup.VerifyStop(result.Proof) || driverCalls.Load() != 0 || httpCalls.Load() != 0 {
		t.Fatal("failed activation escaped", e, result.State, driverCalls.Load(), httpCalls.Load())
	}
	if e = (&policy.Scheduler{Store: s, VerifyStop: sup.VerifyStop}).Release(result.Proof); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Reservation(r.ID); !errors.Is(e, store.ErrNotFound) {
		t.Fatal("reservation retained")
	}
	if _, e = grant.Secret(); e == nil {
		t.Fatal("failed grant survived stop")
	}
	t.Log("actual pinned Native stopped/reaped after activation refusal; driver0 HTTP0; reservation released")
}

func TestManagedPinnedCodexGatewayClient(t *testing.T) {
	for _, mode := range []string{"text", "rate_limit", "retry_rejected", "cancel", "interrupt"} {
		t.Run(mode, func(t *testing.T) { codexManagedHTTPText(t, mode, true) })
	}
}

type codexGatewayShapePeer struct {
	codex.Peer
	t *testing.T
}

func (p codexGatewayShapePeer) Call(ctx context.Context, id uint64, method string, raw json.RawMessage) (json.RawMessage, error) {
	out, e := p.Peer.Call(ctx, id, method, raw)
	if method == "account/read" && e == nil {
		var fields map[string]json.RawMessage
		json.Unmarshal(out, &fields)
		keys := []string{}
		for k := range fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		p.t.Log("Native local account keys", keys)
	}
	return out, e
}

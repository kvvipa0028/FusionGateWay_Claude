package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/stageplan"
)

type gateForward func(context.Context, stageplan.ExecutionTarget, []byte) (ForwardResponse, error)

func (f gateForward) Send(c context.Context, t stageplan.ExecutionTarget, b []byte) (ForwardResponse, error) {
	return f(c, t, b)
}

const gateBody = `{"model":"fixture-model","instructions":"fixture","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"fixture"}]}],"tools":[],"tool_choice":"auto","parallel_tool_calls":false,"reasoning":{"effort":"high"},"store":false,"stream":true,"include":["reasoning.encrypted_content"]}`

func gateSSE() string {
	item := map[string]any{"type": "message", "id": "msg_fixture", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "fixture", "annotations": []any{}}}}
	events := []map[string]any{
		{"type": "response.created", "response": map[string]any{"id": "resp_fixture", "status": "in_progress", "model": "fixture-model", "output": []any{}}},
		{"type": "response.output_item.done", "output_index": 0, "item": item},
		{"type": "response.completed", "response": map[string]any{"id": "resp_fixture", "status": "completed", "model": "fixture-model", "output": []any{item}, "usage": map[string]any{"input_tokens": 3, "output_tokens": 2, "total_tokens": 5}, "error": nil, "incomplete_details": nil}},
	}
	var b strings.Builder
	for i, e := range events {
		e["sequence_number"] = i
		raw, _ := json.Marshal(e)
		fmt.Fprintf(&b, "event: %s\ndata: %s\n\n", e["type"], raw)
	}
	return b.String()
}

type gateFixture struct {
	config         CallGateConfig
	manager        *policy.Manager
	token          string
	current        atomic.Bool
	calls, permits atomic.Int64
}

func newGateFixture(t *testing.T) *gateFixture {
	t.Helper()
	_, x := newFixture(t)
	b := Binding{Scope: x.scope, Identity: x.identity, Target: stageplan.ExecutionTarget{Route: stageplan.RouteRef{ID: "fixture-native", Revision: 1}, RequestedModel: "fixture-model", ResolvedModel: "fixture-model", Account: x.identity.Account, Workspace: x.identity.Workspace, CredentialIdentity: x.identity.CredentialIdentity, RuntimeVersion: CLIVersion, BillingPath: "subscription", LockEnforcement: stageplan.ControlledCalls, Effort: stageplan.FrozenEffort{RequestedMode: stageplan.EffortExplicit, Value: ptr("high")}}, Cwd: "/fixture/project", CodexHome: "/fixture/codex"}
	f := &gateFixture{}
	f.current.Store(true)
	c := policy.Claims{TaskID: "fixture-task", RunID: b.Scope.RunID, Role: b.Scope.Role, Attempt: 1, PlanRevision: 1, Generation: b.Scope.Generation, ProjectID: "fixture-project", Audience: policy.ModelAudience}
	f.manager = policy.NewManager("fixture-management", func(got policy.Claims) bool { return f.current.Load() && got == c }, nil)
	var e error
	f.token, e = f.manager.Issue(c, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	f.config = CallGateConfig{Binding: b, Manager: f.manager, Claims: c, Current: func(got Binding) bool {
		return f.current.Load() && got.Identity == b.Identity && got.Target.Account == b.Target.Account
	}, Permit: func(context.Context, policy.Claims, stageplan.ExecutionTarget, int) error {
		f.permits.Add(1)
		return nil
	}, Forwarder: gateForward(func(ctx context.Context, got stageplan.ExecutionTarget, raw []byte) (ForwardResponse, error) {
		f.calls.Add(1)
		if got.Account != b.Target.Account || got.ResolvedModel != "fixture-model" || strings.Contains(string(raw), f.token) {
			t.Error("authority escaped")
		}
		return gateResponse(gateSSE()), nil
	})}
	return f
}
func ptr(s string) *string { return &s }
func gateResponse(s string) ForwardResponse {
	return ForwardResponse{StatusCode: 200, ContentType: "text/event-stream", ReportedModel: "fixture-model", Body: io.NopCloser(strings.NewReader(s))}
}
func newGate(t *testing.T, f *gateFixture) *CallGate {
	t.Helper()
	g, e := NewCallGate(f.config)
	if e != nil {
		t.Fatal(e)
	}
	return g
}
func send(g *CallGate, f *gateFixture, body, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+f.token)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Magpie-Account", "other")
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	return w
}
func gateHealthy(g *CallGate, f *gateFixture) bool {
	r := httptest.NewRequest("POST", "/responses", nil)
	r.Header.Set("Authorization", "Bearer "+f.token)
	result := false
	f.manager.Stage(f.config.Claims, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { result = g.Healthy(r.Context()) })).ServeHTTP(httptest.NewRecorder(), r)
	return result
}
func TestCodexCallGateSpendsEverySendAndRetry(t *testing.T) {
	f := newGateFixture(t)
	f.config.Forwarder = gateForward(func(context.Context, stageplan.ExecutionTarget, []byte) (ForwardResponse, error) {
		if f.calls.Add(1) == 1 {
			return ForwardResponse{StatusCode: 429, Body: io.NopCloser(strings.NewReader("private failure"))}, nil
		}
		return gateResponse(gateSSE()), nil
	})
	g := newGate(t, f)
	for _, want := range []int{429, 200, 200} {
		w := send(g, f, gateBody, "/responses")
		if w.Code != want {
			t.Fatal(w.Code, want)
		}
	}
	a := g.Audit()
	if a.Attempts != 3 || a.Permits != 3 || a.Forwarded != 3 || a.Completed != 2 || a.Retryable != 1 || !gateHealthy(g, f) {
		t.Fatal(a)
	}
}
func TestCodexCallGateRejectsInvalidRequestsBeforeSpend(t *testing.T) {
	for _, mode := range []string{"model", "effort", "duplicate", "alias", "unknown", "tool", "history_tool", "summary", "stored", "nonstream", "include", "role", "image", "null", "service_tier", "metadata", "query", "absolute", "compact", "encoded_path", "gzip", "get", "oversize", "trailing", "subagent", "stage_reflection", "escaped_reflection"} {
		t.Run(mode, func(t *testing.T) {
			f := newGateFixture(t)
			g := newGate(t, f)
			body, path := gateBody, "/responses"
			switch mode {
			case "model":
				body = strings.Replace(body, "fixture-model", "other", 1)
			case "effort":
				body = strings.Replace(body, "high", "low", 1)
			case "duplicate":
				body = strings.Replace(body, `"model":`, `"model":"other","model":`, 1)
			case "alias":
				body = strings.Replace(body, `"model":`, `"MODEL":`, 1)
			case "unknown":
				body = strings.Replace(body, `"instructions":`, `"route":`, 1)
			case "tool":
				body = strings.Replace(body, `"tools":[]`, `"tools":[{"type":"web_search"}]`, 1)
			case "history_tool":
				body = strings.Replace(body, `"type":"message"`, `"type":"function_call"`, 1)
			case "summary":
				body = strings.Replace(body, `"effort":"high"`, `"effort":"high","summary":"auto"`, 1)
			case "stored":
				body = strings.Replace(body, `"store":false`, `"store":true`, 1)
			case "nonstream":
				body = strings.Replace(body, `"stream":true`, `"stream":false`, 1)
			case "include":
				body = strings.Replace(body, "reasoning.encrypted_content", "web_search_call.action.sources", 1)
			case "role":
				body = strings.Replace(body, `"role":"user"`, `"role":"tool"`, 1)
			case "image":
				body = strings.Replace(body, "input_text", "input_image", 1)
			case "null":
				body = strings.Replace(body, `"tools":[]`, `"tools":null`, 1)
			case "service_tier":
				body = strings.Replace(body, `"tools":[]`, `"service_tier":"priority","tools":[]`, 1)
			case "metadata":
				body = strings.Replace(body, `"tools":[]`, `"client_metadata":{"account":"other"},"tools":[]`, 1)
			case "query":
				path += "?model=other"
			case "absolute":
				path = "https://other.invalid/responses"
			case "compact":
				path += "/compact"
			case "encoded_path":
				path = "/%72esponses"
			case "oversize":
				body = strings.Repeat(" ", 1<<20) + body
			case "trailing":
				body += " {}"
			case "stage_reflection":
				body = strings.Replace(body, "fixture", f.token, 1)
			case "escaped_reflection":
				body = strings.Replace(body, `"instructions":"fixture"`, `"instructions":"\u0066`+f.token[1:]+`"`, 1)
			}
			r := httptest.NewRequest("POST", path, strings.NewReader(body))
			r.Header.Set("Authorization", "Bearer "+f.token)
			r.Header.Set("Content-Type", "application/json")
			if mode == "gzip" {
				r.Header.Set("Content-Encoding", "gzip")
			}
			if mode == "get" {
				r.Method = "GET"
			}
			if mode == "subagent" {
				r.Header.Set("X-OpenAI-Subagent", "other")
			}
			w := httptest.NewRecorder()
			g.ServeHTTP(w, r)
			if w.Code < 400 || f.calls.Load() != 0 || f.permits.Load() != 0 || !g.Audit().Uncertain || strings.Contains(w.Body.String(), f.token) {
				t.Fatal(w.Code, g.Audit())
			}
			if w = send(g, f, gateBody, "/responses"); w.Code < 400 || f.calls.Load() != 0 {
				t.Fatal("failure erased")
			}
		})
	}
}
func TestCodexCallGateAuthenticationDoesNotPoisonRun(t *testing.T) {
	for _, mode := range []string{"token", "management", "duplicate", "origin"} {
		t.Run(mode, func(t *testing.T) {
			f := newGateFixture(t)
			g := newGate(t, f)
			r := httptest.NewRequest("POST", "/responses", strings.NewReader(gateBody))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Authorization", "Bearer "+f.token)
			switch mode {
			case "token":
				r.Header.Set("Authorization", "Bearer wrong")
			case "management":
				r.Header.Set("Authorization", "Bearer fixture-management")
			case "duplicate":
				r.Header.Add("Authorization", "Bearer "+f.token)
			case "origin":
				r.Header.Set("Origin", "https://other.invalid")
			}
			w := httptest.NewRecorder()
			g.ServeHTTP(w, r)
			if w.Code < 400 || g.Audit().Uncertain || f.calls.Load() != 0 {
				t.Fatal(w.Code, g.Audit())
			}
			if w = send(g, f, gateBody, "/responses"); w.Code != 200 {
				t.Fatal(w.Code)
			}
		})
	}
}
func TestCodexCallGateRechecksAfterSpendAndOutput(t *testing.T) {
	for _, mode := range []string{"permit", "revoked", "identity", "send_drift", "body_drift", "transport", "mutate"} {
		t.Run(mode, func(t *testing.T) {
			f := newGateFixture(t)
			f.config.Permit = func(ctx context.Context, c policy.Claims, target stageplan.ExecutionTarget, n int) error {
				f.permits.Add(1)
				switch mode {
				case "permit":
					return errors.New("private detail")
				case "revoked":
					f.manager.RevokeRun(c.RunID)
				case "identity":
					f.current.Store(false)
				case "mutate":
					*target.Effort.Value = "low"
					target.Account = "other"
				}
				return nil
			}
			original := f.config.Forwarder
			f.config.Forwarder = gateForward(func(ctx context.Context, target stageplan.ExecutionTarget, b []byte) (ForwardResponse, error) {
				if mode == "transport" {
					f.calls.Add(1)
					return ForwardResponse{}, errors.New("private upstream detail")
				}
				resp, e := original.Send(ctx, target, b)
				if mode == "send_drift" {
					f.current.Store(false)
				}
				if mode == "body_drift" {
					resp.Body = &driftReader{Reader: strings.NewReader(gateSSE()), drift: func() { f.current.Store(false) }}
				}
				return resp, e
			})
			g := newGate(t, f)
			w := send(g, f, gateBody, "/responses")
			if mode == "mutate" {
				if w.Code != 200 || *g.binding.Target.Effort.Value != "high" {
					t.Fatal(w.Code)
				}
			} else if w.Code < 400 || !g.Audit().Uncertain || strings.Contains(w.Body.String(), "private") {
				t.Fatal(w.Code, g.Audit())
			}
		})
	}
}

type driftReader struct {
	io.Reader
	drift func()
}

func (r *driftReader) Read(b []byte) (int, error) { n, e := r.Reader.Read(b); r.drift(); return n, e }
func (r *driftReader) Close() error               { return nil }
func TestCodexCallGateNeverReleasesUnsafePartialResponse(t *testing.T) {
	for _, mode := range []string{"model", "reported_model", "missing_model", "missing_terminal", "failed", "unknown", "tool", "duplicate", "terminal_id", "output_mismatch", "usage", "encoding", "status", "event_name", "extra_terminal", "unterminated", "stage_reflection", "escaped_reflection", "credential_reflection", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			f := newGateFixture(t)
			body := gateSSE()
			resp := gateResponse(body)
			switch mode {
			case "model":
				body = strings.Replace(body, "fixture-model", "other", 1)
			case "reported_model":
				resp.ReportedModel = "other"
			case "missing_model":
				resp.ReportedModel = ""
			case "missing_terminal":
				body = body[:strings.LastIndex(body, "event: response.completed")]
			case "failed":
				body = strings.ReplaceAll(body, "response.completed", "response.failed")
			case "unknown":
				body = strings.ReplaceAll(body, "response.output_item.done", "response.web_search_call.completed")
			case "tool":
				body = strings.Replace(body, `"type":"message"`, `"type":"function_call"`, 1)
			case "duplicate":
				body = strings.Replace(body, `"id":"resp_fixture"`, `"id":"resp_other","id":"resp_fixture"`, 1)
			case "terminal_id":
				i := strings.LastIndex(body, "resp_fixture")
				body = body[:i] + strings.Replace(body[i:], "resp_fixture", "resp_other", 1)
			case "output_mismatch":
				i := strings.LastIndex(body, `"text":"fixture"`)
				body = body[:i] + strings.Replace(body[i:], `"text":"fixture"`, `"text":"other"`, 1)
			case "usage":
				body = strings.Replace(body, `"total_tokens":5`, `"total_tokens":6`, 1)
			case "encoding":
				resp.ContentEncoding = "gzip"
			case "status":
				resp.StatusCode = 503
			case "event_name":
				body = strings.Replace(body, "event: response.created", "event: other", 1)
			case "extra_terminal":
				body += body[strings.LastIndex(body, "event: response.completed"):]
			case "unterminated":
				body = strings.TrimSuffix(body, "\n\n")
			case "stage_reflection":
				body = strings.ReplaceAll(body, `"text":"fixture"`, `"text":"`+f.token+`"`)
			case "escaped_reflection":
				body = strings.ReplaceAll(body, `"text":"fixture"`, `"text":"\u0066`+f.token[1:]+`"`)
			case "credential_reflection":
				resp.PrivateMarkers = [][]byte{[]byte("fixture-credential-secret")}
				body = strings.ReplaceAll(body, `"text":"fixture"`, `"text":"fixture-credential-secret"`)
			case "oversize":
				body = strings.Repeat(" ", 8<<20) + body
			}
			resp.Body = io.NopCloser(strings.NewReader(body))
			f.config.Forwarder = gateForward(func(context.Context, stageplan.ExecutionTarget, []byte) (ForwardResponse, error) {
				f.calls.Add(1)
				return resp, nil
			})
			g := newGate(t, f)
			w := send(g, f, gateBody, "/responses")
			if w.Code < 400 || strings.Contains(w.Body.String(), "data:") || f.permits.Load() != 1 || f.calls.Load() != 1 || !g.Audit().Uncertain {
				t.Fatal(w.Code, g.Audit())
			}
		})
	}
}
func TestCodexCallGateChecksGrantAfterCurrentCallback(t *testing.T) {
	f := newGateFixture(t)
	f.config.Current = func(b Binding) bool {
		if f.calls.Load() > 0 {
			f.manager.RevokeRun(f.config.Claims.RunID)
		}
		return true
	}
	g := newGate(t, f)
	w := send(g, f, gateBody, "/responses")
	if w.Code < 400 || strings.Contains(w.Body.String(), "data:") {
		t.Fatal("revoked callback released output", w.Code)
	}
}
func TestCodexCallGateRejectsUnfinishedAddedItem(t *testing.T) {
	f := newGateFixture(t)
	body := gateSSE()
	idx := strings.LastIndex(body, "event: response.completed")
	extra := "event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":1,\"item\":{\"type\":\"message\",\"id\":\"msg_unfinished\",\"role\":\"assistant\",\"status\":\"in_progress\",\"content\":[]}}\n\n"
	body = body[:idx] + extra + body[idx:]
	f.config.Forwarder = gateForward(func(context.Context, stageplan.ExecutionTarget, []byte) (ForwardResponse, error) {
		return gateResponse(body), nil
	})
	g := newGate(t, f)
	w := send(g, f, gateBody, "/responses")
	if w.Code < 400 {
		t.Fatal("unfinished item ignored", w.Code)
	}
}
func TestCodexCallGateRequiresFrozenControllerBinding(t *testing.T) {
	for _, mode := range []string{"manager", "current", "permit", "forwarder", "account", "identity", "scope", "claims", "audience", "revision", "version", "billing", "plugin", "lock", "effort", "path", "role"} {
		t.Run(mode, func(t *testing.T) {
			f := newGateFixture(t)
			c := f.config
			switch mode {
			case "manager":
				c.Manager = nil
			case "current":
				c.Current = nil
			case "permit":
				c.Permit = nil
			case "forwarder":
				c.Forwarder = nil
			case "account":
				c.Binding.Target.Account = "other"
			case "identity":
				c.Binding.Identity.Generation = 0
			case "scope":
				c.Binding.Scope.Generation = 0
			case "claims":
				c.Claims.RunID = "other"
			case "audience":
				c.Claims.Audience = policy.EventsAudience
			case "revision":
				c.Binding.Target.Route.Revision = 0
			case "version":
				c.Binding.Target.RuntimeVersion = "other"
			case "billing":
				c.Binding.Target.BillingPath = "api"
			case "plugin":
				c.Binding.Target.PluginVersion = ptr("other")
			case "lock":
				c.Binding.Target.LockEnforcement = "other"
			case "effort":
				c.Binding.Target.Effort.Value = nil
			case "path":
				c.Binding.CodexHome = "relative"
			case "role":
				c.Binding.Scope.Role = "other"
			}
			if _, e := NewCallGate(c); e == nil {
				t.Fatal("untrusted binding accepted")
			}
		})
	}
	f := newGateFixture(t)
	g := newGate(t, f)
	*f.config.Binding.Target.Effort.Value = "low"
	f.config.Binding.Target.Account = "other"
	if w := send(g, f, gateBody, "/responses"); w.Code != 200 {
		t.Fatal("constructor retained mutable target", w.Code)
	}
}
func TestCodexCallGateSerializesSendsAndCancelsWaiter(t *testing.T) {
	f := newGateFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	f.config.Forwarder = gateForward(func(ctx context.Context, target stageplan.ExecutionTarget, raw []byte) (ForwardResponse, error) {
		f.calls.Add(1)
		close(entered)
		select {
		case <-release:
			return gateResponse(gateSSE()), nil
		case <-ctx.Done():
			return ForwardResponse{}, ctx.Err()
		}
	})
	g := newGate(t, f)
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- send(g, f, gateBody, "/responses") }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("send did not start")
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest("POST", "/responses", strings.NewReader(gateBody)).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer "+f.token)
	r.Header.Set("Content-Type", "application/json")
	second := make(chan *httptest.ResponseRecorder, 1)
	go func() { w := httptest.NewRecorder(); g.ServeHTTP(w, r); second <- w }()
	deadline := time.After(time.Second)
	for g.Audit().Attempts < 2 {
		select {
		case <-deadline:
			t.Fatal("waiter did not enter")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	select {
	case w := <-second:
		if w.Code < 400 {
			t.Fatal(w.Code)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled waiter hung")
	}
	close(release)
	select {
	case w := <-first:
		if w.Code < 400 {
			t.Fatal("uncertain sibling erased", w.Code)
		}
	case <-time.After(time.Second):
		t.Fatal("send hung")
	}
	if f.calls.Load() != 1 || f.permits.Load() != 1 || !g.Audit().Uncertain {
		t.Fatal(g.Audit())
	}
}
func TestCodexCallGatePinnedTextStreamProjection(t *testing.T) {
	for _, mode := range []string{"delta", "crlf", "model_in_header", "reasoning", "assistant_history"} {
		t.Run(mode, func(t *testing.T) {
			f := newGateFixture(t)
			body, input := gateSSE(), gateBody
			switch mode {
			case "delta":
				body = richStream()
			case "crlf":
				body = strings.ReplaceAll(body, "\n", "\r\n")
			case "model_in_header":
				body = strings.ReplaceAll(body, `"model":"fixture-model",`, "")
			case "reasoning":
				body = reasoningStream()
			case "assistant_history":
				input = strings.Replace(input, `"input":[`, `"input":[{"type":"message","role":"assistant","id":"msg_old","status":"completed","content":[{"type":"output_text","text":"prior","annotations":[]}]},{"type":"reasoning","id":"rs_old","summary":[],"encrypted_content":"fixture-cipher"},`, 1)
			}
			f.config.Forwarder = gateForward(func(context.Context, stageplan.ExecutionTarget, []byte) (ForwardResponse, error) {
				f.calls.Add(1)
				return gateResponse(body), nil
			})
			g := newGate(t, f)
			w := send(g, f, input, "/responses")
			if w.Code != 200 || w.Body.String() != body || !gateHealthy(g, f) {
				t.Fatal(mode, w.Code, g.Audit())
			}
		})
	}
}
func richStream() string {
	body := gateSSE()
	i := strings.Index(body, "event: response.output_item.done")
	events := []string{
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_fixture","role":"assistant","status":"in_progress","content":[]}}`,
		`{"type":"response.content_part.added","output_index":0,"content_index":0,"item_id":"msg_fixture","part":{"type":"output_text","text":"","annotations":[]}}`,
		`{"type":"response.output_text.delta","output_index":0,"content_index":0,"item_id":"msg_fixture","delta":"fix"}`,
		`{"type":"response.output_text.delta","output_index":0,"content_index":0,"item_id":"msg_fixture","delta":"ture"}`,
		`{"type":"response.output_text.done","output_index":0,"content_index":0,"item_id":"msg_fixture","text":"fixture"}`,
		`{"type":"response.content_part.done","output_index":0,"content_index":0,"item_id":"msg_fixture","part":{"type":"output_text","text":"fixture","annotations":[]}}`,
	}
	var extra strings.Builder
	for _, s := range events {
		extra.WriteString("data: " + s + "\n\n")
	}
	return body[:i] + extra.String() + body[i:]
}
func reasoningStream() string {
	body := gateSSE()
	i := strings.Index(body, "event: response.output_item.done")
	item := `{"type":"reasoning","id":"rs_fixture","summary":[],"encrypted_content":"fixture-cipher","status":"completed"}`
	extra := `data: {"type":"response.output_item.done","output_index":0,"item":` + item + "}\n\n"
	body = body[:i] + extra + body[i:]
	body = strings.Replace(body, `"output_index":0,"sequence_number":1`, `"output_index":1,"sequence_number":1`, 1)
	return strings.Replace(body, `"output":[{`, `"output":[`+item+`,{`, 1)
}
func TestCodexCallGateRejectsContradictoryTextStream(t *testing.T) {
	for _, mode := range []string{"delta", "sequence", "output_index", "item_id", "part", "done_text", "tool_delta", "empty_usage", "negative_usage", "reasoning_summary"} {
		t.Run(mode, func(t *testing.T) {
			f := newGateFixture(t)
			body := richStream()
			switch mode {
			case "delta":
				body = strings.Replace(body, `"delta":"fix"`, `"delta":"other"`, 1)
			case "sequence":
				body = strings.Replace(body, `"sequence_number":2`, `"sequence_number":1`, 1)
			case "output_index":
				body = strings.Replace(body, `"output_index":0`, `"output_index":1`, 1)
			case "item_id":
				body = strings.Replace(body, `"item_id":"msg_fixture"`, `"item_id":"msg_other"`, 1)
			case "part":
				body = strings.Replace(body, `"part":{"type":"output_text"`, `"part":{"type":"refusal"`, 1)
			case "done_text":
				body = strings.Replace(body, `"text":"fixture"`, `"text":"other"`, 1)
			case "tool_delta":
				body = strings.Replace(body, "response.output_text.delta", "response.function_call_arguments.delta", 1)
			case "empty_usage":
				body = strings.Replace(body, `"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}`, `"usage":null`, 1)
			case "negative_usage":
				body = strings.Replace(body, `"input_tokens":3`, `"input_tokens":-3`, 1)
			case "reasoning_summary":
				body = reasoningStream()
				body = strings.Replace(body, `"summary":[]`, `"summary":[{"type":"summary_text","text":"other"}]`, 1)
			}
			f.config.Forwarder = gateForward(func(context.Context, stageplan.ExecutionTarget, []byte) (ForwardResponse, error) {
				return gateResponse(body), nil
			})
			g := newGate(t, f)
			w := send(g, f, gateBody, "/responses")
			if w.Code < 400 || strings.Contains(w.Body.String(), "data:") {
				t.Fatal(mode, w.Code)
			}
		})
	}
}
func TestCodexCallGateRejectsInvalidUTF8AnywhereInStream(t *testing.T) {
	f := newGateFixture(t)
	body := ": " + string([]byte{0xff}) + "\n\n" + gateSSE()
	f.config.Forwarder = gateForward(func(context.Context, stageplan.ExecutionTarget, []byte) (ForwardResponse, error) {
		return gateResponse(body), nil
	})
	w := send(newGate(t, f), f, gateBody, "/responses")
	if w.Code < 400 {
		t.Fatal("invalid UTF8 escaped through comment", w.Code)
	}
}

type secretBody struct{ Secret string }

func (s *secretBody) Read([]byte) (int, error) { return 0, io.EOF }
func (s *secretBody) Close() error             { return nil }
func TestCodexForwardResponseCannotSerializePrivatePayload(t *testing.T) {
	r := ForwardResponse{Body: &secretBody{Secret: "fixture-private-secret"}, PrivateMarkers: [][]byte{[]byte("fixture-private-secret")}}
	b, e := json.Marshal(r)
	if e != nil || strings.Contains(string(b), "fixture-private-secret") || strings.Contains(fmt.Sprintf("%#v", r), "fixture-private-secret") {
		t.Fatal("private payload escaped DTO/format")
	}
}
func TestCodexCallGateRejectsTextDeltaAttachedToReasoning(t *testing.T) {
	f := newGateFixture(t)
	body := reasoningStream()
	i := strings.Index(body, "data: {\"type\":\"response.output_item.done\",\"output_index\":0")
	extra := "data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"reasoning\",\"id\":\"rs_fixture\",\"summary\":[]}}\n\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"content_index\":0,\"item_id\":\"rs_fixture\",\"delta\":\"unmatched output\"}\n\n"
	body = body[:i] + extra + body[i:]
	f.config.Forwarder = gateForward(func(context.Context, stageplan.ExecutionTarget, []byte) (ForwardResponse, error) {
		return gateResponse(body), nil
	})
	w := send(newGate(t, f), f, gateBody, "/responses")
	if w.Code < 400 {
		t.Fatal("text attached to reasoning escaped", w.Code)
	}
}
func TestCodexCallGateAcceptsPinnedObservationMetadataOnly(t *testing.T) {
	f := newGateFixture(t)
	body := strings.Replace(gateBody, `"tools":[]`, `"client_metadata":{"root_turn_id":"fixture-turn","session_id":"fixture-session","thread_id":"fixture-thread","turn_id":"fixture-turn","x-codex-installation-id":"fixture-install","x-codex-turn-metadata":"{\"turn_id\":\"fixture-turn\"}","x-codex-window-id":"fixture-window"},"tools":[]`, 1)
	w := send(newGate(t, f), f, body, "/responses")
	if w.Code != 200 || f.calls.Load() != 1 || f.permits.Load() != 1 {
		t.Fatal("pinned metadata refused", w.Code)
	}
	for _, mode := range []string{"unknown", "null", "type", "oversize", "duplicate", "alias"} {
		t.Run(mode, func(t *testing.T) {
			f := newGateFixture(t)
			bad := body
			switch mode {
			case "unknown":
				bad = strings.Replace(bad, `"thread_id":`, `"account":`, 1)
			case "null":
				bad = strings.Replace(bad, `"thread_id":"fixture-thread"`, `"thread_id":null`, 1)
			case "type":
				bad = strings.Replace(bad, `"thread_id":"fixture-thread"`, `"thread_id":{}`, 1)
			case "oversize":
				bad = strings.Replace(bad, "fixture-thread", strings.Repeat("x", 4097), 1)
			case "duplicate":
				bad = strings.Replace(bad, `"thread_id":`, `"thread_id":"other","thread_id":`, 1)
			case "alias":
				bad = strings.Replace(bad, `"thread_id":`, `"THREAD_ID":`, 1)
			}
			w := send(newGate(t, f), f, bad, "/responses")
			if w.Code < 400 || f.calls.Load() != 0 || f.permits.Load() != 0 {
				t.Fatal(mode, w.Code)
			}
		})
	}
}

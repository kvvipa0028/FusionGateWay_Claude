package grok

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/stageplan"
)

type fakeForwarder func(context.Context, stageplan.ExecutionTarget, []byte) (ForwardResponse, error)

func (f fakeForwarder) Send(c context.Context, t stageplan.ExecutionTarget, b []byte) (ForwardResponse, error) {
	return f(c, t, b)
}

const gateBody = `{"model":"fixture-model","stream":true,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"fixture"}],"tools":[{"type":"function","function":{"name":"read_file","parameters":{"type":"object"}}}]}`
const gateSSE = "data: {\"id\":\"fixture-response\",\"object\":\"chat.completion.chunk\",\"created\":1780000000,\"model\":\"fixture-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"fixture\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"fixture-response\",\"object\":\"chat.completion.chunk\",\"created\":1780000000,\"model\":\"fixture-model\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\ndata: [DONE]\n\n"

type gateFixture struct {
	config         CallGateConfig
	manager        *policy.Manager
	token          string
	current        atomic.Bool
	calls, permits atomic.Int64
}

func newGateFixture(t *testing.T) *gateFixture {
	t.Helper()
	_, b, _ := fixture(t)
	f := &gateFixture{}
	f.current.Store(true)
	target := stageplan.ExecutionTarget{Route: stageplan.RouteRef{ID: "fixture-native", Revision: 1}, RequestedModel: b.Model, ResolvedModel: b.Model, Account: "fixture-account", Workspace: "fixture-workspace", CredentialIdentity: "fixture-identity", RuntimeVersion: CLIVersion, BillingPath: "subscription", LockEnforcement: stageplan.ControlledCalls, Effort: stageplan.FrozenEffort{RequestedMode: stageplan.EffortNone}}
	c := policy.Claims{TaskID: "fixture-task", RunID: b.RunID, Role: b.Role, Attempt: 1, PlanRevision: 1, Generation: b.Generation, ProjectID: "fixture-project", Audience: policy.ModelAudience}
	f.manager = policy.NewManager("fixture-management", func(got policy.Claims) bool {
		if got.Audience == policy.EventsAudience {
			got.Audience = policy.ModelAudience
		}
		return f.current.Load() && got == c
	}, nil)
	var e error
	f.token, e = f.manager.Issue(c, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	f.config = CallGateConfig{Binding: GateBinding{Observer: b, Target: target}, Manager: f.manager, Claims: c, Current: func(got GateBinding) bool {
		return f.current.Load() && got.Target.Account == target.Account && got.Target.CredentialIdentity == target.CredentialIdentity
	}, Permit: func(context.Context, policy.Claims, stageplan.ExecutionTarget, int) error {
		f.permits.Add(1)
		return nil
	}, Forwarder: fakeForwarder(func(ctx context.Context, got stageplan.ExecutionTarget, raw []byte) (ForwardResponse, error) {
		f.calls.Add(1)
		if got.Account != target.Account || got.RequestedModel != target.RequestedModel || strings.Contains(string(raw), f.token) {
			t.Error("authority or model escaped")
		}
		return ForwardResponse{StatusCode: 200, ContentType: "text/event-stream", Body: io.NopCloser(strings.NewReader(gateSSE))}, nil
	})}
	return f
}
func newGate(t *testing.T, f *gateFixture) *CallGate {
	t.Helper()
	g, e := NewCallGate(f.config)
	if e != nil {
		t.Fatal(e)
	}
	return g
}
func send(t *testing.T, g *CallGate, f *gateFixture, body, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+f.token)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Cookie", "fixture-cookie")
	r.Header.Set("X-Magpie-Account", "other")
	r.Header.Set("X-Fusion-Override", "other")
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	return w
}
func TestGrokCallGateAccountsForMainAndTitleIndividually(t *testing.T) {
	f := newGateFixture(t)
	g := newGate(t, f)
	title := strings.Replace(gateBody, "read_file", "session_title", 1)
	for _, body := range []string{title, gateBody} {
		w := send(t, g, f, body, "/v1/chat/completions")
		if w.Code != 200 || w.Body.String() != gateSSE || w.Header().Get("Set-Cookie") != "" {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	a := g.Audit()
	if f.calls.Load() != 2 || f.permits.Load() != 2 || a.Forwarded != 2 || a.Completed != 2 || !healthy(g, f) {
		t.Fatal(a)
	}
}
func TestGrokCallGateDeniedNativeInputsNeverSpendOrForward(t *testing.T) {
	for _, mode := range []string{"model", "alias", "duplicate", "unknown", "nonstream", "tool", "subagent", "backend_tool", "choice", "effort", "content", "role", "path", "query", "absolute", "trailing", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			f := newGateFixture(t)
			g := newGate(t, f)
			body, path := gateBody, "/v1/chat/completions"
			switch mode {
			case "model":
				body = strings.Replace(body, "fixture-model", "other", 1)
			case "alias":
				body = strings.Replace(body, `"model"`, `"MODEL"`, 1)
			case "duplicate":
				body = strings.Replace(body, `"model":"fixture-model"`, `"model":"fixture-model","MODEL":"other"`, 1)
			case "unknown":
				body = strings.Replace(body, `"stream_options"`, `"route_override"`, 1)
			case "nonstream":
				body = strings.Replace(body, `"stream":true`, `"stream":false`, 1)
			case "tool":
				body = strings.Replace(body, "read_file", "search_replace", 1)
			case "subagent":
				body = strings.Replace(body, "read_file", "spawn_subagent", 1)
			case "backend_tool":
				body = strings.Replace(body, `"type":"function"`, `"type":"web_search"`, 1)
			case "choice":
				body = strings.Replace(body, `"stream":true`, `"stream":true,"tool_choice":{"type":"function","function":{"name":"spawn_subagent"}}`, 1)
			case "effort":
				body = strings.Replace(body, `"stream":true`, `"stream":true,"reasoning_effort":"high"`, 1)
			case "content":
				body = strings.Replace(body, `"content":"fixture"`, `"content":[{"type":"image_url","image_url":{"url":"https://other.invalid"}}]`, 1)
			case "role":
				body = strings.Replace(body, `"role":"user"`, `"role":"tool"`, 1)
			case "path":
				path += "/extra"
			case "query":
				path += "?model=other"
			case "absolute":
				path = "https://other.invalid/v1/chat/completions"
			case "trailing":
				body += " {}"
			case "oversize":
				body = strings.Repeat(" ", 1<<20) + body
			}
			w := send(t, g, f, body, path)
			if w.Code < 400 || f.calls.Load() != 0 || f.permits.Load() != 0 || !g.Audit().Uncertain || healthy(g, f) {
				t.Fatal(mode, w.Code, g.Audit())
			}
			if w = send(t, g, f, gateBody, "/v1/chat/completions"); w.Code < 400 || f.calls.Load() != 0 {
				t.Fatal("auxiliary failure erased", w.Code)
			}
		})
	}
}
func TestGrokCallGateAuthorizationDoesNotPoisonValidRun(t *testing.T) {
	for _, mode := range []string{"token", "management", "origin", "events", "duplicate_header"} {
		t.Run(mode, func(t *testing.T) {
			f := newGateFixture(t)
			g := newGate(t, f)
			r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(gateBody))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Authorization", "Bearer "+f.token)
			switch mode {
			case "token":
				r.Header.Set("Authorization", "Bearer invalid")
			case "management":
				r.Header.Set("Authorization", "Bearer fixture-management")
			case "origin":
				r.Header.Set("Origin", "https://other.invalid")
			case "events":
				c := f.config.Claims
				c.Audience = policy.EventsAudience
				token, e := f.manager.Issue(c, time.Minute)
				if e != nil {
					t.Fatal(e)
				}
				r.Header.Set("Authorization", "Bearer "+token)
			case "duplicate_header":
				r.Header.Add("Authorization", "Bearer "+f.token)
			}
			w := httptest.NewRecorder()
			g.ServeHTTP(w, r)
			if w.Code < 400 || f.calls.Load() != 0 || f.permits.Load() != 0 || g.Audit().Uncertain {
				t.Fatal(w.Code, g.Audit())
			}
			if w = send(t, g, f, gateBody, "/v1/chat/completions"); w.Code != 200 {
				t.Fatal(w.Code)
			}
		})
	}
}
func TestGrokCallGateRechecksCurrentAfterPermitAndNeverRefunds(t *testing.T) {
	for _, mode := range []string{"revoked", "identity", "budget", "transport", "mutate"} {
		t.Run(mode, func(t *testing.T) {
			f := newGateFixture(t)
			f.config.Permit = func(ctx context.Context, c policy.Claims, target stageplan.ExecutionTarget, n int) error {
				f.permits.Add(1)
				switch mode {
				case "revoked":
					f.manager.RevokeRun(c.RunID)
				case "identity":
					f.current.Store(false)
				case "budget":
					return errors.New("private-budget-detail")
				case "mutate":
					target.Account = "other"
					target.RequestedModel = "other"
				}
				return nil
			}
			if mode == "transport" {
				f.config.Forwarder = fakeForwarder(func(context.Context, stageplan.ExecutionTarget, []byte) (ForwardResponse, error) {
					f.calls.Add(1)
					return ForwardResponse{}, errors.New("private-upstream-detail")
				})
			}
			g := newGate(t, f)
			w := send(t, g, f, gateBody, "/v1/chat/completions")
			want := int64(0)
			if mode == "transport" || mode == "mutate" {
				want = 1
			}
			if f.calls.Load() != want || f.permits.Load() != 1 || strings.Contains(w.Body.String(), "private-") {
				t.Fatal(mode, w.Code, g.Audit())
			}
			if mode == "mutate" {
				if w.Code != 200 {
					t.Fatal(w.Code)
				}
			} else if w.Code < 400 || healthy(g, f) {
				t.Fatal(mode, w.Code)
			}
		})
	}
}
func TestGrokCallGateUnsafeUpstreamNeverReleasesPartialBytes(t *testing.T) {
	for _, mode := range []string{"model", "id", "missing_done", "missing_stop", "duplicate_done", "after_done", "duplicate_key", "tool_call", "encoding", "status", "marker", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			f := newGateFixture(t)
			f.config.Forwarder = fakeForwarder(func(context.Context, stageplan.ExecutionTarget, []byte) (ForwardResponse, error) {
				f.calls.Add(1)
				body := gateSSE
				r := ForwardResponse{StatusCode: 200, ContentType: "text/event-stream"}
				switch mode {
				case "model":
					body = strings.Replace(body, `"model":"fixture-model"`, `"model":"other"`, 1)
				case "id":
					body = strings.Replace(body, `"id":"fixture-response"`, `"id":"other"`, 1)
				case "missing_done":
					body = strings.Replace(body, "data: [DONE]\n\n", "", 1)
				case "missing_stop":
					body = strings.Replace(body, `"finish_reason":"stop"`, `"finish_reason":null`, 1)
				case "duplicate_done":
					body += "data: [DONE]\n\n"
				case "after_done":
					body += gateSSE
				case "duplicate_key":
					body = strings.Replace(body, `"model":"fixture-model"`, `"model":"fixture-model","MODEL":"other"`, 1)
				case "tool_call":
					body = strings.Replace(body, `"content":"fixture"`, `"tool_calls":[{"id":"x","function":{"name":"read_file"}}]`, 1)
				case "encoding":
					r.ContentType = "application/json"
				case "status":
					r.StatusCode = 302
				case "marker":
					r.PrivateMarkers = [][]byte{[]byte("fixture-response")}
				case "oversize":
					body = strings.Repeat(" ", 8<<20) + body
				}
				r.Body = io.NopCloser(strings.NewReader(body))
				return r, nil
			})
			g := newGate(t, f)
			w := send(t, g, f, gateBody, "/v1/chat/completions")
			if w.Code < 400 || strings.Contains(w.Body.String(), "fixture") || !g.Audit().Uncertain || g.Audit().Completed != 0 || f.permits.Load() != 1 || f.calls.Load() != 1 {
				t.Fatal(mode, w.Code, g.Audit())
			}
		})
	}
}
func TestGrokCallGateRetryNeedsAnotherPermit(t *testing.T) {
	f := newGateFixture(t)
	f.config.Forwarder = fakeForwarder(func(context.Context, stageplan.ExecutionTarget, []byte) (ForwardResponse, error) {
		n := f.calls.Add(1)
		if n == 1 {
			return ForwardResponse{StatusCode: 429, ContentType: "text/plain", Body: io.NopCloser(strings.NewReader("private-upstream"))}, nil
		}
		return ForwardResponse{StatusCode: 200, ContentType: "text/event-stream", Body: io.NopCloser(strings.NewReader(gateSSE))}, nil
	})
	g := newGate(t, f)
	w := send(t, g, f, gateBody, "/v1/chat/completions")
	if w.Code != 429 || strings.Contains(w.Body.String(), "private") || healthy(g, f) {
		t.Fatal(w.Code, g.Audit())
	}
	w = send(t, g, f, gateBody, "/v1/chat/completions")
	if w.Code != 200 || f.calls.Load() != 2 || f.permits.Load() != 2 || g.Audit().Retryable != 1 || g.Audit().Completed != 1 || !healthy(g, f) {
		t.Fatal(w.Code, g.Audit())
	}
}

func healthy(g *CallGate, f *gateFixture) bool {
	ctx, e := f.manager.WithStage(context.Background(), f.token, f.config.Claims)
	return e == nil && g.Healthy(ctx)
}

func TestGrokGateEffortAndCopiesRemainFrozen(t *testing.T) {
	for _, mode := range []string{"explicit", "default"} {
		t.Run(mode, func(t *testing.T) {
			f := newGateFixture(t)
			value := "high"
			f.config.Binding.Target.Effort = stageplan.FrozenEffort{RequestedMode: stageplan.EffortSelectionMode(mode), Value: &value}
			g := newGate(t, f)
			value = "low"
			f.config.Binding.Observer.Tools[0] = "spawn_subagent"
			f.config.Binding.Target.Account = "other"
			body := strings.Replace(gateBody, `"stream":true`, `"stream":true,"reasoning_effort":"high"`, 1)
			w := send(t, g, f, body, "/v1/chat/completions")
			if w.Code != 200 || g.Healthy(context.Background()) || !healthy(g, f) {
				t.Fatal(w.Code, g.Audit())
			}
			w = send(t, g, f, strings.Replace(body, "high", "low", 1), "/v1/chat/completions")
			if w.Code < 400 || f.calls.Load() != 1 {
				t.Fatal(w.Code, g.Audit())
			}
		})
	}
}

func TestGrokGateRejectsUntrustedConstructorControls(t *testing.T) {
	for _, mode := range []string{"nil_manager", "nil_permit", "nil_forwarder", "nil_current", "claims", "billing", "model", "runtime", "plugin", "primary_only", "effort", "revision", "hash"} {
		t.Run(mode, func(t *testing.T) {
			f := newGateFixture(t)
			c := f.config
			switch mode {
			case "nil_manager":
				c.Manager = nil
			case "nil_permit":
				c.Permit = nil
			case "nil_forwarder":
				c.Forwarder = nil
			case "nil_current":
				c.Current = nil
			case "claims":
				c.Claims.Generation++
			case "billing":
				c.Binding.Target.BillingPath = "api"
			case "model":
				c.Binding.Target.ResolvedModel = "other"
			case "runtime":
				c.Binding.Target.RuntimeVersion = "latest"
			case "plugin":
				v := "plugin"
				c.Binding.Target.PluginVersion = &v
			case "primary_only":
				c.Binding.Target.LockEnforcement = stageplan.PrimaryOnly
			case "effort":
				v := "high"
				c.Binding.Target.Effort.Value = &v
			case "revision":
				c.Binding.Target.Route.Revision = 0
			case "hash":
				c.Binding.Observer.ExecutableSHA256 = ""
			}
			if _, e := NewCallGate(c); e == nil {
				t.Fatal(mode)
			}
		})
	}
}

func TestGrokGateGrantReflectionAndLateRevocationFailClosed(t *testing.T) {
	for _, mode := range []string{"request_grant", "response_grant", "response_revoke", "encoding", "choice_extension"} {
		t.Run(mode, func(t *testing.T) {
			f := newGateFixture(t)
			body := gateBody
			if mode == "request_grant" {
				body = strings.Replace(body, `"content":"fixture"`, `"content":"`+f.token+`"`, 1)
			} else {
				f.config.Forwarder = fakeForwarder(func(context.Context, stageplan.ExecutionTarget, []byte) (ForwardResponse, error) {
					f.calls.Add(1)
					s := gateSSE
					r := ForwardResponse{StatusCode: 200, ContentType: "text/event-stream"}
					switch mode {
					case "response_grant":
						s = strings.Replace(s, `"content":"fixture"`, `"content":"`+f.token+`"`, 1)
					case "response_revoke":
						f.manager.RevokeRun(f.config.Claims.RunID)
					case "encoding":
						r.ContentEncoding = "gzip"
					case "choice_extension":
						s = strings.Replace(s, `"finish_reason":"stop"`, `"finish_reason":"stop","message":{"tool_calls":[]}`, 1)
					}
					r.Body = io.NopCloser(strings.NewReader(s))
					return r, nil
				})
			}
			g := newGate(t, f)
			w := send(t, g, f, body, "/v1/chat/completions")
			if w.Code < 400 || strings.Contains(w.Body.String(), f.token) || g.Audit().Completed != 0 || !g.Audit().Uncertain {
				t.Fatal(mode, w.Code, g.Audit())
			}
		})
	}
}

func TestGrokGateSerializesConcurrentAuxiliaryCalls(t *testing.T) {
	f := newGateFixture(t)
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	defer close(release)
	f.config.Forwarder = fakeForwarder(func(ctx context.Context, _ stageplan.ExecutionTarget, _ []byte) (ForwardResponse, error) {
		f.calls.Add(1)
		entered <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return ForwardResponse{}, ctx.Err()
		}
		return ForwardResponse{StatusCode: 200, ContentType: "text/event-stream", Body: io.NopCloser(strings.NewReader(gateSSE))}, nil
	})
	g := newGate(t, f)
	done := make(chan *httptest.ResponseRecorder, 2)
	go func() { done <- send(t, g, f, gateBody, "/v1/chat/completions") }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first call did not enter")
	}
	go func() {
		done <- send(t, g, f, strings.Replace(gateBody, "read_file", "session_title", 1), "/v1/chat/completions")
	}()
	deadline := time.Now().Add(time.Second)
	for g.Audit().Attempts < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if g.Audit().Attempts != 2 || f.permits.Load() != 1 || f.calls.Load() != 1 || healthy(g, f) {
		t.Fatal("concurrent Native exit bypassed Manager", g.Audit())
	}
	release <- struct{}{}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("second call not serialized")
	}
	release <- struct{}{}
	for i := 0; i < 2; i++ {
		select {
		case w := <-done:
			if w.Code != 200 {
				t.Fatal(w.Code)
			}
		case <-time.After(time.Second):
			t.Fatal("request did not finish")
		}
	}
	if !healthy(g, f) || f.permits.Load() != 2 {
		t.Fatal(g.Audit())
	}
}

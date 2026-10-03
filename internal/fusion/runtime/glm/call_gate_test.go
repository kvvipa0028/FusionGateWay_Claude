package glm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

type fixtureRoundTrip func(*http.Request) (*http.Response, error)

func (f fixtureRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

const gateBody = `{"model":"glm-5.3","messages":[{"role":"user","content":"fixture"}],"max_tokens":32,"stream":true,"output_config":{"effort":"high"},"thinking":{"type":"adaptive","display":"updates"},"tools":[]}`
const gateStream = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"model\":\"glm-5.3\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

type gateFixture struct {
	config  CallGateConfig
	manager *policy.Manager
	token   string
	current atomic.Bool
	calls   atomic.Int64
	permits atomic.Int64
}

func newGateFixture(t *testing.T) *gateFixture {
	t.Helper()
	_, b, _ := fixtureSession(t)
	b.Target.Effort = stageplan.FrozenEffort{RequestedMode: stageplan.EffortExplicit, Value: ptr("high")}
	f := &gateFixture{}
	f.current.Store(true)
	c := policy.Claims{TaskID: "fixture-task", RunID: b.RunID, Role: b.Role, Attempt: 1, PlanRevision: 1, Generation: b.Generation, ProjectID: "fixture-project", Audience: policy.ModelAudience}
	f.manager = policy.NewManager("fixture-management", func(got policy.Claims) bool { return f.current.Load() && got == c }, nil)
	var e error
	f.token, e = f.manager.Issue(c, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	f.config = CallGateConfig{Binding: b, Manager: f.manager, Claims: c, APIKey: "fixture-upstream-key", Current: func(got Binding) bool { return f.current.Load() && got.Target.Account == b.Target.Account }, Permit: func(context.Context, policy.Claims, stageplan.ExecutionTarget, int) error {
		f.permits.Add(1)
		return nil
	}, Transport: fixtureRoundTrip(func(r *http.Request) (*http.Response, error) {
		f.calls.Add(1)
		if r.URL.String() != Endpoint+"/v1/messages?beta=true" || r.Header.Get("Authorization") != "Bearer fixture-upstream-key" || r.Header.Get("X-Api-Key") != "fixture-upstream-key" || r.Header.Get("X-Magpie-Account") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("X-Fusion-Override") != "" || r.GetBody != nil {
			t.Error("untrusted headers, authority, endpoint or replay reached upstream")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}, "Set-Cookie": []string{"fixture-cookie"}}, Body: io.NopCloser(strings.NewReader(gateStream))}, nil
	})}
	return f
}
func ptr(s string) *string { return &s }
func (f *gateFixture) send(t *testing.T, body, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	g, e := NewCallGate(f.config)
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("X-Api-Key", token)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Magpie-Account", "fixture-other")
	r.Header.Set("Cookie", "fixture-cookie")
	r.Header.Set("X-Fusion-Override", "fixture-other")
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	return w
}
func TestCallGateFrozenRequestAndControlledCredentialBoundary(t *testing.T) {
	f := newGateFixture(t)
	w := f.send(t, gateBody, "/api/anthropic/v1/messages?beta=true", f.token)
	if w.Code != 200 || w.Body.String() != gateStream || w.Header().Get("Set-Cookie") != "" || f.calls.Load() != 1 || f.permits.Load() != 1 {
		t.Fatal("controlled call failed", w.Code, f.calls.Load(), f.permits.Load())
	}
	if strings.Contains(w.Body.String(), "fixture-upstream-key") || strings.Contains(w.Body.String(), f.token) {
		t.Fatal("credential reflected")
	}
}
func TestCallGateDeniedInputsNeverSpendOrForward(t *testing.T) {
	for _, mode := range []string{"model", "alias", "duplicate", "unknown", "stream", "effort", "tool", "subagent", "tool_choice", "url", "query", "trailing", "oversize", "no_auth", "origin"} {
		t.Run(mode, func(t *testing.T) {
			f := newGateFixture(t)
			body, path, token := gateBody, "/api/anthropic/v1/messages?beta=true", f.token
			switch mode {
			case "model":
				body = strings.Replace(body, "glm-5.3", "glm-other", 1)
			case "alias":
				body = strings.Replace(body, `"model"`, `"MODEL"`, 1)
			case "duplicate":
				body = strings.Replace(body, `"model":"glm-5.3"`, `"model":"glm-5.3","MODEL":"glm-other"`, 1)
			case "unknown":
				body = strings.Replace(body, `"max_tokens"`, `"route_override"`, 1)
			case "stream":
				body = strings.Replace(body, `"stream":true`, `"stream":false`, 1)
			case "effort":
				body = strings.Replace(body, `"high"`, `"low"`, 1)
			case "tool":
				body = strings.Replace(body, `"tools":[]`, `"tools":[{"name":"Bash"}]`, 1)
			case "subagent":
				body = strings.Replace(body, `"tools":[]`, `"tools":[{"name":"Agent"}]`, 1)
			case "tool_choice":
				body = strings.Replace(body, `"tools":[]`, `"tools":[],"tool_choice":{"type":"tool","name":"Agent"}`, 1)
			case "url":
				path = "/api/anthropic/v1/messages/count_tokens"
			case "query":
				path += "&token=" + f.token
			case "trailing":
				body += " {}"
			case "oversize":
				body = strings.Repeat(" ", 1<<20) + body
			case "no_auth":
				token = "fixture-invalid"
			case "origin":
				g, e := NewCallGate(f.config)
				if e != nil {
					t.Fatal(e)
				}
				r := httptest.NewRequest("POST", path, strings.NewReader(body))
				r.Header.Set("Authorization", "Bearer "+token)
				r.Header.Set("Origin", "https://fixture.invalid")
				w := httptest.NewRecorder()
				g.ServeHTTP(w, r)
				if w.Code < 400 || f.calls.Load() != 0 || f.permits.Load() != 0 {
					t.Fatal("origin forwarded")
				}
				return
			}
			w := f.send(t, body, path, token)
			if w.Code < 400 || f.calls.Load() != 0 || f.permits.Load() != 0 {
				t.Fatal("unsafe input reached upstream", w.Code)
			}
		})
	}
}
func TestCallGateRechecksAuthorityAfterPermitAndNeverRefundsFailure(t *testing.T) {
	for _, mode := range []string{"revoked", "identity", "cancelled", "budget", "transport"} {
		t.Run(mode, func(t *testing.T) {
			f := newGateFixture(t)
			f.config.Permit = func(ctx context.Context, c policy.Claims, target stageplan.ExecutionTarget, n int) error {
				f.permits.Add(1)
				switch mode {
				case "revoked":
					f.manager.RevokeRun(c.RunID)
				case "identity":
					f.current.Store(false)
				case "cancelled":
					return context.Canceled
				case "budget":
					return errors.New("fixture-budget-secret")
				}
				return nil
			}
			if mode == "transport" {
				f.config.Transport = fixtureRoundTrip(func(*http.Request) (*http.Response, error) {
					f.calls.Add(1)
					return nil, errors.New("fixture-upstream-key")
				})
			}
			w := f.send(t, gateBody, "/api/anthropic/v1/messages?beta=true", f.token)
			want := int64(0)
			if mode == "transport" {
				want = 1
			}
			if w.Code < 400 || f.permits.Load() != 1 || f.calls.Load() != want || strings.Contains(w.Body.String(), "fixture-") {
				t.Fatal("gate recheck, single send or error sanitization failed", w.Code, f.calls.Load())
			}
		})
	}
}
func TestCallGateRejectsUntrustedUpstreamWithoutDeliveringStream(t *testing.T) {
	for _, mode := range []string{"model", "alias", "duplicate", "missing_stop", "extra_message", "error", "redirect", "content_type", "too_large"} {
		t.Run(mode, func(t *testing.T) {
			f := newGateFixture(t)
			raw, status, content := gateStream, 200, "text/event-stream"
			switch mode {
			case "model":
				raw = strings.Replace(raw, "glm-5.3", "glm-other", 1)
			case "alias":
				raw = strings.Replace(raw, `"model"`, `"MODEL"`, 1)
			case "duplicate":
				raw = strings.Replace(raw, `"model":"glm-5.3"`, `"model":"glm-5.3","MODEL":"glm-other"`, 1)
			case "missing_stop":
				raw = strings.Split(raw, "event: message_stop")[0]
			case "extra_message":
				raw += gateStream
			case "error":
				raw = "event: error\ndata: {\"type\":\"error\",\"error\":\"fixture-upstream-key\"}\n\n"
			case "redirect":
				status = 302
			case "content_type":
				content = "application/json"
			case "too_large":
				raw = strings.Repeat("x", (8<<20)+1)
			}
			f.config.Transport = fixtureRoundTrip(func(*http.Request) (*http.Response, error) {
				f.calls.Add(1)
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{content}, "Location": []string{"https://fixture.invalid/"}}, Body: io.NopCloser(strings.NewReader(raw))}, nil
			})
			w := f.send(t, gateBody, "/api/anthropic/v1/messages?beta=true", f.token)
			if w.Code < 400 || strings.Contains(w.Body.String(), "glm-") || strings.Contains(w.Body.String(), "fixture-upstream-key") || f.calls.Load() != 1 {
				t.Fatal("upstream drift, redirect or raw content delivered", w.Code)
			}
		})
	}
}
func TestCallGateRequiresTrustedDependenciesAndKnownEffort(t *testing.T) {
	for _, mode := range []string{"manager", "permit", "current", "transport", "key", "claims", "effort", "region"} {
		f := newGateFixture(t)
		switch mode {
		case "manager":
			f.config.Manager = nil
		case "permit":
			f.config.Permit = nil
		case "current":
			f.config.Current = nil
		case "transport":
			f.config.Transport = nil
		case "key":
			f.config.APIKey = ""
		case "claims":
			f.config.Claims.Generation++
		case "effort":
			f.config.Binding.Target.Effort.Value = ptr("unknown")
		case "region":
			f.config.Binding.Region = "global"
		}
		if _, e := NewCallGate(f.config); e == nil {
			t.Fatal("missing trusted contract accepted", mode)
		}
	}
}

func TestCallGateSharedRunLeaseAcrossInstancesAndCurrentIssuer(t *testing.T) {
	f := newGateFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	f.config.Transport = fixtureRoundTrip(func(*http.Request) (*http.Response, error) {
		f.calls.Add(1)
		close(entered)
		<-release
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(gateStream))}, nil
	})
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- f.send(t, gateBody, "/api/anthropic/v1/messages?beta=true", f.token) }()
	<-entered
	second := f.send(t, gateBody, "/api/anthropic/v1/messages?beta=true", f.token)
	ctx, e := f.manager.WithStage(context.Background(), f.token, f.config.Claims)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.manager.BeginModelCall(ctx, f.config.Claims); !errors.Is(e, policy.ErrDispatchBusy) {
		t.Error("other controller exit escaped same run lease")
	}
	other := policy.NewManager("fixture-other-manager", func(policy.Claims) bool { return true }, nil)
	if other.ModelCurrent(ctx, f.config.Claims) {
		t.Error("foreign issuer context accepted")
	}
	f.manager.RevokeRun(f.config.Claims.RunID)
	close(release)
	first := <-done
	if second.Code != 409 || first.Code != 502 || f.calls.Load() != 1 || f.permits.Load() != 1 {
		t.Fatal("concurrent/revoked call delivered", second.Code, first.Code)
	}
}

func TestCallGateNativeRetriesSpendPersistentSharedBudget(t *testing.T) {
	f := newGateFixture(t)
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(root, 0700); e != nil {
		t.Fatal(e)
	}
	s, e := store.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	route := stageplan.Route{ID: "fixture-glm-route", Revision: 1, Model: "glm-5.3", Account: "fixture-account", Workspace: "fixture-workspace", CredentialIdentity: "fixture-credential", RuntimeVersion: CLIVersion, BillingPath: "coding_plan", BillingKnown: true, Admitted: true, Efforts: []string{"high"}, DefaultEffort: ptr("high"), Capabilities: []string{"text"}, LockEnforcement: stageplan.ControlledCalls}
	plan, e := stageplan.Compile(1, []stageplan.Role{stageplan.Design}, stageplan.Layer{Roles: map[stageplan.Role]stageplan.Binding{stageplan.Design: {Mode: stageplan.Locked, Route: &stageplan.RouteRef{ID: route.ID, Revision: 1}, Model: route.Model, Effort: &stageplan.EffortSelection{Mode: stageplan.EffortExplicit, Value: "high"}}}}, stageplan.Layer{}, stageplan.Layer{}, []stageplan.Route{route})
	if e != nil {
		t.Fatal(e)
	}
	task, e := s.Create("fixture-gate-budget", store.CreateRequest{ProjectID: "fixture-project", Goal: "fixture", Plan: plan})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConfigureBudget(task.ID, store.Budget{MaxCalls: 2}); e != nil {
		t.Fatal(e)
	}
	target := *plan.Bindings[stageplan.Design].Target
	run, e := s.StartReserved(store.StartRequest{TaskID: task.ID, Role: stageplan.Design, PlanRevision: 1, Owner: "fixture-owner", TTL: time.Minute, Target: target}, store.ReservationRequest{PoolKey: "fixture-pool", GlobalLimit: 2, AdmissionHash: strings.Repeat("a", 64)})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConfirmStarted(run.ID, run.Generation, run.Owner, "fixture-native"); e != nil {
		t.Fatal(e)
	}
	c := policy.Claims{TaskID: task.ID, RunID: run.ID, Role: run.Role, Attempt: run.Attempt, PlanRevision: run.PlanRevision, Generation: run.Generation, ProjectID: task.ProjectID, Audience: policy.ModelAudience}
	f.config.Binding.RunID, f.config.Binding.Generation, f.config.Binding.Target = run.ID, run.Generation, target
	f.config.Claims = c
	f.config.Manager = policy.NewManager("fixture-budget-manager", func(got policy.Claims) bool {
		r, e := s.CheckActive(got.RunID, got.Generation)
		return e == nil && r.State == "running" && got == c
	}, nil)
	f.token, e = f.config.Manager.Issue(c, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	f.config.Permit = func(_ context.Context, got policy.Claims, _ stageplan.ExecutionTarget, _ int) error {
		f.permits.Add(1)
		return s.ReserveCall(got.RunID, got.Generation)
	}
	g, e := NewCallGate(f.config)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(g)
	defer server.Close()
	for i, want := range []int{200, 200, 403} {
		r, e := http.NewRequest("POST", server.URL+"/api/anthropic/v1/messages?beta=true", strings.NewReader(gateBody))
		if e != nil {
			t.Fatal(e)
		}
		r.Header.Set("Authorization", "Bearer "+f.token)
		r.Header.Set("Content-Type", "application/json")
		response, e := server.Client().Do(r)
		if e != nil {
			t.Fatal(e)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != want {
			t.Fatal("wrong retry gate", i, response.StatusCode)
		}
	}
	budget, e := s.Budget(task.ID)
	if e != nil || budget.UsedCalls != 2 || f.calls.Load() != 2 || f.permits.Load() != 3 {
		t.Fatal("retry bypassed persisted task budget", budget.UsedCalls, f.calls.Load(), e)
	}
}

func TestCallGateNoneEffortRejectsNativeDefaultAndCopiesBinding(t *testing.T) {
	f := newGateFixture(t)
	f.config.Binding.Target.Effort = stageplan.FrozenEffort{RequestedMode: stageplan.EffortNone}
	if w := f.send(t, gateBody, "/api/anthropic/v1/messages?beta=true", f.token); w.Code < 400 || f.calls.Load() != 0 {
		t.Fatal("Native high default silently accepted for none effort")
	}
	f = newGateFixture(t)
	g, e := NewCallGate(f.config)
	if e != nil {
		t.Fatal(e)
	}
	*f.config.Binding.Target.Effort.Value = "low"
	f.config.Binding.Target.Account = "fixture-other"
	r := httptest.NewRequest("POST", "/api/anthropic/v1/messages?beta=true", strings.NewReader(gateBody))
	r.Header.Set("Authorization", "Bearer "+f.token)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("caller mutation changed gate binding", w.Code)
	}
}

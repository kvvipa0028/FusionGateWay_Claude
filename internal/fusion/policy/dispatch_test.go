package policy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
	"github.com/yetone/magpie/internal/fusion/testsupport"
)

type dispatchFixture struct {
	s      *store.Store
	m      *Manager
	c      Claims
	route  stageplan.Route
	target stageplan.ExecutionTarget
	raw    string
}

func newDispatchFixture(t *testing.T) *dispatchFixture {
	t.Helper()
	root := t.TempDir()
	os.Chmod(root, 0700)
	root, _ = filepath.EvalSymlinks(root)
	s, e := store.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	def := "high"
	r := stageplan.Route{ID: "fixture-route", Revision: 1, Model: "fixture-model-a", Account: "fixture-account-a", Workspace: "fixture-workspace", CredentialIdentity: "fixture-credential-a", RuntimeVersion: "fixture-v1", BillingPath: "fixture-subscription", BillingKnown: true, Admitted: true, Efforts: []string{"high"}, DefaultEffort: &def, Capabilities: []string{"text"}, LockEnforcement: stageplan.ControlledCalls}
	p, e := stageplan.Compile(1, []stageplan.Role{stageplan.Design}, stageplan.Layer{Roles: map[stageplan.Role]stageplan.Binding{stageplan.Design: {Mode: stageplan.Locked, Route: &stageplan.RouteRef{ID: r.ID, Revision: 1}, Model: r.Model, Effort: &stageplan.EffortSelection{Mode: stageplan.EffortExplicit, Value: "high"}}}}, stageplan.Layer{}, stageplan.Layer{}, []stageplan.Route{r})
	if e != nil {
		t.Fatal(e)
	}
	task, e := s.Create("fixture-key", store.CreateRequest{ProjectID: "fixture-project", Goal: "fixture", Plan: p})
	if e != nil {
		t.Fatal(e)
	}
	target := *p.Bindings[stageplan.Design].Target
	run, e := s.StartIntent(store.StartRequest{TaskID: task.ID, Role: stageplan.Design, PlanRevision: 1, Owner: "fixture-owner", TTL: time.Minute, Target: target})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConfirmStarted(run.ID, run.Generation, run.Owner, "fixture-session"); e != nil {
		t.Fatal(e)
	}
	c := Claims{TaskID: task.ID, RunID: run.ID, Role: run.Role, Attempt: run.Attempt, PlanRevision: run.PlanRevision, Generation: run.Generation, ProjectID: task.ProjectID, Audience: ModelAudience}
	m := NewManager("fixture-admin", StoreValidator(s), nil)
	raw, e := m.Issue(c, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	return &dispatchFixture{s: s, m: m, c: c, route: r, target: target, raw: raw}
}

func (f *dispatchFixture) context(t *testing.T) context.Context {
	t.Helper()
	ctx, e := f.m.WithStage(context.Background(), f.raw, f.c)
	if e != nil {
		t.Fatal(e)
	}
	return ctx
}

func TestStrictDispatchFrozenTargetAndCredentialRenewal(t *testing.T) {
	f := newDispatchFixture(t)
	calls := 0
	credential := "fixture-secret-one"
	p := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var in Invocation
		json.NewDecoder(r.Body).Decode(&in)
		if in.Target.ResolvedModel != "fixture-model-a" || *in.Target.Effort.Value != "high" || in.Target.Account != "fixture-account-a" || r.Header.Get("Authorization") != "Bearer "+credential {
			t.Error("frozen dispatch changed")
		}
		json.NewEncoder(w).Encode(Reply{Text: "fixture", UpstreamReportedModel: stringPtr("fixture-model-a")})
	}))
	defer p.Close()
	tr := testsupport.NewTransport(p.URL)
	defer tr.CloseIdleConnections()
	ex := &HTTPJSONExecutor{Endpoint: p.URL, Transport: tr, Credential: func(context.Context) (Credential, error) {
		return Credential{Identity: f.route.CredentialIdentity, Account: f.route.Account, Secret: credential}, nil
	}}
	d := Dispatcher{Manager: f.m, Store: f.s, Lookup: func(stageplan.RouteRef) (DispatchRoute, error) {
		return DispatchRoute{TransportID: "fixture-http-v1", Route: f.route, Executor: ex}, nil
	}, Permit: func(context.Context, Claims, stageplan.ExecutionTarget, int) error { return nil }}
	for _, v := range []string{"fixture-secret-one", "fixture-secret-two"} {
		credential = v
		out, e := d.Dispatch(f.context(t), DispatchRequest{Messages: []Message{{Role: "user", Content: "fixture"}}}, 1)
		if e != nil || out.ResolvedModel != "fixture-model-a" || out.UpstreamReportedModel == nil {
			t.Fatalf("%+v %v", out, e)
		}
	}
	if calls != 2 || tr.Outbound() != 2 {
		t.Fatal("wrong call count")
	}
}

func stringPtr(v string) *string { return &v }

type fixtureExecutor struct {
	call func(context.Context, Invocation) (Reply, error)
}

func (e fixtureExecutor) Call(ctx context.Context, in Invocation) (Reply, error) {
	return e.call(ctx, in)
}

func TestStrictDispatchRejectsTargetDriftBeforeProvider(t *testing.T) {
	for _, edit := range []string{"removed", "model", "account", "identity", "effort", "default", "runtime", "plugin", "billing", "admission", "lock", "capability"} {
		t.Run(edit, func(t *testing.T) {
			f := newDispatchFixture(t)
			calls := 0
			r := f.route
			switch edit {
			case "model":
				r.Model = "fixture-model-b"
			case "account":
				r.Account = "fixture-account-b"
			case "identity":
				r.CredentialIdentity = "fixture-credential-b"
			case "effort":
				r.Efforts = []string{"medium"}
			case "default": /* explicit effort must survive an unrelated default change */
				r.DefaultEffort = stringPtr("high")
			case "runtime":
				r.RuntimeVersion = "fixture-v2"
			case "plugin":
				r.PluginVersion = stringPtr("fixture-v2")
			case "billing":
				r.BillingPath = "fixture-cash"
			case "admission":
				r.Admitted = false
			case "lock":
				r.LockEnforcement = stageplan.Unverified
			case "capability":
				r.Capabilities = nil
			}
			d := Dispatcher{Manager: f.m, Store: f.s, Lookup: func(stageplan.RouteRef) (DispatchRoute, error) {
				if edit == "removed" {
					return DispatchRoute{}, errors.New("missing")
				}
				return DispatchRoute{TransportID: "fixture-http-v1", Route: r, Executor: fixtureExecutor{func(context.Context, Invocation) (Reply, error) { calls++; return Reply{Text: "fixture"}, nil }}}, nil
			}, Permit: func(context.Context, Claims, stageplan.ExecutionTarget, int) error { return nil }}
			_, e := d.Dispatch(f.context(t), DispatchRequest{Messages: []Message{{Role: "user", Content: "fixture"}}}, 1)
			if edit == "default" {
				if e != nil || calls != 1 {
					t.Fatalf("explicit effort changed by defaults: %v", e)
				}
				return
			}
			if e == nil || calls != 0 {
				t.Fatalf("drift admitted: %v calls=%d", e, calls)
			}
		})
	}
}

func TestStrictDispatchRetriesRecheckIdentityAndNeverFallback(t *testing.T) {
	for _, change := range []string{"none", "route", "revoked", "budget", "cancelled"} {
		t.Run(change, func(t *testing.T) {
			f := newDispatchFixture(t)
			calls := 0
			permits := 0
			r := f.route
			ex := fixtureExecutor{func(ctx context.Context, in Invocation) (Reply, error) {
				calls++
				if change == "revoked" {
					f.m.RevokeRun(f.c.RunID)
				}
				if change == "cancelled" {
					run, _ := f.s.Run(f.c.RunID)
					f.s.CancelIntent(run.ID, run.Generation, run.Owner)
				}
				return Reply{}, &DispatchFailure{Retryable: true}
			}}
			d := Dispatcher{Manager: f.m, Store: f.s, Lookup: func(stageplan.RouteRef) (DispatchRoute, error) {
				if calls > 0 && change == "route" {
					r.Account = "fixture-account-b"
				}
				return DispatchRoute{TransportID: "fixture-http-v1", Route: r, Executor: ex}, nil
			}, Permit: func(context.Context, Claims, stageplan.ExecutionTarget, int) error {
				permits++
				if permits > 1 && change == "budget" {
					return errors.New("exhausted")
				}
				return nil
			}}
			_, e := d.Dispatch(f.context(t), DispatchRequest{Messages: []Message{{Role: "user", Content: "fixture"}}}, 2)
			want := 1
			if change == "none" {
				want = 2
			}
			if e == nil || calls != want {
				t.Fatalf("%v calls=%d", e, calls)
			}
		})
	}
}

func TestStrictDispatchMissingAuthorityAndOverrides(t *testing.T) {
	f := newDispatchFixture(t)
	calls := 0
	d := Dispatcher{Manager: f.m, Store: f.s, Lookup: func(stageplan.RouteRef) (DispatchRoute, error) {
		return DispatchRoute{TransportID: "fixture-http-v1", Route: f.route, Executor: fixtureExecutor{func(context.Context, Invocation) (Reply, error) { calls++; return Reply{}, nil }}}, nil
	}, Permit: func(context.Context, Claims, stageplan.ExecutionTarget, int) error { return nil }}
	requests := []DispatchRequest{{Model: "fixture-model-b", Messages: []Message{{Role: "user", Content: "fixture"}}}, {Effort: stringPtr("medium"), Messages: []Message{{Role: "user", Content: "fixture"}}}, {Messages: []Message{{Role: "tool", Content: "fixture"}}}, {Messages: []Message{{Role: "user", Content: strings.Repeat("x", 65537)}}}}
	for _, in := range requests {
		if _, e := d.Dispatch(f.context(t), in, 1); e == nil {
			t.Fatal("override admitted")
		}
	}
	if _, e := d.Dispatch(context.Background(), DispatchRequest{Messages: []Message{{Role: "user", Content: "fixture"}}}, 1); e == nil {
		t.Fatal("no auth")
	}
	d.Permit = nil
	if _, e := d.Dispatch(f.context(t), DispatchRequest{Messages: []Message{{Role: "user", Content: "fixture"}}}, 1); e == nil {
		t.Fatal("no gate")
	}
	if calls != 0 {
		t.Fatal("provider called")
	}
}

func TestStrictDispatchUnknownAndMismatchedUpstreamModel(t *testing.T) {
	for _, model := range []*string{nil, stringPtr("fixture-model-b")} {
		f := newDispatchFixture(t)
		calls := 0
		d := Dispatcher{Manager: f.m, Store: f.s, Lookup: func(stageplan.RouteRef) (DispatchRoute, error) {
			return DispatchRoute{TransportID: "fixture-http-v1", Route: f.route, Executor: fixtureExecutor{func(context.Context, Invocation) (Reply, error) {
				calls++
				return Reply{Text: "fixture", UpstreamReportedModel: model}, nil
			}}}, nil
		}, Permit: func(context.Context, Claims, stageplan.ExecutionTarget, int) error { return nil }}
		out, e := d.Dispatch(f.context(t), DispatchRequest{Messages: []Message{{Role: "user", Content: "fixture"}}}, 2)
		if model == nil {
			if e != nil || out.UpstreamReportedModel != nil {
				t.Fatal("unknown model forged")
			}
		} else if e == nil {
			t.Fatal("mismatch accepted")
		}
		if calls != 1 {
			t.Fatal("mismatch retried")
		}
	}
}

func TestStrictDispatchGateCannotChangeRouteOrMutateFrozenTarget(t *testing.T) {
	for _, change := range []string{"route", "transport", "revoke", "copy"} {
		t.Run(change, func(t *testing.T) {
			f := newDispatchFixture(t)
			calls := 0
			r := f.route
			transport := "fixture-v1"
			d := Dispatcher{Manager: f.m, Store: f.s, Lookup: func(stageplan.RouteRef) (DispatchRoute, error) {
				return DispatchRoute{Route: r, TransportID: transport, Executor: fixtureExecutor{func(_ context.Context, in Invocation) (Reply, error) {
					calls++
					if *in.Target.Effort.Value != "high" {
						t.Error("gate mutated frozen effort")
					}
					*in.Target.Effort.Value = "fixture-mutated"
					return Reply{Text: "fixture"}, nil
				}}}, nil
			}, Permit: func(_ context.Context, _ Claims, target stageplan.ExecutionTarget, _ int) error {
				switch change {
				case "route":
					r.Admitted = false
				case "transport":
					transport = "fixture-v2"
				case "revoke":
					f.m.RevokeRun(f.c.RunID)
				case "copy":
					*target.Effort.Value = "fixture-mutated"
				}
				return nil
			}}
			_, e := d.Dispatch(f.context(t), DispatchRequest{Messages: []Message{{Role: "user", Content: "fixture"}}}, 1)
			if change == "copy" {
				if e != nil || calls != 1 {
					t.Fatal(e)
				}
				run, _ := f.s.Run(f.c.RunID)
				if *run.Target.Effort.Value != "high" {
					t.Fatal("executor mutated persistent target")
				}
			} else if e == nil || calls != 0 {
				t.Fatalf("%v calls=%d", e, calls)
			}
		})
	}
}

func TestHTTPStrictExecutorRefusesIdentitySwitchRedirectAndAmbiguousResponse(t *testing.T) {
	for _, mode := range []string{"identity", "account", "redirect", "503", "partial", "duplicate", "unknown", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			f := newDispatchFixture(t)
			calls, alternate := 0, 0
			alt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { alternate++; w.WriteHeader(200) }))
			defer alt.Close()
			p := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				switch mode {
				case "redirect":
					http.Redirect(w, r, alt.URL, 307)
				case "503":
					http.Error(w, "fixture-secret-should-not-leak", 503)
				case "partial":
					w.Header().Set("Content-Length", "100")
					w.Write([]byte(`{"text":"partial"`))
				case "duplicate":
					w.Write([]byte(`{"text":"fixture","upstream_reported_model":"fixture-model-b","upstream_reported_model":"fixture-model-a"}`))
				case "unknown":
					w.Write([]byte(`{"text":"fixture","unexpected":"fixture-secret-should-not-leak"}`))
				case "oversize":
					w.Write([]byte(strings.Repeat("x", 1024*1024+1)))
				default:
					json.NewEncoder(w).Encode(Reply{Text: "fixture"})
				}
			}))
			defer p.Close()
			tr := testsupport.NewTransport(p.URL)
			defer tr.CloseIdleConnections()
			cred := Credential{Identity: f.route.CredentialIdentity, Account: f.route.Account, Secret: "fixture-secret"}
			if mode == "identity" {
				cred.Identity = "fixture-other"
			}
			if mode == "account" {
				cred.Account = "fixture-other"
			}
			ex := &HTTPJSONExecutor{Endpoint: p.URL, Transport: tr, Credential: func(context.Context) (Credential, error) { return cred, nil }}
			d := Dispatcher{Manager: f.m, Store: f.s, Lookup: func(stageplan.RouteRef) (DispatchRoute, error) {
				return DispatchRoute{TransportID: p.URL, Route: f.route, Executor: ex}, nil
			}, Permit: func(context.Context, Claims, stageplan.ExecutionTarget, int) error { return nil }}
			out, e := d.Dispatch(f.context(t), DispatchRequest{Messages: []Message{{Role: "user", Content: "fixture"}}}, 3)
			if e == nil || strings.Contains(e.Error(), "fixture-secret") || out.Text != "" {
				t.Fatalf("unsafe reply: %+v %v", out, e)
			}
			want := 1
			if mode == "identity" || mode == "account" {
				want = 0
			}
			if calls != want || alternate != 0 {
				t.Fatalf("calls=%d alternate=%d", calls, alternate)
			}
		})
	}
}

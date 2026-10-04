//go:build darwin || linux

package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/api"
	"github.com/yetone/magpie/internal/fusion/control"
	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/quota"
	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
	"github.com/yetone/magpie/internal/fusion/workspace"
)

func privateExecutionDir(t *testing.T) string {
	t.Helper()
	p, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(p, 0700); e != nil {
		t.Fatal(e)
	}
	return p
}

func admittedFixtureRoute(e RuntimeEnvironment) stageplan.Route {
	p, _ := e.Source.Project("fixture-project")
	r := p.Configuration.Routes[0]
	r.Admitted, r.BillingKnown = true, true
	r.Capabilities = []string{"text"}
	r.LockEnforcement = stageplan.ControlledCalls
	return r
}
func fixtureHostInspection(r stageplan.Route) policy.Inspection {
	now := time.Now()
	used := float64(20)
	return policy.Inspection{Route: r, Provider: "fixture-provider", QueryAdmitted: true, DataAllowed: true, SandboxVerified: true, VerificationAvailable: true, AllCallsCounted: true, ManagedExecutions: 1, ProofHash: strings.Repeat("a", 64), Quota: quota.Snapshot{Identity: quota.Identity{Provider: "fixture-provider", Account: r.Account, Workspace: r.Workspace, Region: "CN", Generation: 1}, Source: "fixture", ObservedAt: &now, ReceivedAt: now, Complete: true, Status: quota.Available, Pool: quota.Pool{ID: "fixture-pool", Provider: "fixture-provider", Region: "CN", Scope: "account", Owner: r.Account, Verified: true}, Windows: []quota.Window{{Kind: "subscription", Unit: "percent", UsedPercent: &used}}}}
}
func hostRegistration(e RuntimeEnvironment) RuntimeRegistration {
	r := admittedFixtureRoute(e)
	return RuntimeRegistration{Routes: map[string][]stageplan.Route{"fixture-project": {r}}, Inspect: func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (policy.Inspection, error) {
		return fixtureHostInspection(r), nil
	}, Resolve: func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (control.Launch, error) {
		return control.Launch{}, control.ErrUnsupported
	}}
}

func TestExecutionHostRejectsRouteIdentityChangesAndCleansFactory(t *testing.T) {
	for _, mode := range []string{"nil", "empty", "unknown-project", "unknown-route", "revision", "model", "account", "workspace", "credential", "runtime", "billing", "effort", "plugin", "unadmitted", "no-inspector", "no-resolver", "factory-error"} {
		t.Run(mode, func(t *testing.T) {
			path, _ := sourceFixture(t)
			root := filepath.Join(filepath.Dir(path), "control")
			var closed atomic.Int64
			var factory RuntimeFactory
			if mode != "nil" {
				factory = func(ctx context.Context, e RuntimeEnvironment) (RuntimeRegistration, error) {
					r := hostRegistration(e)
					r.Close = func(context.Context) error { closed.Add(1); return nil }
					x := r.Routes["fixture-project"][0]
					switch mode {
					case "empty":
						r.Routes = nil
					case "unknown-project":
						r.Routes = map[string][]stageplan.Route{"fixture-other": {x}}
					case "unknown-route":
						x.ID = "fixture-other"
					case "revision":
						x.Revision++
					case "model":
						x.Model = "fixture-other"
					case "account":
						x.Account = "fixture-other"
					case "workspace":
						x.Workspace = "fixture-other"
					case "credential":
						x.CredentialIdentity = "fixture-other"
					case "runtime":
						x.RuntimeVersion = "fixture-other"
					case "billing":
						x.BillingPath = "api"
					case "effort":
						x.NoEffort = false
					case "plugin":
						v := "fixture-other"
						x.PluginVersion = &v
					case "unadmitted":
						x.Admitted = false
					case "no-inspector":
						r.Inspect = nil
					case "no-resolver":
						r.Resolve = nil
					case "factory-error":
						return r, errors.New("fixture-private-secret")
					}
					if r.Routes != nil && mode != "unknown-project" {
						r.Routes["fixture-project"][0] = x
					}
					return r, nil
				}
			}
			h, e := OpenExecutionControl(context.Background(), path, root, "127.0.0.1:0", factory)
			if h != nil {
				h.Close()
				t.Fatal("invalid runtime registered")
			}
			if !errors.Is(e, ErrControlHost) {
				t.Fatal("unsafe registration accepted or error disclosed", e)
			}
			if mode != "nil" && closed.Load() != 1 {
				t.Fatal("factory cleanup missing")
			}
			// Failed registration must relinquish the database owner.
			draft, e := OpenControl(path, root, "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			draft.Close()
		})
	}
}

type hostExecution struct {
	done      chan struct{}
	cancelled chan struct{}
	once      sync.Once
	result    managed.Result
}

func (x *hostExecution) Cancel() error { x.once.Do(func() { close(x.cancelled) }); return nil }
func (x *hostExecution) Wait(ctx context.Context) (managed.Result, error) {
	select {
	case <-ctx.Done():
		return managed.Result{}, ctx.Err()
	case <-x.done:
		return x.result, nil
	}
}

func hostHTTP(t *testing.T, h *ControlHost, method, path, body, key, tag string) (int, []byte, http.Header) {
	t.Helper()
	token, e := os.ReadFile(filepath.Join(h.root, "management.token"))
	if e != nil {
		t.Fatal(e)
	}
	r, e := http.NewRequest(method, "http://"+h.Addr()+path, strings.NewReader(body))
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	r.Header.Set("Content-Type", "application/json")
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	if tag != "" {
		r.Header.Set("If-Match", tag)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	response, e := client.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	b, e := io.ReadAll(response.Body)
	if e != nil {
		t.Fatal(e)
	}
	return response.StatusCode, b, response.Header
}
func hostCreateTask(t *testing.T, h *ControlHost) store.Task {
	t.Helper()
	code, b, _ := hostHTTP(t, h, "POST", "/control/v1/tasks/preview", `{"project_id":"fixture-project","goal":"fixture goal","required_roles":["design"]}`, "", "")
	var p api.Preview
	if code != 200 || json.Unmarshal(b, &p) != nil {
		t.Fatal("preview", code, string(b))
	}
	body, _ := json.Marshal(api.SubmitRequest{PreviewID: p.ID, PlanHash: p.Plan.Hash})
	code, b, _ = hostHTTP(t, h, "POST", "/agent/v1/tasks", string(body), "fixture-create", "")
	var task store.Task
	if code != 201 || json.Unmarshal(b, &task) != nil {
		t.Fatal("submit", code, string(b))
	}
	return task
}
func serveExecutionHost(t *testing.T, h *ControlHost) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		h.Close()
		select {
		case e := <-done:
			if e != nil {
				t.Error(e)
			}
		case <-time.After(5 * time.Second):
			t.Error("host serve did not exit")
		}
	})
}
func TestExecutionHostBindsRegisteredSourceAndPermissionsBeforeIntent(t *testing.T) {
	for _, mode := range []string{"absent", "other-source", "writable", "root-in-project", "inspect-write", "inspect-route", "rotated-during-resolve"} {
		t.Run(mode, func(t *testing.T) {
			path, d := sourceFixture(t)
			root := filepath.Join(filepath.Dir(path), "control")
			var started atomic.Int64
			h, e := OpenExecutionControl(context.Background(), path, root, "127.0.0.1:0", func(ctx context.Context, env RuntimeEnvironment) (RuntimeRegistration, error) {
				reg := hostRegistration(env)
				r := reg.Routes["fixture-project"][0]
				reg.Inspect = func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (policy.Inspection, error) {
					in := fixtureHostInspection(r)
					if mode == "inspect-write" {
						in.WriteKey = "fixture-write"
					}
					if mode == "inspect-route" {
						in.Route.Account = "fixture-other"
					}
					return in, nil
				}
				source := d.Projects[0].Path
				if mode == "other-source" {
					source = privateExecutionDir(t)
				}
				snapshot, e := workspace.Copy(source, privateExecutionDir(t), "copy")
				if e != nil {
					t.Fatal(e)
				}
				guard, e := snapshot.Guard()
				if e != nil {
					t.Fatal(e)
				}
				reg.Resolve = func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (control.Launch, error) {
					spec := managed.Spec{Root: privateExecutionDir(t), Workspace: snapshot.Path, Source: guard, Input: []byte("fixture"), Timeout: time.Second}
					if mode == "absent" {
						spec.Source = workspace.SourceGuard{}
					}
					if mode == "writable" {
						spec.Writable = true
					}
					if mode == "root-in-project" {
						spec.Root = d.Projects[0].Path
					}
					if mode == "rotated-during-resolve" {
						d.Revision++
						writeSource(t, path, d)
					}
					return control.Launch{Spec: spec, Backend: control.Backend{Probe: func(context.Context) (managed.Capabilities, error) {
						return managed.Capabilities{Probe: true, Start: true, Events: true, Cancel: true}, nil
					}, Start: func(context.Context, store.StageRun, managed.Spec) (control.Execution, error) {
						started.Add(1)
						return nil, nil
					}, Release: func(policy.StopProof) error { return nil }}}, nil
				}
				return reg, nil
			})
			if e != nil {
				t.Fatal(e)
			}
			serveExecutionHost(t, h)
			task := hostCreateTask(t, h)
			code, b, _ := hostHTTP(t, h, "POST", "/control/v1/tasks/"+task.ID+"/start", `{"role":"design"}`, "fixture-start", `"p1-g0-ready"`)
			if code < 400 {
				t.Fatal("unsafe execution accepted", code, string(b))
			}
			if started.Load() != 0 {
				t.Fatal("Native started")
			}
			events, e := h.store.Events(task.ID, 0)
			if e != nil || len(events) != 1 {
				t.Fatal("unsafe source committed intent", e)
			}
		})
	}
}

func TestExecutionHostShutdownWaitsForOwnedCompletionAndCanRetry(t *testing.T) {
	path, _ := sourceFixture(t)
	root := filepath.Join(filepath.Dir(path), "control")
	finish := make(chan struct{})
	var cleanup atomic.Int64
	var ownerStore *store.Store
	var execution *hostExecution
	h, e := OpenExecutionControl(context.Background(), path, root, "127.0.0.1:0", func(ctx context.Context, env RuntimeEnvironment) (RuntimeRegistration, error) {
		ownerStore = env.Store
		reg := hostRegistration(env)
		p, _ := env.Source.Project("fixture-project")
		snapshot, e := workspace.Copy(p.Path, privateExecutionDir(t), "copy")
		if e != nil {
			t.Fatal(e)
		}
		guard, _ := snapshot.Guard()
		reg.Close = func(context.Context) error {
			if _, e := env.Store.Capacity(); e != nil {
				t.Error("store closed before runtime cleanup")
			}
			cleanup.Add(1)
			return nil
		}
		reg.Resolve = func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (control.Launch, error) {
			return control.Launch{Spec: managed.Spec{Root: privateExecutionDir(t), Workspace: snapshot.Path, Source: guard, Input: []byte("fixture"), Timeout: time.Second}, Backend: control.Backend{Probe: func(context.Context) (managed.Capabilities, error) {
				return managed.Capabilities{Probe: true, Start: true, Events: true, Cancel: true}, nil
			}, Release: func(proof policy.StopProof) error {
				return env.Store.ReleaseReserved(proof.RunID, proof.Generation, proof.ReportHash, true)
			}, Start: func(ctx context.Context, r store.StageRun, s managed.Spec) (control.Execution, error) {
				if e := env.Store.ConfirmStarted(r.ID, r.Generation, r.Owner, "fixture-session"); e != nil {
					return nil, e
				}
				execution = &hostExecution{done: make(chan struct{}), cancelled: make(chan struct{})}
				go func() {
					<-ctx.Done()
					execution.Cancel()
					<-finish
					if e := env.Store.Finish(r.ID, r.Generation, r.Owner, "cancelled"); e != nil {
						t.Error(e)
					}
					execution.result = managed.Result{State: "cancelled", StoppedVerified: true, Proof: policy.StopProof{RunID: r.ID, Generation: r.Generation, NativeSessionID: "fixture-session", ProcessIdentityHash: strings.Repeat("b", 64), ReportHash: strings.Repeat("c", 64), DescendantsStopped: true}}
					close(execution.done)
				}()
				return execution, nil
			}}}, nil
		}
		return reg, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	serveExecutionHost(t, h)
	task := hostCreateTask(t, h)
	code, b, _ := hostHTTP(t, h, "POST", "/control/v1/tasks/"+task.ID+"/start", `{"role":"design"}`, "fixture-start", `"p1-g0-ready"`)
	if code != 202 {
		t.Fatal("start", code, string(b))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if e := h.CloseContext(ctx); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("close claimed completion", e)
	}
	if cleanup.Load() != 0 {
		t.Fatal("runtime cleaned before wait")
	}
	if _, e := ownerStore.Task(task.ID); e != nil {
		t.Fatal("database closed while owned execution waits", e)
	}
	select {
	case <-execution.cancelled:
	case <-time.After(time.Second):
		t.Fatal("owned runtime not cancelled")
	}
	close(finish)
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	if e := h.CloseContext(ctx2); e != nil {
		t.Fatal(e)
	}
	if cleanup.Load() != 1 {
		t.Fatal("cleanup count")
	}
}

func TestExecutionHostQuotaCatalogueAndRevocation(t *testing.T) {
	for _, mode := range []string{"unknown-project", "unknown-route", "account", "workspace", "no-current", "success", "revoke-fetch"} {
		t.Run(mode, func(t *testing.T) {
			path, d := sourceFixture(t)
			root := filepath.Join(filepath.Dir(path), "control")
			var calls atomic.Int64
			h, e := OpenExecutionControl(context.Background(), path, root, "127.0.0.1:0", func(ctx context.Context, env RuntimeEnvironment) (RuntimeRegistration, error) {
				reg := hostRegistration(env)
				r := reg.Routes["fixture-project"][0]
				source := api.QuotaSource{Route: stageplan.RouteRef{ID: r.ID, Revision: r.Revision}, Identity: fixtureHostInspection(r).Quota.Identity,
					Current: func(context.Context, quota.Identity) bool { return true },
					Fetch: func(context.Context, quota.Identity) (quota.Snapshot, error) {
						calls.Add(1)
						if mode == "revoke-fetch" {
							d.Revision++
							writeSource(t, path, d)
						}
						return fixtureHostInspection(r).Quota, nil
					}}
				switch mode {
				case "unknown-route":
					source.Route.ID = "fixture-other"
				case "account":
					source.Identity.Account = "fixture-other"
				case "workspace":
					source.Identity.Workspace = "fixture-other"
				case "no-current":
					source.Current = nil
				}
				id := "fixture-project"
				if mode == "unknown-project" {
					id = "fixture-other"
				}
				reg.QuotaSources = map[string][]api.QuotaSource{id: {source}}
				return reg, nil
			})
			if mode != "success" && mode != "revoke-fetch" {
				if h != nil {
					h.Close()
					t.Fatal("invalid quota registration")
				}
				if !errors.Is(e, ErrControlHost) || calls.Load() != 0 {
					t.Fatal("registration fetched quota", e)
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			serveExecutionHost(t, h)
			quotaPath := "/control/v1/projects/fixture-project/quota"
			code, b, _ := hostHTTP(t, h, "GET", quotaPath, "", "", "")
			var reply api.QuotaReply
			if code != 200 || json.Unmarshal(b, &reply) != nil || len(reply.Routes) != 1 || reply.Routes[0].Status != quota.Unknown || calls.Load() != 0 {
				t.Fatal("cache GET issued query", code, string(b))
			}
			code, b, _ = hostHTTP(t, h, "POST", quotaPath+"/fixture-glm/refresh", `{}`, "", "")
			if calls.Load() != 1 {
				t.Fatal("quota query count")
			}
			if mode == "success" {
				if code != 200 || json.Unmarshal(b, &reply) != nil || reply.Routes[0].Status != quota.Available {
					t.Fatal("registered quota source not used", code, string(b))
				}
			} else if code < 400 || strings.Contains(string(b), `"status":"available"`) {
				t.Fatal("revoked result published", code, string(b))
			}
		})
	}
}

func TestExecutionHostCloseWaitsForDetachedQuotaReader(t *testing.T) {
	path, _ := sourceFixture(t)
	root := filepath.Join(filepath.Dir(path), "control")
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var cleaned atomic.Int64
	h, e := OpenExecutionControl(context.Background(), path, root, "127.0.0.1:0", func(ctx context.Context, env RuntimeEnvironment) (RuntimeRegistration, error) {
		reg := hostRegistration(env)
		r := reg.Routes["fixture-project"][0]
		reg.Close = func(context.Context) error {
			if _, e := env.Store.Capacity(); e != nil {
				t.Error("store closed before quota reader stopped")
			}
			cleaned.Add(1)
			return nil
		}
		reg.QuotaSources = map[string][]api.QuotaSource{"fixture-project": {{Route: stageplan.RouteRef{ID: r.ID, Revision: r.Revision}, Identity: fixtureHostInspection(r).Quota.Identity, Current: func(context.Context, quota.Identity) bool { return true }, Fetch: func(ctx context.Context, q quota.Identity) (quota.Snapshot, error) {
			close(entered)
			<-ctx.Done()
			close(cancelled)
			<-release
			return quota.Snapshot{}, ctx.Err()
		}}}}
		return reg, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	serveExecutionHost(t, h)
	token, e := os.ReadFile(filepath.Join(root, "management.token"))
	if e != nil {
		t.Fatal(e)
	}
	request, e := http.NewRequest("POST", "http://"+h.Addr()+"/control/v1/projects/fixture-project/quota/fixture-glm/refresh", strings.NewReader(`{}`))
	if e != nil {
		t.Fatal(e)
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	request.Header.Set("Content-Type", "application/json")
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		response, e := (&http.Client{Timeout: 5 * time.Second}).Do(request)
		if e == nil {
			io.Copy(io.Discard, response.Body)
			response.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("quota reader absent")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if e := h.CloseContext(ctx); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("detached reader not waited", e)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("detached reader not cancelled")
	}
	if cleaned.Load() != 0 {
		t.Fatal("early cleanup")
	}
	if _, e := h.store.Capacity(); e != nil {
		t.Fatal("early store close", e)
	}
	close(release)
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	if e := h.CloseContext(ctx2); e != nil {
		t.Fatal(e)
	}
	if cleaned.Load() != 1 {
		t.Fatal("cleanup count")
	}
	<-requestDone
}

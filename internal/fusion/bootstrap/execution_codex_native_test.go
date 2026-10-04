//go:build darwin

package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
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
	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/runtime/codex"
	"github.com/yetone/magpie/internal/fusion/runtime/codexadapter"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
	"github.com/yetone/magpie/internal/fusion/workspace"
)

var hostNativeCodex = flag.String("fusion-host-native-codex", "", "explicit pinned Codex HTTP host fixture; synthetic identity/model/quota only")

type hostCodexForwarder func(context.Context, stageplan.ExecutionTarget, []byte) (codex.ForwardResponse, error)

func (f hostCodexForwarder) Send(ctx context.Context, t stageplan.ExecutionTarget, b []byte) (codex.ForwardResponse, error) {
	return f(ctx, t, b)
}

func TestExecutionHostPinnedCodexHTTP(t *testing.T) {
	if *hostNativeCodex == "" {
		t.Skip("requires explicit pinned Native; no real credential")
	}
	exe, e := filepath.EvalSymlinks(*hostNativeCodex)
	if e != nil {
		t.Fatal(e)
	}
	hash, e := managed.FileHash(exe)
	if e != nil || hash != managed.CodexExecutableSHA256 {
		t.Fatal("Native pin changed")
	}
	for _, mode := range []string{"success", "cancel", "source-revocation", "close", "parent-cancel", "rejected-prompt", "rejected-timeout"} {
		t.Run(mode, func(t *testing.T) {
			path, d := sourceFixture(t)
			d.Routes[0].NativeRoute = "codex-chatgpt"
			effort := "medium"
			d.Routes[0].NoEffort = false
			d.Routes[0].Efforts = []string{effort}
			d.Routes[0].DefaultEffort = &effort
			binding := d.Projects[0].Layer.Groups[stageplan.DesignPlanning]
			binding.Effort = &stageplan.EffortSelection{Mode: stageplan.EffortDefault}
			d.Projects[0].Layer.Groups[stageplan.DesignPlanning] = binding
			d.Routes[0].RuntimeVersion = codex.CLIVersion
			writeSource(t, path, d)
			ownedText := []byte("FUSION_HOST_SOURCE\n")
			sourceFile := filepath.Join(d.Projects[0].Path, "fixture.txt")
			if os.WriteFile(sourceFile, ownedText, 0600) != nil {
				t.Fatal("fixture source")
			}
			root := filepath.Join(filepath.Dir(path), "control")
			parent, cancelParent := context.WithCancel(context.Background())
			defer cancelParent()
			var calls atomic.Int64
			var resolved atomic.Int64
			var cleaned atomic.Int64
			entered, ended := make(chan struct{}), make(chan struct{})
			var once, exitOnce sync.Once
			var adapter *codexadapter.Adapter
			var ownerStore *store.Store
			var runID string
			h, e := OpenExecutionControl(parent, path, root, "127.0.0.1:0", func(ctx context.Context, env RuntimeEnvironment) (RuntimeRegistration, error) {
				ownerStore = env.Store
				reg := hostRegistration(env)
				r := reg.Routes["fixture-project"][0]
				reg.Inspect = func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (policy.Inspection, error) {
					return fixtureHostInspection(r), nil
				}
				adapter, e = codexadapter.NewAdapter(codexadapter.AdapterConfig{Scheduler: env.Scheduler, Manager: env.Manager, Executable: exe, Identity: func(target stageplan.ExecutionTarget) codex.Identity {
					return codex.Identity{Account: target.Account, Workspace: target.Workspace, CredentialIdentity: target.CredentialIdentity, Generation: 1}
				}, Current: func(b codex.Binding) bool { return env.Current("fixture-project") }, Forwarder: hostCodexForwarder(func(ctx context.Context, target stageplan.ExecutionTarget, raw []byte) (codex.ForwardResponse, error) {
					calls.Add(1)
					if target.ResolvedModel != r.Model || target.CredentialIdentity != r.CredentialIdentity {
						t.Error("frozen route changed")
					}
					if target.Effort.Value == nil || *target.Effort.Value != effort || target.RuntimeVersion != codex.CLIVersion || target.BillingPath != "subscription" {
						t.Error("frozen Codex contract changed")
					}
					if mode != "success" {
						once.Do(func() { close(entered) })
						<-ctx.Done()
						exitOnce.Do(func() { close(ended) })
						return codex.ForwardResponse{}, ctx.Err()
					}
					return codex.ForwardResponse{StatusCode: 200, ContentType: "text/event-stream", ReportedModel: target.ResolvedModel, Body: io.NopCloser(strings.NewReader(hostCodexSSE(target.ResolvedModel)))}, nil
				})})
				if e != nil {
					return reg, e
				}
				reg.Resolve = func(ctx context.Context, task store.Task, role stageplan.Role, target stageplan.ExecutionTarget) (control.Launch, error) {
					resolved.Add(1)
					p, e := env.Source.Project(task.ProjectID)
					if e != nil {
						return control.Launch{}, e
					}
					snapshot, e := workspace.Copy(p.Path, privateExecutionDir(t), "copy")
					if e != nil {
						return control.Launch{}, e
					}
					guard, e := snapshot.Guard()
					if e != nil {
						return control.Launch{}, e
					}
					spec := managed.Spec{Root: privateExecutionDir(t), Workspace: snapshot.Path, Source: guard, Timeout: 15 * time.Second, Input: []byte("Reply Fixture ready. Do not use tools.")}
					if mode == "rejected-prompt" {
						spec.Input = []byte(strings.Repeat("<", 6000))
					}
					if mode == "rejected-timeout" {
						spec.Timeout = 5 * time.Minute
					}
					return control.Launch{Backend: control.BindAdapter(adapter), Spec: spec}, nil
				}
				reg.Close = func(context.Context) error {
					cleaned.Add(1)
					if runID != "" {
						if _, e := env.Store.Run(runID); e != nil {
							t.Error("store closed before runtime")
						}
						if _, e := env.Store.Reservation(runID); !errors.Is(e, store.ErrNotFound) {
							t.Error("owned reservation not released before cleanup", e)
						}
					}
					return nil
				}
				return reg, nil
			})
			if e != nil {
				t.Fatal(e)
			}
			serveExecutionHost(t, h)
			task := hostCreateTask(t, h)
			// Management authority must be checked before trusted resolution.
			unauthorized, err := http.NewRequest("POST", "http://"+h.Addr()+"/control/v1/tasks/"+task.ID+"/start", strings.NewReader(`{"role":"design"}`))
			if err != nil {
				t.Fatal(err)
			}
			unauthorized.Header.Set("Content-Type", "application/json")
			unauthorized.Header.Set("Idempotency-Key", "fixture-unauthorized")
			unauthorized.Header.Set("If-Match", `"p1-g0-ready"`)
			denied, err := (&http.Client{Timeout: 5 * time.Second}).Do(unauthorized)
			if err != nil {
				t.Fatal(err)
			}
			denied.Body.Close()
			if denied.StatusCode != 401 || resolved.Load() != 0 || calls.Load() != 0 {
				t.Fatal("unauthorized execution", denied.StatusCode)
			}
			code, b, headers := hostHTTP(t, h, "POST", "/control/v1/tasks/"+task.ID+"/start", `{"role":"design"}`, "fixture-native-start", `"p1-g0-ready"`)
			if mode == "rejected-prompt" || mode == "rejected-timeout" {
				if code != 503 {
					t.Fatal("preflight HTTP status", code, string(b))
				}
				x, err := h.store.Task(task.ID)
				if err != nil || x.State != "ready" || x.Generation != 0 || calls.Load() != 0 || resolved.Load() != 1 {
					t.Fatal("preflight changed task or called upstream", err)
				}
				if _, err := h.store.LookupStart("fixture-native-start", store.StartIdentity{TaskID: task.ID, Role: stageplan.Design, PlanRevision: 1, Generation: 0}); !errors.Is(err, store.ErrNotFound) {
					t.Fatal("preflight receipt created", err)
				}
				budget, err := h.store.Budget(task.ID)
				if err != nil || budget.UsedCalls != 0 {
					t.Fatal("preflight spent budget", err)
				}
				actual, err := os.ReadFile(sourceFile)
				if err != nil || string(actual) != string(ownedText) {
					t.Fatal("preflight source changed")
				}
				if strings.Contains(string(b), root) || strings.Contains(string(b), d.Projects[0].Path) {
					t.Fatal("private preflight error")
				}
				if err := h.Close(); err != nil {
					t.Fatal(err)
				}
				if cleaned.Load() != 1 {
					t.Fatal("preflight cleanup")
				}
				t.Logf("mode=%s native_processes=0 synthetic_calls=0 ready_generation=0 preintent=true", mode)
				return
			}
			var receipt api.ExecutionReply
			if code != 202 || json.Unmarshal(b, &receipt) != nil || !receipt.Created {
				t.Fatal("HTTP Native start", code, string(b))
			}
			runID = receipt.Run.ID
			if strings.Contains(string(b), root) || strings.Contains(string(b), d.Projects[0].Path) || headers.Get("Location") == "" {
				t.Fatal("unsafe Native receipt")
			}
			code, b, _ = hostHTTP(t, h, "POST", "/control/v1/tasks/"+task.ID+"/start", `{"role":"design"}`, "fixture-native-start", `"p1-g0-ready"`)
			var retry api.ExecutionReply
			if code != 200 || json.Unmarshal(b, &retry) != nil || retry.Created || retry.Run.ID != runID || resolved.Load() != 1 {
				t.Fatal("Native replay", code, string(b))
			}
			if mode != "success" {
				select {
				case <-entered:
				case <-time.After(10 * time.Second):
					t.Fatal("controlled Native request absent")
				}
				switch mode {
				case "cancel":
					code, _, hdr := hostHTTP(t, h, "GET", "/agent/v1/tasks/"+task.ID, "", "", "")
					if code != 200 {
						t.Fatal("task GET", code)
					}
					code, b, _ = hostHTTP(t, h, "POST", "/control/v1/tasks/"+task.ID+"/cancel", `{}`, "", hdr.Get("ETag"))
					if code != 202 {
						t.Fatal("Native cancel", code, string(b))
					}
				case "source-revocation":
					d.Revision++
					writeSource(t, path, d)
				case "close":
					if e := h.Close(); e != nil {
						t.Fatal(e)
					}
				case "parent-cancel":
					cancelParent()
				}
				select {
				case <-ended:
				case <-time.After(5 * time.Second):
					t.Fatal("owned HTTP request not revoked")
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			done, e := h.controller.Wait(ctx, runID)
			want := "succeeded"
			if mode != "success" {
				want = "cancelled"
			}
			if e != nil || done.State != want || !done.StoppedVerified || !done.Released {
				t.Fatal("Native stop/release", done, e)
			}
			if mode == "success" || mode == "cancel" {
				code, b, _ = hostHTTP(t, h, "GET", "/agent/v1/tasks/"+task.ID+"/runs/"+runID, "", "", "")
				var view api.ExecutionReply
				if code != 200 || json.Unmarshal(b, &view) != nil || view.Run.State != want {
					t.Fatal("run GET", code, string(b))
				}
				code, b, _ = hostHTTP(t, h, "POST", "/control/v1/tasks/"+task.ID+"/start", `{"role":"design"}`, "fixture-native-start", `"p1-g0-ready"`)
				if code != 200 || json.Unmarshal(b, &retry) != nil || retry.Created || retry.Run.ID != runID || resolved.Load() != 1 || calls.Load() != 1 {
					t.Fatal("terminal HTTP replay", code)
				}
				observed, text, err := adapter.Observation(runID, receipt.Run.Generation)
				if err != nil || observed.State != want || (want == "succeeded" && text != "Fixture ready.") || (want != "succeeded" && text != "") {
					t.Fatal("terminal observation", err)
				}
			}
			if mode == "source-revocation" {
				code, _, _ = hostHTTP(t, h, "GET", "/agent/v1/tasks/"+task.ID, "", "", "")
				if code != 503 {
					t.Fatal("revoked registration remained accessible", code)
				}
			}
			if e := h.CloseContext(ctx); e != nil {
				t.Fatal(e)
			}
			if cleaned.Load() != 1 {
				t.Fatal("runtime cleanup")
			}
			// Reopen only after ordered shutdown: verify durable real-process result.
			ownerStore, e = store.Open(filepath.Join(root, "tasks"))
			if e != nil {
				t.Fatal(e)
			}
			defer ownerStore.Close()
			r, e := ownerStore.Run(runID)
			if e != nil || r.State != want || !r.LaunchConfirmed || r.NativeSessionID == "" || r.Owner != "" {
				t.Fatal("durable Native terminal", e)
			}
			if _, e := ownerStore.Reservation(runID); !errors.Is(e, store.ErrNotFound) {
				t.Fatal("Native reservation retained", e)
			}
			budget, e := ownerStore.Budget(task.ID)
			if e != nil || int64(budget.UsedCalls) != calls.Load() || calls.Load() != 1 {
				t.Fatal("Native calls not fully counted", e)
			}
			actual, e := os.ReadFile(sourceFile)
			if e != nil || string(actual) != string(ownedText) {
				t.Fatal("source modified")
			}
			t.Logf("mode=%s calls=%d native_processes=1 durable=%s stopped_verified=true released=true real_identity=false real_model=false real_quota=false", mode, calls.Load(), r.State)
		})
	}
}

func hostCodexSSE(model string) string {
	item := map[string]any{"type": "message", "id": "msg_fixture", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "Fixture ready.", "annotations": []any{}}}}
	events := []map[string]any{{"type": "response.created", "response": map[string]any{"id": "resp_fixture", "status": "in_progress", "model": model, "output": []any{}}}, {"type": "response.output_item.done", "output_index": 0, "item": item}, {"type": "response.completed", "response": map[string]any{"id": "resp_fixture", "status": "completed", "model": model, "output": []any{item}, "usage": map[string]any{"input_tokens": 3, "output_tokens": 2, "total_tokens": 5}}}}
	var b strings.Builder
	for i, x := range events {
		x["sequence_number"] = i
		raw, _ := json.Marshal(x)
		fmt.Fprintf(&b, "event: %s\ndata: %s\n\n", x["type"], raw)
	}
	return b.String()
}

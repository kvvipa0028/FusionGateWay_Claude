//go:build darwin

package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
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
	"github.com/yetone/magpie/internal/fusion/runtime/grok"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
	"github.com/yetone/magpie/internal/fusion/workspace"
)

var hostNativeGrok = flag.String("fusion-host-native-grok", "", "explicit pinned Native HTTP host fixture; synthetic model/quota only")

type hostNativeForwarder func(context.Context, stageplan.ExecutionTarget, []byte) (grok.ForwardResponse, error)

func (f hostNativeForwarder) Send(ctx context.Context, t stageplan.ExecutionTarget, b []byte) (grok.ForwardResponse, error) {
	return f(ctx, t, b)
}

const hostNativeSSE = "data: {\"id\":\"fixture-response\",\"object\":\"chat.completion.chunk\",\"created\":1780000000,\"model\":\"fixture-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Fixture ready.\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"fixture-response\",\"object\":\"chat.completion.chunk\",\"created\":1780000000,\"model\":\"fixture-model\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\ndata: [DONE]\n\n"

func TestExecutionHostPinnedNativeHTTP(t *testing.T) {
	if *hostNativeGrok == "" {
		t.Skip("requires explicit pinned Native; no real credential")
	}
	exe, e := filepath.EvalSymlinks(*hostNativeGrok)
	if e != nil {
		t.Fatal(e)
	}
	hash, e := managed.FileHash(exe)
	if e != nil || hash != grok.NativeExecutableSHA256 {
		t.Fatal("Native pin changed")
	}
	for _, mode := range []string{"success", "cancel", "source-revocation", "close", "parent-cancel"} {
		t.Run(mode, func(t *testing.T) {
			path, d := sourceFixture(t)
			d.Routes[0].NativeRoute = "grok-subscription"
			d.Routes[0].RuntimeVersion = grok.CLIVersion
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
			var adapter *grok.Adapter
			var ownerStore *store.Store
			var runID string
			h, e := OpenExecutionControl(parent, path, root, "127.0.0.1:0", func(ctx context.Context, env RuntimeEnvironment) (RuntimeRegistration, error) {
				ownerStore = env.Store
				reg := hostRegistration(env)
				r := reg.Routes["fixture-project"][0]
				reg.Inspect = func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (policy.Inspection, error) {
					return fixtureHostInspection(r), nil
				}
				adapter, e = grok.NewAdapter(grok.AdapterConfig{Scheduler: env.Scheduler, Manager: env.Manager, Executable: exe, Current: func(b grok.GateBinding) bool { return env.Current("fixture-project") }, Forwarder: hostNativeForwarder(func(ctx context.Context, target stageplan.ExecutionTarget, raw []byte) (grok.ForwardResponse, error) {
					calls.Add(1)
					if target.ResolvedModel != r.Model || target.CredentialIdentity != r.CredentialIdentity {
						t.Error("frozen route changed")
					}
					var request struct {
						Tools []struct{ Function struct{ Name string } }
					}
					if json.Unmarshal(raw, &request) != nil {
						return grok.ForwardResponse{}, grok.ErrProtocol
					}
					main := false
					for _, tool := range request.Tools {
						if tool.Function.Name == "read_file" {
							main = true
						}
					}
					if main && mode != "success" {
						once.Do(func() { close(entered) })
						<-ctx.Done()
						exitOnce.Do(func() { close(ended) })
						return grok.ForwardResponse{}, ctx.Err()
					}
					return grok.ForwardResponse{StatusCode: 200, ContentType: "text/event-stream", Body: io.NopCloser(strings.NewReader(hostNativeSSE))}, nil
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
					return control.Launch{Backend: control.BindAdapter(adapter), Spec: managed.Spec{Root: privateExecutionDir(t), Workspace: snapshot.Path, Source: guard, Timeout: 15 * time.Second, Input: []byte("Reply Fixture ready.")}}, nil
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
			code, b, headers := hostHTTP(t, h, "POST", "/control/v1/tasks/"+task.ID+"/start", `{"role":"design"}`, "fixture-native-start", `"p1-g0-ready"`)
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
			if e != nil || int64(budget.UsedCalls) != calls.Load() || calls.Load() < 1 || calls.Load() > 2 {
				t.Fatal("Native calls not fully counted", e)
			}
			actual, e := os.ReadFile(sourceFile)
			if e != nil || string(actual) != string(ownedText) {
				t.Fatal("source modified")
			}
			t.Logf("mode=%s calls=%d native_processes=1 durable=%s stopped_verified=true released=true real_model=false real_quota=false", mode, calls.Load(), r.State)
		})
	}
}

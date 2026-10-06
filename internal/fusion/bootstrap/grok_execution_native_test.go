//go:build darwin

package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/yetone/magpie/internal/fusion/handoff"
	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/runtime/grok"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

// The factory drives the real pinned Grok process with a synthetic upstream,
// admission and quota only; no account, billing or generation admission.
func TestGrokFactoryPinnedNativeProductLifecycle(t *testing.T) {
	if *hostNativeGrok == "" {
		t.Skip("explicit pinned Native fixture only")
	}
	for _, mode := range []string{"success", "cancel", "registry_revocation", "credential_rotation", "source_revocation", "writer_launch"} {
		t.Run(mode, func(t *testing.T) {
			path, d, c, _ := grokRuntimeFixture(t, mode == "writer_launch")
			exe, err := filepath.EvalSymlinks(*hostNativeGrok)
			if err != nil {
				t.Fatal(err)
			}
			hash, err := managed.FileHash(exe)
			if err != nil || hash != grok.NativeExecutableSHA256 {
				t.Fatal("Native pin changed")
			}
			c.Executable = exe
			if err := os.WriteFile(filepath.Join(d.Projects[0].Path, "original.txt"), []byte("original\n"), 0600); err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int64
			var mu sync.Mutex
			var spec managed.Spec
			var owned control.Execution
			entered, ended := make(chan struct{}), make(chan struct{})
			var enterOnce, endOnce sync.Once
			forwarder := hostNativeForwarder(func(ctx context.Context, target stageplan.ExecutionTarget, raw []byte) (grok.ForwardResponse, error) {
				calls.Add(1)
				if target.ResolvedModel == "" || target.Effort.RequestedMode != stageplan.EffortNone || target.Effort.Value != nil || target.RuntimeVersion != grok.CLIVersion || target.BillingPath != "subscription" {
					t.Error("frozen Grok contract changed")
				}
				if mode != "success" && mode != "writer_launch" {
					enterOnce.Do(func() { close(entered) })
					<-ctx.Done()
					endOnce.Do(func() { close(ended) })
					return grok.ForwardResponse{}, ctx.Err()
				}
				return grok.ForwardResponse{StatusCode: 200, ContentType: "text/event-stream", ReportedModel: target.ResolvedModel, Body: io.NopCloser(strings.NewReader(hostNativeSSE))}, nil
			})
			factory, err := newGrokRuntimeFactory(c, func(credential *grok.FileCredential) (grok.NativeForwarder, error) {
				return forwarder, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			wrapped := func(ctx context.Context, e RuntimeEnvironment) (RuntimeRegistration, error) {
				reg, err := factory(ctx, e)
				if err != nil {
					return reg, err
				}
				resolve := reg.Resolve
				reg.Resolve = func(ctx context.Context, task store.Task, role stageplan.Role, target stageplan.ExecutionTarget) (control.Launch, error) {
					launch, err := resolve(ctx, task, role, target)
					mu.Lock()
					spec = launch.Spec
					mu.Unlock()
					start := launch.Backend.Start
					launch.Backend.Start = func(ctx context.Context, run store.StageRun, s managed.Spec) (control.Execution, error) {
						h, err := start(ctx, run, s)
						if err != nil {
							t.Logf("native start failed: %v", err)
						}
						mu.Lock()
						owned = h
						mu.Unlock()
						return h, err
					}
					return launch, err
				}
				return reg, err
			}
			root := filepath.Join(filepath.Dir(path), "control")
			h, err := OpenExecutionControl(context.Background(), path, root, "127.0.0.1:0", wrapped)
			if err != nil {
				t.Fatal(err)
			}
			serveExecutionHost(t, h)
			role := stageplan.Design
			if mode == "writer_preintent" {
				role = stageplan.Implementation
			}
			request := fmt.Sprintf(`{"project_id":"fixture-project","goal":"synthetic grok factory goal","required_roles":["%s"]}`, role)
			code, b, _ := hostHTTP(t, h, "POST", "/control/v1/tasks/preview", request, "", "")
			var preview api.Preview
			if code != 200 || json.Unmarshal(b, &preview) != nil {
				t.Fatal("factory preview", code)
			}
			raw, err := json.Marshal(api.SubmitRequest{PreviewID: preview.ID, PlanHash: preview.Plan.Hash})
			if err != nil {
				t.Fatal(err)
			}
			code, b, _ = hostHTTP(t, h, "POST", "/agent/v1/tasks", string(raw), "fixture-submit", "")
			var task store.Task
			if code != 201 || json.Unmarshal(b, &task) != nil {
				t.Fatal("factory submit", code)
			}
			code, b, _ = hostHTTP(t, h, "POST", "/control/v1/tasks/"+task.ID+"/start", fmt.Sprintf(`{"role":"%s"}`, role), "fixture-start", `"p1-g0-ready"`)
			if mode == "writer_launch" {
				// The native's write tools are in-process: the outer profile
				// bounds them, no nested sandbox is spawned. The writable
				// launch is a legal production path and must start.
				if code != 202 {
					t.Fatal("writable Grok launch refused", code, string(b))
				}
			}
			var receipt api.ExecutionReply
			if code != 202 || json.Unmarshal(b, &receipt) != nil || receipt.Run.ID == "" {
				t.Fatal("factory Native start", code, string(b))
			}
			if mode != "success" && mode != "writer_launch" {
				select {
				case <-entered:
				case <-time.After(10 * time.Second):
					t.Fatal("no owned Native call")
				}
				switch mode {
				case "cancel":
					code, _, hdr := hostHTTP(t, h, "GET", "/agent/v1/tasks/"+task.ID, "", "", "")
					if code != 200 {
						t.Fatal(code)
					}
					code, _, _ = hostHTTP(t, h, "POST", "/control/v1/tasks/"+task.ID+"/cancel", `{}`, "", hdr.Get("ETag"))
					if code != 202 {
						t.Fatal("cancel", code)
					}
				case "registry_revocation":
					c.Registry.Revoke(c.Route)
				case "credential_rotation":
					if err := os.WriteFile(c.CredentialPath, []byte(`{}`), 0600); err != nil {
						t.Fatal(err)
					}
				case "source_revocation":
					d.Revision++
					writeSource(t, path, d)
				}
				select {
				case <-ended:
				case <-time.After(5 * time.Second):
					t.Fatal("revoked owned Native still live")
				}
			}
			wait, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			done, err := h.controller.Wait(wait, receipt.Run.ID)
			want := "cancelled"
			if mode == "success" || mode == "writer_launch" {
				want = "succeeded"
			}
			if err != nil || done.State != want || !done.StoppedVerified || !done.Released {
				t.Fatal("factory Native stop/release", done, err)
			}
			mu.Lock()
			artifactPath := filepath.Join(filepath.Dir(spec.Workspace), "handoff")
			actualHandle := owned
			mu.Unlock()
			budgetStore := h.store
			if want == "succeeded" {
				indexed, err := h.store.Artifact(receipt.Run.ID)
				if err != nil || indexed.Reference.Binding.RunID != receipt.Run.ID || indexed.Reference.Binding.TaskID != task.ID {
					t.Fatal("real Native artifact not durably indexed", err)
				}
				raw, err := os.ReadFile(filepath.Join(artifactPath, "handoff.json"))
				var doc handoff.Document
				if err != nil || json.Unmarshal(raw, &doc) != nil || doc.Binding.TaskID != task.ID || doc.Binding.RunID != receipt.Run.ID || doc.Binding.Role != role || doc.Binding.PlanHash != preview.Plan.Hash {
					t.Fatal("actual run handoff absent or wrong", err)
				}
				entries, _ := json.Marshal(doc.Artifact.Entries)
				changes, _ := json.Marshal(doc.Changes)
				result, err := actualHandle.Wait(wait)
				if err != nil || handoff.Hash(entries) != doc.Artifact.TreeHash || handoff.Hash(changes) != doc.Artifact.ChangeHash || doc.Evidence.OutputHash != result.OutputHash || doc.Evidence.StopProofHash != result.Proof.ReportHash {
					t.Fatal("handoff hash binding lost", err)
				}
				if strings.Contains(string(raw), "opaque-fixture-grok-bearer") || strings.Contains(string(raw), "accounts.x.ai") {
					t.Fatal("credential entered handoff")
				}
				if err := h.Close(); err != nil {
					t.Fatal(err)
				}
				reopened, err := store.Open(filepath.Join(root, "tasks"))
				if err != nil {
					t.Fatal("actual DB reopen", err)
				}
				defer reopened.Close()
				budgetStore = reopened
				persisted, err := reopened.Artifact(receipt.Run.ID)
				if err != nil {
					t.Fatal("actual Native receipt lost", err)
				}
				consumer, err := handoff.Restore(persisted.Reference, d.Projects[0].Path, c.ExecutionRoot)
				if err != nil {
					t.Fatal("real artifact restore", err)
				}
				next, err := consumer.Copy(persisted.Reference.Binding, c.ExecutionRoot, "consumer-"+mode)
				if err != nil {
					t.Fatal("real artifact transfer", err)
				}
				if next.Path == "" || next.Path == spec.Workspace {
					t.Fatal("consumer copy bound to producer workspace")
				}
				if _, err := handoff.Restore(persisted.Reference, d.Projects[0].Path, filepath.Dir(c.ExecutionRoot)); err == nil {
					t.Fatal("unregistered root restore")
				}
			} else if _, err := os.Stat(artifactPath); !os.IsNotExist(err) {
				t.Fatal("cancelled run published successful artifact", err)
			}
			budget, err := budgetStore.Budget(task.ID)
			if err != nil || int64(budget.UsedCalls) != calls.Load() || calls.Load() < 1 {
				t.Fatal("factory bypassed persisted calls", err, budget, calls.Load())
			}
			if _, err := os.Stat(filepath.Join(d.Projects[0].Path, "created.txt")); !os.IsNotExist(err) {
				t.Fatal("factory changed original tree")
			}
			original, err := os.ReadFile(filepath.Join(d.Projects[0].Path, "original.txt"))
			if err != nil || string(original) != "original\n" {
				t.Fatal("original content changed", err)
			}
			t.Logf("production factory -> HTTP/Controller/Store -> GrokAdapter -> pinned Native -> %d synthetic calls; actual stop and artifact/release outcome verified; no real account/quota/billing admission", calls.Load())
		})
	}
}

//go:build darwin

package bootstrap

import (
	"context"
	"encoding/json"
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
	"github.com/yetone/magpie/internal/fusion/handoff"
	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

var hostNativeGLM = flag.String("fusion-host-native-claude", "", "explicit pinned Claude for production GLM factory; synthetic upstream/admission only")

type glmHostUpstream func(*http.Request) (*http.Response, error)

func (f glmHostUpstream) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func glmHostSSE(t *testing.T, n int64, writePath string) string {
	return glmHostSSEWithText(t, n, writePath, "Synthetic factory result.")
}

func glmHostSSEWithText(t *testing.T, n int64, writePath, text string) string {
	t.Helper()
	block := map[string]any{"type": "text", "text": ""}
	delta := map[string]any{"type": "text_delta", "text": text}
	stop := "end_turn"
	if writePath != "" {
		block = map[string]any{"type": "tool_use", "id": "fixture-edit", "name": "Edit", "input": map[string]any{}}
		input, err := json.Marshal(map[string]string{"file_path": writePath, "old_string": "", "new_string": "synthetic factory file\n"})
		if err != nil {
			t.Fatal(err)
		}
		delta = map[string]any{"type": "input_json_delta", "partial_json": string(input)}
		stop = "tool_use"
	}
	frames := []map[string]any{
		{"type": "message_start", "message": map[string]any{"id": fmt.Sprintf("fixture-message-%d", n), "type": "message", "role": "assistant", "model": "glm-5.3", "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 8, "output_tokens": 0}}},
		{"type": "content_block_start", "index": 0, "content_block": block},
		{"type": "content_block_delta", "index": 0, "delta": delta},
		{"type": "content_block_stop", "index": 0},
		{"type": "message_delta", "delta": map[string]any{"stop_reason": stop, "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 8}},
		{"type": "message_stop"},
	}
	var out strings.Builder
	for _, f := range frames {
		raw, err := json.Marshal(f)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&out, "event: %s\ndata: %s\n\n", f["type"], raw)
	}
	return out.String()
}

func TestGLMFactoryPinnedNativeProductLifecycle(t *testing.T) {
	if *hostNativeGLM == "" {
		t.Skip("explicit pinned Native fixture only")
	}
	for _, mode := range []string{"success", "implementation", "handoff_collision", "cancel", "registry_revocation", "credential_rotation", "source_revocation"} {
		t.Run(mode, func(t *testing.T) {
			path, d, c, _, _ := glmFactoryFixture(t, mode == "implementation")
			exe, err := filepath.EvalSymlinks(*hostNativeGLM)
			if err != nil {
				t.Fatal(err)
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
			upstream := glmHostUpstream(func(r *http.Request) (*http.Response, error) {
				n := calls.Add(1)
				if r.URL.String() != "https://open.bigmodel.cn/api/anthropic/v1/messages?beta=true" || r.Header.Get("Authorization") != "Bearer fixture-controller-key" || r.Header.Get("X-Api-Key") != "fixture-controller-key" || r.GetBody != nil {
					t.Error("factory credential/route drift")
				}
				if mode != "success" && mode != "implementation" && mode != "handoff_collision" {
					enterOnce.Do(func() { close(entered) })
					<-r.Context().Done()
					endOnce.Do(func() { close(ended) })
					return nil, r.Context().Err()
				}
				write := ""
				if mode == "handoff_collision" && n == 1 {
					mu.Lock()
					path := filepath.Join(filepath.Dir(spec.Workspace), "handoff")
					mu.Unlock()
					if err := os.Mkdir(path, 0700); err != nil {
						t.Error(err)
					}
					if err := os.WriteFile(filepath.Join(path, "foreign"), []byte("preserve"), 0600); err != nil {
						t.Error(err)
					}
				}
				if mode == "implementation" && n == 1 {
					mu.Lock()
					write = filepath.Join(spec.Workspace, "created.txt")
					mu.Unlock()
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(glmHostSSE(t, n, write)))}, nil
			})
			factory, err := newGLMRuntimeFactory(c, upstream)
			if err != nil {
				t.Fatal(err)
			}
			wrapped := func(ctx context.Context, e RuntimeEnvironment) (RuntimeRegistration, error) {
				reg, err := factory(ctx, e)
				resolve := reg.Resolve
				reg.Resolve = func(ctx context.Context, task store.Task, role stageplan.Role, target stageplan.ExecutionTarget) (control.Launch, error) {
					launch, err := resolve(ctx, task, role, target)
					mu.Lock()
					spec = launch.Spec
					mu.Unlock()
					start := launch.Backend.Start
					launch.Backend.Start = func(ctx context.Context, run store.StageRun, spec managed.Spec) (control.Execution, error) {
						h, err := start(ctx, run, spec)
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
			if mode == "implementation" {
				role = stageplan.Implementation
			}
			request := fmt.Sprintf(`{"project_id":"fixture-project","goal":"synthetic factory goal","required_roles":["%s"]}`, role)
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
			startBody := fmt.Sprintf(`{"role":"%s"}`, role)
			code, b, _ = hostHTTP(t, h, "POST", "/control/v1/tasks/"+task.ID+"/start", startBody, "fixture-start", `"p1-g0-ready"`)
			var receipt api.ExecutionReply
			if code != 202 || json.Unmarshal(b, &receipt) != nil || receipt.Run.ID == "" {
				t.Fatal("factory Native start", code, string(b))
			}
			if mode != "success" && mode != "implementation" && mode != "handoff_collision" {
				select {
				case <-entered:
				case <-time.After(8 * time.Second):
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
					if err := os.WriteFile(c.CredentialPath, []byte("fixture-rotated-key\n"), 0600); err != nil {
						t.Fatal(err)
					}
				case "source_revocation":
					d.Revision++
					writeSource(t, path, d)
				}
				select {
				case <-ended:
				case <-time.After(3 * time.Second):
					t.Fatal("revoked owned upstream still live")
				}
			}
			wait, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			done, err := h.controller.Wait(wait, receipt.Run.ID)
			want := "cancelled"
			if mode == "success" || mode == "implementation" || mode == "handoff_collision" {
				want = "succeeded"
			}
			if err != nil || done.State != want || done.StoppedVerified != (mode != "handoff_collision") || done.Released != (mode != "handoff_collision") {
				t.Fatal("factory Native stop/release", done, err)
			}
			mu.Lock()
			artifactPath := filepath.Join(filepath.Dir(spec.Workspace), "handoff")
			actualHandle := owned
			mu.Unlock()
			budgetStore := h.store
			if mode == "handoff_collision" {
				result, err := actualHandle.Wait(wait)
				if err != nil || !result.StoppedVerified || !result.Proof.DescendantsStopped {
					t.Fatal("collision lost actual stop proof", err)
				}
				if _, err := h.store.Reservation(receipt.Run.ID); err != nil {
					t.Fatal("capture failure released reservation", err)
				}
				b, err := os.ReadFile(filepath.Join(artifactPath, "foreign"))
				if err != nil || string(b) != "preserve" {
					t.Fatal("capture overwrote foreign directory", err)
				}
				if _, err := os.Stat(filepath.Join(artifactPath, "handoff.json")); !os.IsNotExist(err) {
					t.Fatal("failed capture published handoff", err)
				}
			} else if want == "succeeded" {
				indexed, err := h.store.Artifact(receipt.Run.ID)
				if err != nil || indexed.Reference.Binding.RunID != receipt.Run.ID || indexed.Reference.Binding.TaskID != task.ID {
					t.Fatal("real Native artifact not durably indexed", err)
				}
				raw, err := os.ReadFile(filepath.Join(artifactPath, "handoff.json"))
				var doc handoff.Document
				if err != nil || json.Unmarshal(raw, &doc) != nil || doc.Binding.TaskID != task.ID || doc.Binding.RunID != receipt.Run.ID || doc.Binding.Role != role || doc.Binding.PlanHash != preview.Plan.Hash || doc.Evidence.TestsExecuted || doc.Evidence.Status != "unverified" {
					t.Fatal("actual run handoff absent or wrong", err)
				}
				entries, _ := json.Marshal(doc.Artifact.Entries)
				changes, _ := json.Marshal(doc.Changes)
				result, err := actualHandle.Wait(wait)
				if err != nil || handoff.Hash(entries) != doc.Artifact.TreeHash || handoff.Hash(changes) != doc.Artifact.ChangeHash || doc.Evidence.OutputHash != result.OutputHash || doc.Evidence.StopProofHash != result.Proof.ReportHash {
					t.Fatal("handoff hash binding lost", err)
				}
				if mode == "implementation" {
					b, err := os.ReadFile(filepath.Join(artifactPath, "code", "created.txt"))
					if err != nil || string(b) != "synthetic factory file\n" || len(doc.Changes) != 1 || doc.Artifact.BaseTreeHash == doc.Artifact.TreeHash {
						t.Fatal("actual Edit artifact lost", err)
					}
				}
				if strings.Contains(string(raw), "native_session") || strings.Contains(string(raw), "fixture-controller-key") {
					t.Fatal("session or credential entered handoff")
				}
				if err := h.Close(); err != nil {
					t.Fatal("producer close", err)
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
				if mode == "implementation" {
					b, err := os.ReadFile(filepath.Join(next.Path, "created.txt"))
					if err != nil || string(b) != "synthetic factory file\n" {
						t.Fatal("restart lost actual Edit output", err)
					}
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
			if mode == "implementation" {
				mu.Lock()
				copyPath := spec.Workspace
				mu.Unlock()
				raw, err := os.ReadFile(filepath.Join(copyPath, "created.txt"))
				if err != nil || string(raw) != "synthetic factory file\n" {
					t.Fatal("real Edit did not produce copy", err)
				}
			}
			if _, err := os.Stat(filepath.Join(d.Projects[0].Path, "created.txt")); !os.IsNotExist(err) {
				t.Fatal("factory changed original", err)
			}
			raw, err = os.ReadFile(filepath.Join(d.Projects[0].Path, "original.txt"))
			if err != nil || string(raw) != "original\n" {
				t.Fatal("original content changed", err)
			}
			t.Logf("production factory -> HTTP/Controller/Store -> GLMAdapter -> fixed Native -> %d synthetic calls; actual stop and artifact/release outcome verified; no real account/quota/billing admission", calls.Load())
		})
	}
}

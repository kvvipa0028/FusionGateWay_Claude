//go:build darwin

package bootstrap

import (
	"context"
	"encoding/json"
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
	"github.com/yetone/magpie/internal/fusion/evidence"
	"github.com/yetone/magpie/internal/fusion/handoff"
	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
	"github.com/yetone/magpie/internal/fusion/workflow"
)

func TestGLMFactoryPinnedNativeConsumesApprovedParentAcrossRestart(t *testing.T) {
	if *hostNativeGLM == "" {
		t.Skip("explicit pinned Native fixture only")
	}
	for _, mode := range []string{"success", "testing_scope_violation", "parent_header_drift", "unscoped_external_write", "hard_test_failure", "malformed_test_report", "missing_verifier", "wrong_standard", "verifier_config_mutation"} {
		t.Run(mode, func(t *testing.T) {
			path, d, c, _, _ := glmFactoryFixture(t, true)
			c.Executable, _ = filepath.EvalSymlinks(*hostNativeGLM)
			c.TestingWritePaths = []string{"tests"}
			verifierMode := "pass"
			if mode == "hard_test_failure" {
				verifierMode = "fail"
			}
			if mode == "malformed_test_report" {
				verifierMode = "malformed"
			}
			c.Verification = glmVerificationFixture(t, verifierMode)
			if mode == "missing_verifier" {
				c.Verification = nil
			}
			if mode == "wrong_standard" {
				c.Verification.Acceptance[0] = "other approved standard"
			}
			expectedVerification := copyRuntimeValue(c.Verification)
			for _, dir := range []string{"src", "tests", "tests2"} {
				if err := os.Mkdir(filepath.Join(d.Projects[0].Path, dir), 0700); err != nil {
					t.Fatal(err)
				}
			}
			var mu sync.Mutex
			var activeRole stageplan.Role
			var spec managed.Spec
			var owned control.Execution
			var calls atomic.Int64
			rounds := map[stageplan.Role]int{}
			upstream := glmHostUpstream(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				mu.Lock()
				role, work := activeRole, spec.Workspace
				rounds[role]++
				n := rounds[role]
				mu.Unlock()
				write := ""
				if role == stageplan.Implementation && n == 1 {
					write = filepath.Join(work, "src/implemented.txt")
				}
				if role == stageplan.Testing && n == 1 {
					write = filepath.Join(work, "tests/generated.txt")
					if mode == "testing_scope_violation" {
						write = filepath.Join(work, "src/forbidden.txt")
					}
				}
				if role == stageplan.Testing && n == 2 && mode == "unscoped_external_write" {
					// A host-side fixture bypasses the Native kernel boundary to
					// prove publication independently checks the actual final delta.
					if err := os.WriteFile(filepath.Join(work, "src/forbidden.txt"), []byte("unapproved host mutation"), 0600); err != nil {
						t.Error(err)
					}
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(glmHostSSE(t, int64(n), write)))}, nil
			})
			factory, err := newGLMRuntimeFactory(c, upstream)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "verifier_config_mutation" {
				c.Verification.Spec.Args[len(c.Verification.Spec.Args)-1] = "fail"
				c.Verification.Acceptance[0] = "caller mutated"
			}
			c.TestingWritePaths[0] = "src" // Caller mutation must not broaden the frozen scope.
			wrapped := func(ctx context.Context, e RuntimeEnvironment) (RuntimeRegistration, error) {
				reg, err := factory(ctx, e)
				resolve := reg.Resolve
				reg.Resolve = func(ctx context.Context, task store.Task, role stageplan.Role, target stageplan.ExecutionTarget) (control.Launch, error) {
					launch, err := resolve(ctx, task, role, target)
					if err == nil {
						if role == stageplan.Implementation || role == stageplan.Testing {
							var prompt glmStagePrompt
							if json.Unmarshal(launch.Spec.Input, &prompt) != nil || prompt.Role != role || prompt.Workflow == nil || prompt.Workflow.Design == nil || prompt.Workflow.Approval == nil || prompt.Parent == nil || prompt.Parent.Binding.TaskID != task.ID || prompt.Workflow.Approval.DesignHash != prompt.Workflow.Design.Snapshot.Hash || prompt.Parent.Evidence != "unverified" {
								t.Error("Native prompt lost exact approved context or promoted evidence")
							}
						}
						mu.Lock()
						activeRole, spec = role, launch.Spec
						mu.Unlock()
						start := launch.Backend.Start
						launch.Backend.Start = func(ctx context.Context, run store.StageRun, spec managed.Spec) (control.Execution, error) {
							h, err := start(ctx, run, spec)
							mu.Lock()
							owned = h
							mu.Unlock()
							return h, err
						}
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
			code, raw, _ := hostHTTP(t, h, "POST", "/control/v1/tasks/preview", `{"project_id":"fixture-project","goal":"synthetic pipeline goal","required_roles":["design","implementation","testing","review","acceptance"]}`, "", "")
			var preview api.Preview
			if code != 200 || json.Unmarshal(raw, &preview) != nil {
				t.Fatal("preview", code)
			}
			body, _ := json.Marshal(api.SubmitRequest{PreviewID: preview.ID, PlanHash: preview.Plan.Hash})
			code, raw, _ = hostHTTP(t, h, "POST", "/agent/v1/tasks", string(body), "pipeline-submit", "")
			var task store.Task
			if code != 201 || json.Unmarshal(raw, &task) != nil {
				t.Fatal("submit", code)
			}
			baseURL := "/control/v1/tasks/" + task.ID
			code, _, _ = hostHTTP(t, h, "POST", baseURL+"/workflow", `{"kind":"change"}`, "", `"p1-g0-ready"`)
			if code != 200 {
				t.Fatal("attach", code)
			}
			start := func(role stageplan.Role) api.ExecutionReply {
				t.Helper()
				current, err := h.store.Task(task.ID)
				if err != nil {
					t.Fatal(err)
				}
				etag := fmt.Sprintf(`"p%d-g%d-%s"`, current.PlanRevision, current.Generation, current.State)
				code, raw, _ := hostHTTP(t, h, "POST", baseURL+"/start", fmt.Sprintf(`{"role":"%s"}`, role), "pipeline-"+string(role), etag)
				var reply api.ExecutionReply
				if code != 202 || json.Unmarshal(raw, &reply) != nil {
					t.Fatal("start", role, code, string(raw))
				}
				return reply
			}
			wait := func(reply api.ExecutionReply, success bool) {
				t.Helper()
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				done, err := h.controller.Wait(ctx, reply.Run.ID)
				if err != nil || !done.StoppedVerified || !done.Released || (done.State == "succeeded") != success {
					t.Fatal("actual stop/release", done, err)
				}
			}
			design := start(stageplan.Design)
			wait(design, true)
			body, _ = json.Marshal(api.WorkflowDesignRequest{RunID: design.Run.ID, Document: workflow.DesignDocument{Goal: task.Goal, Scope: []string{"src", "tests"}, Constraints: []string{"preserve original"}, Interfaces: []string{"stage handoff"}, Acceptance: []string{"genuine tests required"}}})
			code, raw, _ = hostHTTP(t, h, "POST", baseURL+"/workflow/design", string(body), "", `"p1-g1-ready"`)
			var view api.WorkflowReply
			if code != 200 || json.Unmarshal(raw, &view) != nil || view.Workflow == nil || view.Workflow.Design == nil {
				t.Fatal("freeze human design", code)
			}
			before := calls.Load()
			code, _, _ = hostHTTP(t, h, "POST", baseURL+"/start", `{"role":"implementation"}`, "unapproved", `"p1-g1-ready"`)
			if code != 409 || calls.Load() != before {
				t.Fatal("unapproved implementation launched", code)
			}
			body, _ = json.Marshal(api.ApproveWorkflowRequest{DesignHash: view.Workflow.Design.Snapshot.Hash, AcceptanceHash: view.Workflow.Design.Snapshot.AcceptanceHash})
			code, _, _ = hostHTTP(t, h, "POST", baseURL+"/workflow/approve", string(body), "", `"p1-g1-ready"`)
			if code != 200 {
				t.Fatal("explicit human approval", code)
			}
			implementation := start(stageplan.Implementation)
			wait(implementation, true)
			parent, err := h.store.Artifact(implementation.Run.ID)
			if err != nil || parent.ParentRunID != design.Run.ID {
				t.Fatal("implementation parent", err)
			}
			if err := h.Close(); err != nil {
				t.Fatal(err)
			}
			h, err = OpenExecutionControl(context.Background(), path, root, "127.0.0.1:0", wrapped)
			if err != nil {
				t.Fatal("actual host restart", err)
			}
			serveExecutionHost(t, h)
			if mode == "parent_header_drift" {
				if err := os.Chmod(filepath.Join(parent.Reference.Path, "handoff.json"), 0600); err != nil {
					t.Fatal(err)
				}
				before := calls.Load()
				code, _, _ := hostHTTP(t, h, "POST", baseURL+"/start", `{"role":"testing"}`, "bad-parent", `"p1-g2-ready"`)
				if code == 202 || calls.Load() != before {
					t.Fatal("tampered parent launched", code)
				}
				current, _ := h.store.Task(task.ID)
				if current.Generation != 2 || current.State != "ready" {
					t.Fatal("bad parent wrote a new intent")
				}
				return
			}
			if mode == "missing_verifier" || mode == "wrong_standard" {
				before := calls.Load()
				code, _, _ := hostHTTP(t, h, "POST", baseURL+"/start", `{"role":"testing"}`, "no-registered-verifier", `"p1-g2-ready"`)
				if code == 202 || calls.Load() != before {
					t.Fatal("missing/mismatched trusted verifier launched", code)
				}
				current, err := h.store.Task(task.ID)
				if err != nil || current.Generation != 2 || current.State != "ready" {
					t.Fatal("verifier rejection wrote intent", err)
				}
				return
			}
			testingRun := start(stageplan.Testing)
			mu.Lock()
			testingSpec := spec
			testingHandle := owned
			mu.Unlock()
			if len(testingSpec.WritePaths) != 1 || testingSpec.WritePaths[0] != "tests" {
				t.Fatal("testing scope was not frozen/narrowed")
			}
			if mode == "unscoped_external_write" {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				done, err := h.controller.Wait(ctx, testingRun.Run.ID)
				if err != nil || done.State != "succeeded" || done.Released || done.StoppedVerified {
					t.Fatal("unapproved delta published/released", done, err)
				}
				actual, err := testingHandle.Wait(ctx)
				if err != nil || !actual.StoppedVerified || !actual.Proof.DescendantsStopped {
					t.Fatal("scope rejection lost real stop proof", err)
				}
				if _, err := h.store.Reservation(testingRun.Run.ID); err != nil {
					t.Fatal("scope rejection released write lease", err)
				}
				if _, err := h.store.Artifact(testingRun.Run.ID); err == nil {
					t.Fatal("unapproved delta indexed")
				}
				return
			}
			wait(testingRun, mode != "testing_scope_violation")
			if mode == "testing_scope_violation" {
				if _, err := os.Stat(filepath.Join(testingSpec.Workspace, "src/forbidden.txt")); !os.IsNotExist(err) {
					t.Fatal("kernel allowed testing to edit product", err)
				}
				if _, err := h.store.Artifact(testingRun.Run.ID); err == nil {
					t.Fatal("failed test edit published successful artifact")
				}
				current, _ := h.store.Task(task.ID)
				if current.State != "needs_review" {
					t.Fatal("write violation did not stop workflow", current.State)
				}
				return
			}

			if mode == "hard_test_failure" || mode == "malformed_test_report" {
				current, err := h.store.Task(task.ID)
				if err != nil || current.State != "needs_review" {
					t.Fatal("Native success overrode hard verification failure", err, current.State)
				}
				stored, v, err := h.store.VerifiedArtifact(testingRun.Run.ID, d.Projects[0].Path, c.ExecutionRoot, expectedVerification.Spec)
				want := evidence.Failed
				if mode == "malformed_test_report" {
					want = evidence.Unverified
				}
				if err != nil || v.Status != want || stored.Verification == nil {
					t.Fatal("hard failure not recorded", err, v)
				}
				return
			}
			result, err := h.store.Artifact(testingRun.Run.ID)
			if err != nil || result.ParentRunID != implementation.Run.ID || result.InputTreeHash != parent.Reference.TreeHash || result.Reference.TreeHash == parent.Reference.TreeHash {
				t.Fatal("new testing revision/parent", err)
			}
			bundle, err := handoff.Restore(result.Reference, d.Projects[0].Path, c.ExecutionRoot)
			if err != nil {
				t.Fatal(err)
			}
			copy, err := bundle.Copy(result.Reference.Binding, c.ExecutionRoot, "verified-output")
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"src/implemented.txt", "tests/generated.txt"} {
				if b, err := os.ReadFile(filepath.Join(copy.Path, name)); err != nil || string(b) != "synthetic factory file\n" {
					t.Fatal("real stage changes lost", name, err)
				}
				if _, err := os.Stat(filepath.Join(d.Projects[0].Path, name)); !os.IsNotExist(err) {
					t.Fatal("original project changed", name)
				}
			}
			seen := map[string]bool{}
			for _, id := range []string{design.Run.ID, implementation.Run.ID, testingRun.Run.ID} {
				run, err := h.store.Run(id)
				if err != nil || run.NativeSessionID == "" || seen[run.NativeSessionID] {
					t.Fatal("stage reused session", err)
				}
				seen[run.NativeSessionID] = true
			}
			budget, err := h.store.Budget(task.ID)
			if err != nil || int64(budget.UsedCalls) != calls.Load() || calls.Load() != 5 {
				t.Fatal("stage budget reset/bypass", err, calls.Load())
			}

			if err = h.Close(); err != nil {
				t.Fatal(err)
			}
			h, err = OpenExecutionControl(context.Background(), path, root, "127.0.0.1:0", wrapped)
			if err != nil {
				t.Fatal("verified host restart", err)
			}
			serveExecutionHost(t, h)
			_, verified, err := h.store.VerifiedArtifact(testingRun.Run.ID, d.Projects[0].Path, c.ExecutionRoot, expectedVerification.Spec)
			if err != nil || verified.Status != evidence.Passed || verified.Tests != 2 {
				t.Fatal("restart lost actual verification", err, verified)
			}
			changed := expectedVerification.Spec
			changed.Rules.MinTests = 3
			if _, v, err := h.store.VerifiedArtifact(testingRun.Run.ID, d.Projects[0].Path, c.ExecutionRoot, changed); err != nil || v.Status != evidence.Superseded {
				t.Fatal("changed standard reused result", err, v)
			}
			before = calls.Load()
			code, _, _ = hostHTTP(t, h, "POST", baseURL+"/start", `{"role":"review"}`, "review-consumer-pending", `"p1-g3-ready"`)
			if code == 202 || calls.Load() != before {
				t.Fatal("missing review consumer allowed later workflow", code)
			}
		})
	}
}

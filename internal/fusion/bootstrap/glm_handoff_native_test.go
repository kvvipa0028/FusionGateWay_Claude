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
	"reflect"
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
	for _, mode := range []string{"independence_conflict", "rework_success", "rework_exhausted", "rework_retest_failure", "rework_budget", "rework_revoked", "rework_parent_drift", "rework_cancel", "success", "testing_scope_violation", "parent_header_drift", "unscoped_external_write", "hard_test_failure", "malformed_test_report", "missing_verifier", "wrong_standard", "verifier_config_mutation", "review_changes", "review_malformed", "review_write", "review_standard_changed", "review_no_verifier", "review_parent_drift", "acceptance_rejected", "acceptance_unverified", "acceptance_malformed", "acceptance_missing_criteria", "acceptance_write", "acceptance_standard_changed", "acceptance_parent_drift", "human_standard_changed", "human_reader_revoked", "human_commit_revoked"} {
		t.Run(mode, func(t *testing.T) {
			path, d, c, _, _ := glmFactoryFixture(t, true)
			c.Executable, _ = filepath.EvalSymlinks(*hostNativeGLM)
			c.TestingWritePaths = []string{"tests"}
			if mode == "independence_conflict" {
				d.Projects[0].Independence = []stageplan.RolePair{{First: stageplan.Design, Second: stageplan.Acceptance}}
				writeSource(t, path, d)
			}
			if mode == "review_changes" {
				zero := 0
				d.Projects[0].MaxReworks = &zero
				writeSource(t, path, d)
			}
			if mode == "rework_budget" {
				calls := 8
				d.Projects[0].MaxCalls = &calls
				writeSource(t, path, d)
			}
			verifierMode := "pass"
			if mode == "rework_retest_failure" {
				verifierMode = "repair-fail"
			}
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
			var activeAttempt int64
			var spec managed.Spec
			var owned control.Execution
			var calls atomic.Int64
			rounds := map[stageplan.Role]int{}
			upstream := glmHostUpstream(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				mu.Lock()
				role, work, attempt := activeRole, spec.Workspace, activeAttempt
				rounds[role]++
				n := rounds[role]
				mu.Unlock()
				write := ""
				if role == stageplan.Implementation && n == 1 {
					write = filepath.Join(work, "src/implemented.txt")
					if attempt == 2 {
						write = filepath.Join(work, "src/reworked.txt")
					}
				}
				if role == stageplan.Testing && n == 1 {
					write = filepath.Join(work, "tests/generated.txt")
					if attempt == 2 {
						write = filepath.Join(work, "tests/retested.txt")
					}
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
				if role == stageplan.Review {
					text := `{"version":1,"verdict":"approve","findings":[]}`
					if mode == "review_changes" || strings.HasPrefix(mode, "rework_") && (attempt == 1 || mode == "rework_exhausted") {
						text = `{"version":1,"verdict":"changes_required","findings":[{"id":"R1","severity":"blocking","summary":"synthetic defect requires implementation repair"}]}`
					}
					if mode == "review_malformed" {
						text = "model says accepted but no structured review"
					}
					if mode == "review_write" && n == 1 {
						write = filepath.Join(work, "src/reviewer-write.txt")
					}
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(glmHostSSEWithText(t, int64(n), write, text)))}, nil
				}
				if role == stageplan.Acceptance {
					text := `{"version":1,"verdict":"accepted","criteria":[{"index":0,"status":"met","reason":"actual frozen code, passed pinned verifier and review checked"}]}`
					switch mode {
					case "acceptance_rejected":
						text = `{"version":1,"verdict":"rejected","criteria":[{"index":0,"status":"not_met","reason":"synthetic acceptance defect"}]}`
					case "acceptance_unverified":
						text = `{"version":1,"verdict":"unverified","criteria":[{"index":0,"status":"unverified","reason":"required evidence not independently established"}]}`
					case "acceptance_malformed":
						text = "accepted but no actual criterion report"
					case "acceptance_missing_criteria":
						text = `{"version":1,"verdict":"accepted","criteria":[]}`
					case "acceptance_write":
						if n == 1 {
							write = filepath.Join(work, "src/acceptance-write.txt")
						}
					}
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(glmHostSSEWithText(t, int64(n), write, text)))}, nil
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
			var humanGuard atomic.Bool
			wrapped := func(ctx context.Context, e RuntimeEnvironment) (RuntimeRegistration, error) {
				reg, err := factory(ctx, e)
				readFinal := reg.FinalEvidence
				reg.FinalEvidence = func(ctx context.Context, task store.Task, run store.StageRun) (store.FinalEvidence, func() bool, error) {
					proof, current, err := readFinal(ctx, task, run)
					var checks atomic.Int64
					return proof, func() bool {
						if humanGuard.Load() && checks.Add(1) == 3 {
							if mode == "human_commit_revoked" {
								e.Manager.RevokeManagement()
								return current != nil && current()
							}
							return false
						}
						return current != nil && current()
					}, err
				}
				after := reg.AfterRelease
				reg.AfterRelease = func(call context.Context, run store.StageRun) (*control.Followup, error) {
					if run.Role == stageplan.Review && run.Attempt == 1 {
						if mode == "rework_revoked" {
							e.Manager.RevokeManagement()
						}
						if mode == "rework_parent_drift" {
							a, err := e.Store.Artifact(run.ID)
							if err != nil {
								t.Error(err)
							} else {
								name := filepath.Join(a.Reference.Path, "handoff.json")
								if err = os.Chmod(name, 0600); err != nil {
									t.Error(err)
								}
								if err = os.WriteFile(name, []byte("changed review header"), 0400); err != nil {
									t.Error(err)
								}
							}
						}
					}
					next, err := after(call, run)
					if mode == "rework_cancel" && next != nil {
						e.Manager.RevokeManagement()
					}
					return next, err
				}
				resolve := reg.Resolve
				reg.Resolve = func(ctx context.Context, task store.Task, role stageplan.Role, target stageplan.ExecutionTarget) (control.Launch, error) {
					launch, err := resolve(ctx, task, role, target)
					if err == nil {
						if role == stageplan.Implementation || role == stageplan.Testing {
							var prompt glmStagePrompt
							if json.Unmarshal(launch.Spec.Input, &prompt) != nil || prompt.Role != role || prompt.Workflow == nil || prompt.Workflow.Design == nil || prompt.Workflow.Approval == nil || prompt.Parent == nil || prompt.Parent.Binding.TaskID != task.ID || prompt.Workflow.Approval.DesignHash != prompt.Workflow.Design.Snapshot.Hash || prompt.Parent.Evidence != "unverified" {
								t.Error("Native prompt lost exact approved context or promoted evidence")
							}
							if role == stageplan.Implementation && prompt.Parent != nil && prompt.Parent.Binding.Role == stageplan.Review {
								if prompt.Review == nil || prompt.Review.RunID != prompt.Parent.Binding.RunID || prompt.Review.Document.Verdict != "changes_required" || len(prompt.Review.Document.Findings) != 1 || prompt.Review.Document.Findings[0].ID != "R1" || prompt.Review.Document.Findings[0].Summary != "synthetic defect requires implementation repair" || prompt.Review.TextHash == "" {
									t.Error("repair lost exact released review findings")
								}
							}
						}
						if role == stageplan.Review {
							var prompt glmStagePrompt
							if json.Unmarshal(launch.Spec.Input, &prompt) != nil || launch.Spec.Writable || len(launch.Spec.WritePaths) != 0 || prompt.Verification == nil || prompt.Verification.Tests != 2 || prompt.Verification.ExitCode != 0 || prompt.Parent == nil || prompt.Parent.Evidence != "passed" || prompt.Parent.TreeHash != prompt.Verification.ArtifactHash || prompt.ReviewResponseContract == "" {
								t.Error("review lost readonly current hard-evidence contract")
							}
						}
						if role == stageplan.Acceptance {
							var prompt glmStagePrompt
							if json.Unmarshal(launch.Spec.Input, &prompt) != nil || launch.Spec.Writable || len(launch.Spec.WritePaths) != 0 || prompt.Verification == nil || prompt.Verification.Tests != 2 || prompt.Review == nil || prompt.Review.Document.Verdict != "approve" || prompt.Parent == nil || prompt.Review.RunID != prompt.Parent.Binding.RunID || prompt.Parent.TreeHash != prompt.Verification.ArtifactHash || prompt.AcceptanceResponseContract == "" || prompt.Workflow.Design.Snapshot.AcceptanceHash != prompt.Verification.AcceptanceHash {
								t.Error("acceptance lost readonly exact review/test/approved-criteria contract")
							}
						}
						mu.Lock()
						activeRole, spec = role, launch.Spec
						mu.Unlock()
						start := launch.Backend.Start
						launch.Backend.Start = func(ctx context.Context, run store.StageRun, spec managed.Spec) (control.Execution, error) {
							mu.Lock()
							activeAttempt = run.Attempt
							rounds[role] = 0
							mu.Unlock()
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
			if mode == "independence_conflict" {
				if code != 422 || !strings.Contains(string(raw), "independence_conflict") || calls.Load() != 0 || owned != nil {
					t.Fatal("hard project conflict launched Native or failed implicitly", code)
				}
				return
			}
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
				before = calls.Load()
				etag := fmt.Sprintf(`"p1-g3-%s"`, current.State)
				code, _, _ = hostHTTP(t, h, "POST", baseURL+"/start", `{"role":"review"}`, "hard-failed-review", etag)
				if code == 202 || calls.Load() != before {
					t.Fatal("hard failure allowed review start", code)
				}
				code, _, _ = hostHTTP(t, h, "POST", baseURL+"/start", `{"role":"acceptance"}`, "hard-failed-acceptance", etag)
				if code == 202 || calls.Load() != before {
					t.Fatal("model acceptance bypassed actual hard failure", code)
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
			if mode == "review_standard_changed" || mode == "review_no_verifier" {
				updated := c
				updated.TestingWritePaths = []string{"tests"}
				updated.Verification = copyRuntimeValue(expectedVerification)
				if mode == "review_standard_changed" {
					updated.Verification.Spec.Rules.MinTests = 3
				} else {
					updated.Verification = nil
				}
				factory, err = newGLMRuntimeFactory(updated, upstream)
				if err != nil {
					t.Fatal(err)
				}
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
			if mode == "review_parent_drift" {
				name := filepath.Join(result.Reference.Path, "handoff.json")
				if err := os.Chmod(name, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(name, []byte("changed testing header"), 0400); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "review_standard_changed" || mode == "review_no_verifier" || mode == "review_parent_drift" {
				before = calls.Load()
				code, _, _ = hostHTTP(t, h, "POST", baseURL+"/start", `{"role":"review"}`, "refused-review", `"p1-g3-ready"`)
				current, _ := h.store.Task(task.ID)
				if code == 202 || calls.Load() != before || current.Generation != 3 || current.State != "ready" {
					t.Fatal("invalid review inputs spawned Native or wrote intent", code, current)
				}
				return
			}
			review := start(stageplan.Review)
			wait(review, mode != "review_write")
			if mode == "review_write" {
				mu.Lock()
				reviewSpec := spec
				mu.Unlock()
				if _, err := os.Stat(filepath.Join(reviewSpec.Workspace, "src/reviewer-write.txt")); !os.IsNotExist(err) {
					t.Fatal("reviewer acquired write authority", err)
				}
				if _, err := h.store.Artifact(review.Run.ID); err == nil {
					t.Fatal("failed review write published artifact")
				}
				return
			}
			if strings.HasPrefix(mode, "rework_") {
				deadline := time.Now().Add(20 * time.Second)
				for {
					current, err := h.store.Task(task.ID)
					if err != nil {
						t.Fatal(err)
					}
					wantGen := int64(0)
					wantRound := 1
					wantCalls := int64(0)
					switch mode {
					case "rework_exhausted":
						wantGen = 7
						wantCalls = 11
					case "rework_retest_failure":
						wantGen = 6
						wantCalls = 10
					case "rework_budget":
						wantGen = 6
						wantCalls = 8
					case "rework_revoked", "rework_parent_drift":
						wantGen = 4
						wantRound = 0
						wantCalls = 6
					case "rework_cancel":
						wantGen = 5
						wantCalls = 6
					}
					if wantGen != 0 && current.Generation == wantGen && current.State == "needs_review" {
						if err := h.controller.Close(context.Background()); err != nil {
							t.Fatal(err)
						}
						budget, _ := h.store.Budget(task.ID)
						if budget.UsedReworks != wantRound || int64(budget.UsedCalls) != calls.Load() || calls.Load() != wantCalls {
							t.Fatal("bounded repair did not stop with exact budget", mode, budget, calls.Load())
						}
						var stageCount int
						events, _ := h.store.Events(task.ID, 0)
						for _, ev := range events {
							if ev.Kind == "start_intent" {
								stageCount++
							}
						}
						expectedStages := int(wantGen)
						if mode == "rework_cancel" || mode == "rework_budget" {
							expectedStages--
						}
						if stageCount != expectedStages {
							t.Fatal("repair launched extra stage", stageCount, expectedStages)
						}
						return
					}
					if current.Generation == 8 && current.State == "advisory_only" {
						final, err := h.store.FinalAcceptanceRun(task.ID, store.TaskVersion{PlanRevision: current.PlanRevision, Generation: current.Generation, State: current.State})
						if err == nil {
							wait(api.ExecutionReply{Run: api.RunView{ID: final.ID}}, true)
							proof, err := h.store.PrepareFinalEvidence(final.ID, d.Projects[0].Path, c.ExecutionRoot, expectedVerification.Spec)
							if err != nil || proof.Report().Hard.Status != evidence.Passed {
								t.Fatal("rework lost actual hard evidence", err)
							}
							if _, hard, err := h.store.VerifiedArtifact(testingRun.Run.ID, d.Projects[0].Path, c.ExecutionRoot, expectedVerification.Spec); err != nil || hard.Status != evidence.Superseded {
								t.Fatal("repair reused old tests", hard, err)
							}
							budget, _ := h.store.Budget(task.ID)
							if budget.UsedReworks != 1 || int64(budget.UsedCalls) != calls.Load() || calls.Load() != 12 {
								t.Fatal("rework budget reset or hidden calls", budget, calls.Load())
							}
							sequence := []stageplan.Role{stageplan.Design, stageplan.Implementation, stageplan.Testing, stageplan.Review, stageplan.Implementation, stageplan.Testing, stageplan.Review, stageplan.Acceptance}
							events, _ := h.store.Events(task.ID, 0)
							n := 0
							sessions := map[string]bool{}
							lastParent := ""
							plan, _ := h.store.Plan(task.ID, 1)
							for _, ev := range events {
								if ev.Kind == "start_intent" {
									r, _ := h.store.Run(ev.RunID)
									a, err := h.store.Artifact(r.ID)
									if n >= len(sequence) || r.Role != sequence[n] || r.State != "succeeded" || r.Attempt > 2 || r.NativeSessionID == "" || sessions[r.NativeSessionID] || !reflect.DeepEqual(r.Target, *plan.Bindings[r.Role].Target) || err != nil || a.ParentRunID != lastParent {
										t.Fatal("repair changed binding/session/parent sequence", r.Role, r.Attempt, err)
									}
									sessions[r.NativeSessionID] = true
									lastParent = r.ID
									n++
								}
							}
							if n != 8 {
								t.Fatal("repair stage count", n)
							}
							accepted, err := h.store.Artifact(final.ID)
							if err != nil {
								t.Fatal(err)
							}
							bundle, err := handoff.Restore(accepted.Reference, d.Projects[0].Path, c.ExecutionRoot)
							if err != nil {
								t.Fatal(err)
							}
							copy, err := bundle.Copy(accepted.Reference.Binding, c.ExecutionRoot, "repaired-output")
							if err != nil {
								t.Fatal(err)
							}
							for _, name := range []string{"src/reworked.txt", "tests/retested.txt"} {
								if b, err := os.ReadFile(filepath.Join(copy.Path, name)); err != nil || string(b) != "synthetic factory file\n" {
									t.Fatal("actual repair/retest file missing", name, err)
								}
							}
							nativeHumanDecision(t, h, task.ID, "accept", "operator checked repaired tree and actual retest", true)
							return
						}
					}
					if time.Now().After(deadline) {
						events, _ := h.store.Events(task.ID, 0)
						for _, ev := range events {
							t.Log("repair diagnostic", ev.Kind, ev.Generation)
							if ev.Kind == "start_intent" {
								r, _ := h.store.Run(ev.RunID)
								t.Log("repair run", r.Role, r.Attempt, r.State)
								done, err := h.controller.Wait(context.Background(), r.ID)
								t.Log("repair completion", done, err)
							}
						}
						t.Fatal("actual rework chain did not reach current acceptance", current)
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
			reviewed, hard, err := h.store.ReviewedArtifact(review.Run.ID, d.Projects[0].Path, c.ExecutionRoot, expectedVerification.Spec)
			if err != nil || hard.Status != evidence.Passed || reviewed.Review == nil || reviewed.Review.TestingRunID != testingRun.Run.ID || reviewed.Reference.TreeHash != result.Reference.TreeHash {
				t.Fatal("review lost exact released testing evidence", err, hard)
			}
			current, _ := h.store.Task(task.ID)
			if mode == "review_changes" || mode == "review_malformed" {
				if current.State != "needs_review" || (mode == "review_malformed") == reviewed.Review.Valid || reviewed.Review.Document.Verdict == "approve" {
					t.Fatal("Native success overrode blocking/invalid model review", current, reviewed.Review)
				}
				before = calls.Load()
				code, _, _ = hostHTTP(t, h, "POST", baseURL+"/start", `{"role":"acceptance"}`, "blocked-review-acceptance", `"p1-g4-needs_review"`)
				if code == 202 || calls.Load() != before {
					t.Fatal("acceptance bypassed blocked/invalid review", code)
				}
				return
			}
			if !reviewed.Review.Valid || reviewed.Review.Document.Verdict != "approve" || current.State != "ready" {
				t.Fatal("advisory approval lost or falsely completed task", current)
			}
			reviewRun, _ := h.store.Run(review.Run.ID)
			if seen[reviewRun.NativeSessionID] || reviewRun.NativeSessionID == "" {
				t.Fatal("same model reused earlier stage session")
			}
			budget, _ = h.store.Budget(task.ID)
			if int64(budget.UsedCalls) != calls.Load() || calls.Load() != 6 {
				t.Fatal("review reset task budget", calls.Load(), budget)
			}
			if err := h.Close(); err != nil {
				t.Fatal(err)
			}
			if mode == "acceptance_standard_changed" {
				updated := c
				updated.TestingWritePaths = []string{"tests"}
				updated.Verification = copyRuntimeValue(expectedVerification)
				updated.Verification.Spec.Rules.MinTests = 3
				factory, err = newGLMRuntimeFactory(updated, upstream)
				if err != nil {
					t.Fatal(err)
				}
			}
			h, err = OpenExecutionControl(context.Background(), path, root, "127.0.0.1:0", wrapped)
			if err != nil {
				t.Fatal(err)
			}
			serveExecutionHost(t, h)
			if _, v, err := h.store.ReviewedArtifact(review.Run.ID, d.Projects[0].Path, c.ExecutionRoot, expectedVerification.Spec); err != nil || v.Status != evidence.Passed {
				t.Fatal("review restart lost bound evidence", err, v)
			}
			if mode == "acceptance_parent_drift" {
				name := filepath.Join(reviewed.Reference.Path, "handoff.json")
				if err := os.Chmod(name, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(name, []byte("changed review header"), 0400); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "acceptance_standard_changed" || mode == "acceptance_parent_drift" {
				before = calls.Load()
				code, _, _ = hostHTTP(t, h, "POST", baseURL+"/start", `{"role":"acceptance"}`, "refused-acceptance", `"p1-g4-ready"`)
				current, _ := h.store.Task(task.ID)
				if code == 202 || calls.Load() != before || current.Generation != 4 || current.State != "ready" {
					t.Fatal("invalid acceptance inputs spawned Native or wrote intent", code, current)
				}
				return
			}
			acceptance := start(stageplan.Acceptance)
			wait(acceptance, mode != "acceptance_write")
			if mode == "acceptance_write" {
				mu.Lock()
				acceptanceSpec := spec
				mu.Unlock()
				if _, err := os.Stat(filepath.Join(acceptanceSpec.Workspace, "src/acceptance-write.txt")); !os.IsNotExist(err) {
					t.Fatal("acceptance model acquired write authority", err)
				}
				if _, err := h.store.Artifact(acceptance.Run.ID); err == nil {
					t.Fatal("failed acceptance write registered decision")
				}
				return
			}
			accepted, hard, err := h.store.AcceptanceArtifact(acceptance.Run.ID, d.Projects[0].Path, c.ExecutionRoot, expectedVerification.Spec)
			if err != nil || hard.Status != evidence.Passed || accepted.Acceptance == nil || accepted.Acceptance.ReviewRunID != review.Run.ID || accepted.Acceptance.TestingRunID != testingRun.Run.ID || accepted.Reference.TreeHash != result.Reference.TreeHash {
				t.Fatal("acceptance lost exact actual review/test inputs", err, hard)
			}
			current, _ = h.store.Task(task.ID)
			if strings.HasPrefix(mode, "acceptance_") {
				if current.State != "needs_review" || accepted.Acceptance.Document.Verdict == "accepted" {
					t.Fatal("Native success overrode invalid/rejected/unverified acceptance", current)
				}
				before := calls.Load()
				decision := nativeHumanDecision(t, h, task.ID, "return", "operator requires missing evidence or corrections", mode == "acceptance_rejected")
				if decision.Action != "return" || calls.Load() != before {
					t.Fatal("human return started a Native or lost exact receipt")
				}
				return
			}
			if !accepted.Acceptance.Valid || accepted.Acceptance.Document.Verdict != "accepted" || current.State != "advisory_only" {
				t.Fatal("model opinion falsely completed task or lost advisory status", current)
			}
			acceptedRun, _ := h.store.Run(acceptance.Run.ID)
			if acceptedRun.NativeSessionID == "" || seen[acceptedRun.NativeSessionID] || acceptedRun.NativeSessionID == reviewRun.NativeSessionID {
				t.Fatal("acceptance reused prior Native session")
			}
			budget, _ = h.store.Budget(task.ID)
			if int64(budget.UsedCalls) != calls.Load() || calls.Load() != 7 {
				t.Fatal("acceptance reset task budget", calls.Load(), budget)
			}
			if err := h.Close(); err != nil {
				t.Fatal(err)
			}
			h, err = OpenExecutionControl(context.Background(), path, root, "127.0.0.1:0", wrapped)
			if err != nil {
				t.Fatal(err)
			}
			serveExecutionHost(t, h)
			if _, v, err := h.store.AcceptanceArtifact(acceptance.Run.ID, d.Projects[0].Path, c.ExecutionRoot, expectedVerification.Spec); err != nil || v.Status != evidence.Passed {
				t.Fatal("acceptance restart lost bound evidence", err, v)
			}
			if _, v, err := h.store.AcceptanceArtifact(acceptance.Run.ID, d.Projects[0].Path, c.ExecutionRoot, changed); err != nil || v.Status != evidence.Superseded {
				t.Fatal("final decision reused obsolete test standard", err, v)
			}
			if mode == "human_reader_revoked" || mode == "human_commit_revoked" {
				verifyNativeHumanRevoked(t, h, task.ID, mode == "human_commit_revoked", func() { humanGuard.Store(true) })
				return
			}
			action := "accept"
			if mode == "human_standard_changed" {
				if err := h.Close(); err != nil {
					t.Fatal(err)
				}
				c.Verification = copyRuntimeValue(expectedVerification)
				c.Verification.Spec = changed
				factory, err = newGLMRuntimeFactory(c, upstream)
				if err != nil {
					t.Fatal(err)
				}
				h, err = OpenExecutionControl(context.Background(), path, root, "127.0.0.1:0", wrapped)
				if err != nil {
					t.Fatal(err)
				}
				serveExecutionHost(t, h)
				action = "return"
			}
			before = calls.Load()
			beforeBudget, _ := h.store.Budget(task.ID)
			beforeRun, _ := h.store.Run(acceptance.Run.ID)
			decision := nativeHumanDecision(t, h, task.ID, action, "operator checked actual code and frozen criteria", mode == "success" || mode == "human_standard_changed")
			if decision.Action != action || decision.RunID != acceptance.Run.ID || calls.Load() != before {
				t.Fatal("explicit human decision lost actual released origin")
			}
			current, _ = h.store.Task(task.ID)
			current, _ = h.store.Task(task.ID)
			afterBudget, _ := h.store.Budget(task.ID)
			afterRun, _ := h.store.Run(acceptance.Run.ID)
			wantState := "completed"
			if action == "return" {
				wantState = "needs_review"
			}
			if current.State != wantState || beforeBudget != afterBudget || !reflect.DeepEqual(beforeRun, afterRun) {
				t.Fatal("human acceptance changed Native/budget or did not complete", current)
			}
			if err := h.Close(); err != nil {
				t.Fatal(err)
			}
			h, err = OpenExecutionControl(context.Background(), path, root, "127.0.0.1:0", wrapped)
			if err != nil {
				t.Fatal(err)
			}
			serveExecutionHost(t, h)
			if persisted, err := h.store.HumanDecision(task.ID); err != nil || persisted != decision {
				t.Fatal("restart lost separate actual human decision", err)
			}
			verifyNativeHumanReadback(t, h, task.ID, decision)

		})
	}
}

//go:build darwin || linux

package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/api"
	"github.com/yetone/magpie/internal/fusion/control"
	"github.com/yetone/magpie/internal/fusion/policy"
	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
	"github.com/yetone/magpie/internal/fusion/workspace"
)

func nativeControlRequest(b *NativeStageBridge, method, path, body, key, tag string) *httptest.ResponseRecorder {
	r := nativeRequest(method, path, body)
	r.Header.Set("Content-Type", "application/json")
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	if tag != "" {
		r.Header.Set("If-Match", tag)
	}
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	return w
}
func nativeControlTag(task store.Task) string {
	return fmt.Sprintf(`"p%d-g%d-%s"`, task.PlanRevision, task.Generation, task.State)
}
func nativeControlReply(t *testing.T, w *httptest.ResponseRecorder) api.TaskControlReply {
	t.Helper()
	var out api.TaskControlReply
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Task.ID == "" || w.Header().Get("X-Fusion-Task-ETag") != nativeControlTag(out.Task) {
		t.Fatal("control body/header mismatch", w.Code)
	}
	return out
}

func TestNativeTaskControlIdleConditionalCycle(t *testing.T) {
	path, _ := sourceFixture(t)
	var calls atomic.Int64
	h, e := OpenExecutionControl(context.Background(), path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0", func(_ context.Context, env RuntimeEnvironment) (RuntimeRegistration, error) {
		reg := hostRegistration(env)
		reg.Inspect = func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (policy.Inspection, error) {
			calls.Add(1)
			return policy.Inspection{}, control.ErrUnsupported
		}
		return reg, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	serveExecutionHost(t, h)
	b, e := newNativeTestBridge(t, h)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	task := hostCreateTask(t, h)
	base := "/control/v1/tasks/" + task.ID
	for _, step := range []struct {
		action, state string
		generation    int64
		changed       bool
	}{{"pause", "paused", 1, true}, {"pause", "paused", 1, false}, {"continue", "ready", 2, true}, {"continue", "ready", 2, false}, {"cancel", "cancelled", 3, true}, {"cancel", "cancelled", 3, false}} {
		w := nativeControlRequest(b, "POST", base+"/"+step.action, `{}`, "", nativeControlTag(task))
		if w.Code != 200 {
			t.Fatal(step.action, w.Code)
		}
		out := nativeControlReply(t, w)
		if out.Task.ID != task.ID || out.Task.State != step.state || out.Task.Generation != step.generation || out.Changed != step.changed || out.Run != nil {
			t.Fatal("wrong idle transition", step.action)
		}
		task = out.Task
	}
	if calls.Load() != 0 {
		t.Fatal("idle controls inspected Runtime")
	}
	w := nativeControlRequest(b, "POST", base+"/continue", `{}`, "", nativeControlTag(task))
	if w.Code != 412 {
		t.Fatal("cancelled task resumed", w.Code)
	}
}

func TestNativeTaskControlOwnedRunAndOriginalStartRetry(t *testing.T) {
	for _, action := range []string{"run-cancel", "pause", "cancel"} {
		t.Run(action, func(t *testing.T) {
			path, _ := sourceFixture(t)
			var starts, resolves, releases atomic.Int64
			h, e := OpenExecutionControl(context.Background(), path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0", func(_ context.Context, env RuntimeEnvironment) (RuntimeRegistration, error) {
				reg := hostRegistration(env)
				p, _ := env.Source.Project("fixture-project")
				snapshot, e := workspace.Copy(p.Path, privateExecutionDir(t), "copy")
				if e != nil {
					t.Fatal(e)
				}
				guard, e := snapshot.Guard()
				if e != nil {
					t.Fatal(e)
				}
				reg.Resolve = func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (control.Launch, error) {
					resolves.Add(1)
					return control.Launch{Spec: managed.Spec{Root: privateExecutionDir(t), Workspace: snapshot.Path, Source: guard, Input: []byte("synthetic Native bridge"), Timeout: time.Second}, Backend: control.Backend{
						Probe: func(context.Context) (managed.Capabilities, error) {
							return managed.Capabilities{Probe: true, Start: true, Events: true, Cancel: true}, nil
						},
						Start: func(ctx context.Context, run store.StageRun, _ managed.Spec) (control.Execution, error) {
							starts.Add(1)
							if e := env.Store.ConfirmStarted(run.ID, run.Generation, run.Owner, "synthetic-bridge-session"); e != nil {
								return nil, e
							}
							execution := &hostExecution{done: make(chan struct{}), cancelled: make(chan struct{})}
							go func() {
								select {
								case <-execution.cancelled:
								case <-ctx.Done():
									execution.Cancel()
								}
								if e := env.Store.Finish(run.ID, run.Generation, run.Owner, "cancelled"); e != nil {
									t.Error(e)
								}
								execution.result = managed.Result{State: "cancelled", StoppedVerified: true, Proof: policy.StopProof{RunID: run.ID, Generation: run.Generation, NativeSessionID: "synthetic-bridge-session", ProcessIdentityHash: strings.Repeat("b", 64), ReportHash: strings.Repeat("c", 64), DescendantsStopped: true}}
								close(execution.done)
							}()
							return execution, nil
						}, Release: func(proof policy.StopProof) error {
							err := env.Store.ReleaseReserved(proof.RunID, proof.Generation, proof.ReportHash, true)
							if err == nil {
								releases.Add(1)
							}
							return err
						},
					}}, nil
				}
				return reg, nil
			})
			if e != nil {
				t.Fatal(e)
			}
			serveExecutionHost(t, h)
			b, e := newNativeTestBridge(t, h)
			if e != nil {
				t.Fatal(e)
			}
			defer b.Close()
			task := hostCreateTask(t, h)
			original := nativeControlTag(task)
			base := "/control/v1/tasks/" + task.ID
			w := nativeControlRequest(b, "POST", base+"/start", `{"role":"design"}`, "synthetic-native-original-start", original)
			var first api.ExecutionReply
			if w.Code != 202 || json.Unmarshal(w.Body.Bytes(), &first) != nil || !first.Created || first.Run.TaskID != task.ID || first.Run.ID == "" || w.Header().Get("X-Fusion-Task-ETag") == "" {
				t.Fatal("Native start missing", w.Code)
			}
			if strings.Contains(w.Body.String(), "synthetic-bridge-session") || w.Header().Get("Location") != "" {
				t.Fatal("private/unchecked metadata disclosed")
			}
			runPath := "/agent/v1/tasks/" + task.ID + "/runs/" + first.Run.ID
			read := nativeControlRequest(b, "GET", runPath, "", "", "")
			var readReply api.ExecutionReply
			if read.Code != 200 || json.Unmarshal(read.Body.Bytes(), &readReply) != nil || readReply.Run.ID != first.Run.ID || readReply.Run.TaskID != task.ID {
				t.Fatal("owned run read", read.Code)
			}
			run := readReply.Run
			current, e := h.store.Task(task.ID)
			if e != nil {
				t.Fatal(e)
			}
			w = nativeControlRequest(b, "POST", base+func() string {
				if action == "run-cancel" {
					return "/runs/" + run.ID + "/cancel"
				}
				return "/" + action
			}(), `{}`, "", nativeControlTag(current))
			if w.Code != 202 {
				t.Fatal("owned run cancel", w.Code)
			}
			deadline := time.Now().Add(time.Second)
			for releases.Load() != 1 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if releases.Load() != 1 {
				t.Fatal("owned synthetic release missing")
			}
			current, e = h.store.Task(task.ID)
			if e != nil {
				t.Fatal(e)
			}
			if action != "pause" && current.State != "cancelled" || action == "pause" && current.State != "needs_review" {
				t.Fatal("stopping intent became misleading completion", action, current.State)
			}
			w = nativeControlRequest(b, "POST", base+"/start", `{"role":"design"}`, "synthetic-native-original-start", original)
			var repeat api.ExecutionReply
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &repeat) != nil || repeat.Created || repeat.Run.ID != first.Run.ID || starts.Load() != 1 || resolves.Load() != 1 {
				t.Fatal("original start replayed", w.Code)
			}
			// A run ID cannot be read or cancelled through another registered task.
			plan, e := h.store.Plan(task.ID, task.PlanRevision)
			if e != nil {
				t.Fatal(e)
			}
			other, e := h.store.Create("another-native-task", store.CreateRequest{ProjectID: "fixture-project", Goal: "another registered task", Plan: plan})
			if e != nil || other.ID == task.ID {
				t.Fatal("another task setup", e)
			}
			for _, route := range []struct{ method, path, body string }{{"GET", "/agent/v1/tasks/" + other.ID + "/runs/" + run.ID, ""}, {"POST", "/control/v1/tasks/" + other.ID + "/runs/" + run.ID + "/cancel", `{}`}} {
				w = nativeControlRequest(b, route.method, route.path, route.body, "", nativeControlTag(other))
				if w.Code != 404 || strings.Contains(w.Body.String(), run.ID) {
					t.Fatal("cross-task run disclosed", w.Code)
				}
			}
		})
	}
}

func TestNativeTaskControlBoundariesAndUnadmittedHost(t *testing.T) {
	path, source := sourceFixture(t)
	h, e := OpenExecutionControl(context.Background(), path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0", func(_ context.Context, env RuntimeEnvironment) (RuntimeRegistration, error) {
		return hostRegistration(env), nil
	})
	if e != nil {
		t.Fatal(e)
	}
	serveExecutionHost(t, h)
	b, e := newNativeTestBridge(t, h)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	task := hostCreateTask(t, h)
	base := "/control/v1/tasks/" + task.ID
	for _, test := range []struct {
		name, method, path, body, key, tag string
		code                               int
	}{
		{"missing-condition", "POST", base + "/pause", `{}`, "", "", 428},
		{"weak-condition", "POST", base + "/cancel", `{}`, "", `W/"p1-g0-ready"`, 400},
		{"stale-condition", "POST", base + "/continue", `{}`, "", `"p1-g1-ready"`, 412},
		{"missing-key", "POST", base + "/start", `{"role":"design"}`, "", nativeControlTag(task), 400},
		{"privileged-body", "POST", base + "/pause", `{"owner":"unsafe"}`, "", nativeControlTag(task), 400},
		{"GET-action", "GET", base + "/start", "", "", "", 404},
		{"PUT-action", "PUT", base + "/pause", `{}`, "", nativeControlTag(task), 404},
		{"extra-segment", "POST", base + "/cancel/extra", `{}`, "", nativeControlTag(task), 404},
		{"not-resume", "POST", base + "/resume", `{}`, "", nativeControlTag(task), 404},
		{"not-checkpoint", "POST", base + "/runs/run-fixture/checkpoint", `{}`, "", nativeControlTag(task), 404},
		{"not-events", "GET", "/agent/v1/tasks/" + task.ID + "/events", "", "", "", 404},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := nativeControlRequest(b, test.method, test.path, test.body, test.key, test.tag)
			if w.Code != test.code {
				t.Fatal("boundary", w.Code)
			}
		})
	}
	for _, header := range []string{"If-Match", "Idempotency-Key"} {
		r := nativeRequest("POST", base+"/start", `{"role":"design"}`)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("If-Match", nativeControlTag(task))
		r.Header.Set("Idempotency-Key", "duplicate-start-key")
		r.Header.Add(header, r.Header.Get(header))
		w := httptest.NewRecorder()
		b.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal("duplicate header lost", header, w.Code)
		}
	}
	r := nativeRequest("POST", base+"/pause", `{}`)
	r.Header.Set("X-Wails-Window-Id", "2")
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("other window", w.Code)
	}
	plan, e := h.store.Plan(task.ID, task.PlanRevision)
	if e != nil {
		t.Fatal(e)
	}
	foreign, e := h.store.Create("foreign-native-control", store.CreateRequest{ProjectID: "unregistered", Goal: "private-control-goal", Plan: plan})
	if e != nil {
		t.Fatal(e)
	}
	for _, id := range []string{foreign.ID, "missing-task"} {
		for _, action := range []string{"start", "pause", "continue", "cancel", "runs/run-fixture/cancel"} {
			w := nativeControlRequest(b, "POST", "/control/v1/tasks/"+id+"/"+action, `{}`, "synthetic-key", nativeControlTag(task))
			if w.Code != 404 || strings.Contains(w.Body.String(), "private-control-goal") {
				t.Fatal("unregistered task control", action, w.Code)
			}
		}
	}
	events, e := h.store.Events(task.ID, 0)
	if e != nil || len(events) != 1 {
		t.Fatal("rejected control mutated task", e)
	}
	source.Revision++
	writeSource(t, path, source)
	w = nativeControlRequest(b, "POST", base+"/cancel", `{}`, "", nativeControlTag(task))
	if w.Code != 503 || strings.Contains(w.Body.String(), task.ID) {
		t.Fatal("revoked source control", w.Code)
	}
}

func TestNativeTaskControlDraftHostStaysUnavailable(t *testing.T) {
	path, _ := sourceFixture(t)
	h, e := OpenControl(path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	serveExecutionHost(t, h)
	b, e := newNativeTestBridge(t, h)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	// Obtain a valid frozen test plan from an independent synthetic host; the
	// draft host itself cannot admit a real preview or register a Controller.
	otherPath, _ := sourceFixture(t)
	other, e := OpenExecutionControl(context.Background(), otherPath, filepath.Join(filepath.Dir(otherPath), "control"), "127.0.0.1:0", func(_ context.Context, env RuntimeEnvironment) (RuntimeRegistration, error) {
		return hostRegistration(env), nil
	})
	if e != nil {
		t.Fatal(e)
	}
	serveExecutionHost(t, other)
	sourceTask := hostCreateTask(t, other)
	plan, e := other.store.Plan(sourceTask.ID, sourceTask.PlanRevision)
	if e != nil {
		t.Fatal(e)
	}
	task, e := h.store.Create("draft-native-control", store.CreateRequest{ProjectID: "fixture-project", Goal: "draft remains unavailable", Plan: plan})
	if e != nil {
		t.Fatal(e)
	}
	for _, action := range []string{"start", "pause", "continue", "cancel"} {
		w := nativeControlRequest(b, "POST", "/control/v1/tasks/"+task.ID+"/"+action, `{}`, "synthetic-key", nativeControlTag(task))
		if w.Code != 503 {
			t.Fatal("draft gained control", action, w.Code)
		}
	}
	current, e := h.store.Task(task.ID)
	if e != nil || current.State != "ready" || current.Generation != 0 {
		t.Fatal("draft mutated", e)
	}
}

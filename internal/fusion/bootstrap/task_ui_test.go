//go:build darwin || linux

package bootstrap

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yetone/magpie/internal/fusion/api"
	"github.com/yetone/magpie/internal/fusion/control"
	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

func TestNativeTaskUISubmitsFrozenPreviewAndReadsOnlyRegisteredTask(t *testing.T) {
	path, source := sourceFixture(t)
	var calls atomic.Int64
	h, err := OpenExecutionControl(context.Background(), path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0", func(_ context.Context, env RuntimeEnvironment) (RuntimeRegistration, error) {
		reg := hostRegistration(env)
		reg.Inspect = func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (policy.Inspection, error) {
			calls.Add(1)
			return policy.Inspection{}, control.ErrUnsupported
		}
		reg.Resolve = func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (control.Launch, error) {
			calls.Add(1)
			return control.Launch{}, control.ErrUnsupported
		}
		return reg, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	serveExecutionHost(t, h)
	b, err := newNativeTestBridge(t, h)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	code, raw, _ := hostHTTP(t, h, "POST", "/control/v1/tasks/preview", `{"project_id":"fixture-project","goal":"native task UI","required_roles":["design"]}`, "", "")
	var preview api.Preview
	if code != 200 || json.Unmarshal(raw, &preview) != nil {
		t.Fatal("preview", code)
	}
	payload, _ := json.Marshal(api.SubmitRequest{PreviewID: preview.ID, PlanHash: preview.Plan.Hash})
	var created store.Task
	for i := 0; i < 2; i++ {
		request := nativeRequest("POST", "/agent/v1/tasks", string(payload))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "native-ui-frozen-retry")
		w := httptest.NewRecorder()
		b.ServeHTTP(w, request)
		var task store.Task
		if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &task) != nil || task.State != "ready" || task.ProjectID != "fixture-project" {
			t.Fatal("native frozen submit", w.Code)
		}
		if i == 1 && task.ID != created.ID {
			t.Fatal("retry created a different task")
		}
		created = task
	}
	for _, path := range []string{"/agent/v1/tasks/" + created.ID, "/control/v1/tasks/" + created.ID + "/plan", "/control/v1/tasks/" + created.ID + "/budget"} {
		w := httptest.NewRecorder()
		b.ServeHTTP(w, nativeRequest("GET", path, ""))
		if w.Code != 200 || w.Body.Len() == 0 {
			t.Fatal("task read", path, w.Code)
		}
		request := nativeRequest("GET", path, "")
		request.Header.Set("X-Wails-Window-Id", "2")
		w = httptest.NewRecorder()
		b.ServeHTTP(w, request)
		if w.Code != 403 {
			t.Fatal("task read accepted another window", w.Code)
		}
	}
	for _, duplicate := range []bool{false, true} {
		request := nativeRequest("POST", "/agent/v1/tasks", string(payload))
		request.Header.Set("Content-Type", "application/json")
		if duplicate {
			request.Header.Add("Idempotency-Key", "native-ui-frozen-retry")
			request.Header.Add("Idempotency-Key", "native-ui-frozen-retry")
		}
		w := httptest.NewRecorder()
		b.ServeHTTP(w, request)
		if w.Code != 400 {
			t.Fatal("invalid submit header accepted", w.Code)
		}
	}
	foreign, err := h.store.Create("foreign-ui", store.CreateRequest{ProjectID: "not-registered", Goal: "private foreign task", Plan: preview.Plan})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{foreign.ID, "missing"} {
		for _, path := range []string{"/agent/v1/tasks/" + id, "/control/v1/tasks/" + id + "/plan", "/control/v1/tasks/" + id + "/budget"} {
			w := httptest.NewRecorder()
			b.ServeHTTP(w, nativeRequest("GET", path, ""))
			if w.Code != 404 || strings.Contains(w.Body.String(), "private foreign") {
				t.Fatal("task scope", path, w.Code)
			}
		}
	}
	for _, path := range []string{"/agent/v1/tasks/" + created.ID, "/control/v1/tasks/" + created.ID + "/start", "/control/v1/tasks/" + created.ID + "/cancel", "/agent/v1/tasks/" + created.ID + "/events"} {
		w := httptest.NewRecorder()
		b.ServeHTTP(w, nativeRequest("POST", path, "{}"))
		if w.Code != 404 {
			t.Fatal("unexpected task control", path, w.Code)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("submit or read queried/launched Runtime")
	}
	events, err := h.store.Events(created.ID, 0)
	if err != nil || len(events) != 1 || events[0].Kind != "created" {
		t.Fatal("unexpected execution events", err)
	}
	source.Revision++
	writeSource(t, path, source)
	w := httptest.NewRecorder()
	b.ServeHTTP(w, nativeRequest("GET", "/agent/v1/tasks/"+created.ID, ""))
	if w.Code != 503 || strings.Contains(w.Body.String(), created.ID) {
		t.Fatal("source revocation", w.Code)
	}
}

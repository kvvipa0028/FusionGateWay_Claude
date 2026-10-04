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

	"github.com/yetone/magpie/internal/fusion/api"
	"github.com/yetone/magpie/internal/fusion/control"
	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

func TestNativeStageBridgeTaskIndexReadOnlyAndSourceBound(t *testing.T) {
	path, source := sourceFixture(t)
	h, err := OpenControl(path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serveExecutionHost(t, h)
	b, err := newNativeTestBridge(t, h)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	w := httptest.NewRecorder()
	b.ServeHTTP(w, nativeRequest("GET", "/control/v1/projects/fixture-project/tasks", ""))
	var out api.TaskListReply
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Tasks == nil || len(out.Tasks) != 0 {
		t.Fatal("native list bridge", w.Code)
	}
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/control/v1/projects/fixture-project/tasks/before/missing", 404},
		{"POST", "/control/v1/projects/fixture-project/tasks", 404}, {"PUT", "/control/v1/projects/fixture-project/tasks", 404},
		{"GET", "/control/v1/projects/other/tasks", 404}, {"GET", "/control/v1/projects/fixture-project/tasks/start", 404},
		{"GET", "/control/v1/projects/fixture-project/tasks/before/missing/extra", 404},
	} {
		w := httptest.NewRecorder()
		b.ServeHTTP(w, nativeRequest(tc.method, tc.path, ""))
		if w.Code != tc.status {
			t.Fatal("native scope", tc.path, w.Code)
		}
	}
	source.Revision++
	writeSource(t, path, source)
	w = httptest.NewRecorder()
	b.ServeHTTP(w, nativeRequest("GET", "/control/v1/projects/fixture-project/tasks", ""))
	if w.Code != 503 || strings.Contains(w.Body.String(), "tasks") {
		t.Fatal("revoked native source", w.Code)
	}
}

func TestControlHostTaskIndexListsSubmittedTasksWithoutDispatch(t *testing.T) {
	path, source := sourceFixture(t)
	var inspections, launches atomic.Int64
	h, err := OpenExecutionControl(context.Background(), path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0", func(_ context.Context, env RuntimeEnvironment) (RuntimeRegistration, error) {
		r := hostRegistration(env)
		inspect, resolve := r.Inspect, r.Resolve
		r.Inspect = func(ctx context.Context, task store.Task, role stageplan.Role, target stageplan.ExecutionTarget) (policy.Inspection, error) {
			inspections.Add(1)
			return inspect(ctx, task, role, target)
		}
		r.Resolve = func(ctx context.Context, task store.Task, role stageplan.Role, target stageplan.ExecutionTarget) (control.Launch, error) {
			launches.Add(1)
			return resolve(ctx, task, role, target)
		}
		return r, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	serveExecutionHost(t, h)
	var ids []string
	for i := 0; i < 33; i++ {
		// A preview belongs to one submission receipt. Each distinct task needs
		// a new preview; changing the key of a committed preview is rejected.
		code, raw, _ := hostHTTP(t, h, "POST", "/control/v1/tasks/preview", `{"project_id":"fixture-project","goal":"index fixture","required_roles":["design"]}`, "", "")
		var preview api.Preview
		if code != 200 || json.Unmarshal(raw, &preview) != nil {
			t.Fatal("preview", code)
		}
		body, _ := json.Marshal(api.SubmitRequest{PreviewID: preview.ID, PlanHash: preview.Plan.Hash})
		code, raw, _ = hostHTTP(t, h, "POST", "/agent/v1/tasks", string(body), fmt.Sprintf("index-submit-%d", i), "")
		var task store.Task
		if code != 201 || json.Unmarshal(raw, &task) != nil {
			t.Fatal("submit", code)
		}
		ids = append(ids, task.ID)
	}
	indexPath := "/control/v1/projects/fixture-project/tasks"
	code, raw, headers := hostHTTP(t, h, "GET", indexPath, "", "", "")
	var out api.TaskListReply
	if code != 200 || json.Unmarshal(raw, &out) != nil || len(out.Tasks) != 32 || out.NextBefore != ids[1] || headers.Get("Cache-Control") != "no-store" {
		t.Fatal("loopback index", code)
	}
	b, err := newNativeTestBridge(t, h)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	w := httptest.NewRecorder()
	b.ServeHTTP(w, nativeRequest("GET", indexPath+"/before/"+out.NextBefore, ""))
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Tasks) != 1 || out.Tasks[0].ID != ids[0] || out.NextBefore != "" {
		t.Fatal("native older page", w.Code)
	}
	if inspections.Load() != 0 || launches.Load() != 0 {
		t.Fatal("listing dispatched or inspected")
	}
	for _, id := range ids {
		events, err := h.store.Events(id, 0)
		if err != nil || len(events) != 1 || events[0].Kind != "created" {
			t.Fatal("index created execution events", err)
		}
	}
	source.Revision++
	writeSource(t, path, source)
	code, raw, _ = hostHTTP(t, h, "GET", indexPath, "", "", "")
	if code != 503 || strings.Contains(string(raw), ids[0]) {
		t.Fatal("revoked loopback source", code)
	}
}

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

func TestSubmissionReceiptOwnedHostRestartThroughNativeBridge(t *testing.T) {
	path, source := sourceFixture(t)
	root := filepath.Join(filepath.Dir(path), "control")
	var calls atomic.Int64
	factory := func(_ context.Context, env RuntimeEnvironment) (RuntimeRegistration, error) {
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
	}
	h, e := OpenExecutionControl(context.Background(), path, root, "127.0.0.1:0", factory)
	if e != nil {
		t.Fatal(e)
	}
	serveExecutionHost(t, h)
	code, raw, _ := hostHTTP(t, h, "POST", "/control/v1/tasks/preview", `{"project_id":"fixture-project","goal":"restart receipt","required_roles":["design"]}`, "", "")
	var p api.Preview
	if code != 200 || json.Unmarshal(raw, &p) != nil {
		t.Fatal("preview", code)
	}
	body, _ := json.Marshal(api.SubmitRequest{PreviewID: p.ID, PlanHash: p.Plan.Hash})
	code, raw, _ = hostHTTP(t, h, "POST", "/agent/v1/tasks", string(body), "owned-restart-key", "")
	var original store.Task
	if code != 201 || json.Unmarshal(raw, &original) != nil {
		t.Fatal("create", code)
	}
	foreign, e := h.store.CreateSubmission("foreign-preview", "foreign-key", store.CreateRequest{ProjectID: "not-registered", Goal: "foreign private goal", Plan: p.Plan}, store.DefaultStamp{})
	if e != nil {
		t.Fatal(e)
	}
	if e = h.Close(); e != nil {
		t.Fatal(e)
	}
	h, e = OpenExecutionControl(context.Background(), path, root, "127.0.0.1:0", factory)
	if e != nil {
		t.Fatal("restart", e)
	}
	serveExecutionHost(t, h)
	b, e := newNativeTestBridge(t, h)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	for i := 0; i < 2; i++ {
		request := nativeRequest("POST", "/agent/v1/tasks", string(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "owned-restart-key")
		w := httptest.NewRecorder()
		b.ServeHTTP(w, request)
		var got store.Task
		if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got != original {
			t.Fatal("native recovery", w.Code)
		}
	}
	foreignBody, _ := json.Marshal(api.SubmitRequest{PreviewID: "foreign-preview", PlanHash: p.Plan.Hash})
	code, raw, _ = hostHTTP(t, h, "POST", "/agent/v1/tasks", string(foreignBody), "foreign-key", "")
	if code != 404 || strings.Contains(string(raw), foreign.ID) || strings.Contains(string(raw), "foreign private goal") {
		t.Fatal("foreign recovered through HTTP", code)
	}
	request := nativeRequest("POST", "/agent/v1/tasks", string(foreignBody))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "foreign-key")
	w := httptest.NewRecorder()
	b.ServeHTTP(w, request)
	if w.Code != 404 || strings.Contains(w.Body.String(), foreign.ID) {
		t.Fatal("foreign recovered through Native", w.Code)
	}
	events, e := h.store.Events(original.ID, 0)
	if e != nil || len(events) != 1 || events[0].Kind != "created" {
		t.Fatal("recovery wrote history", e)
	}
	budget, e := h.store.Budget(original.ID)
	if e != nil || budget.UsedCalls != 0 || budget.UsedReworks != 0 || calls.Load() != 0 {
		t.Fatal("recovery executed or changed budget", e)
	}
	source.Revision++
	writeSource(t, path, source)
	request = nativeRequest("POST", "/agent/v1/tasks", string(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "owned-restart-key")
	w = httptest.NewRecorder()
	b.ServeHTTP(w, request)
	if w.Code != 503 || strings.Contains(w.Body.String(), original.ID) {
		t.Fatal("revoked source recovered receipt", w.Code)
	}
}

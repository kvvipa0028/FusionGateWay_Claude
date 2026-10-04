//go:build darwin || linux

package bootstrap

import (
	"context"
	"encoding/json"
	"net/http"
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

type journalTrip struct {
	base  http.RoundTripper
	after func()
}

func (t journalTrip) RoundTrip(r *http.Request) (*http.Response, error) {
	response, e := t.base.RoundTrip(r)
	if e == nil {
		t.after()
	}
	return response, e
}

func TestSubmissionJournalNativeDoesNotDiscloseAfterSourceChangedDuringResponse(t *testing.T) {
	path, source := sourceFixture(t)
	h, e := OpenExecutionControl(context.Background(), path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0", func(_ context.Context, env RuntimeEnvironment) (RuntimeRegistration, error) {
		return hostRegistration(env), nil
	})
	if e != nil {
		t.Fatal(e)
	}
	serveExecutionHost(t, h)
	code, raw, _ := hostHTTP(t, h, "POST", "/control/v1/tasks/preview", `{"project_id":"fixture-project","goal":"private original goal","required_roles":["design"]}`, "", "")
	var p api.Preview
	if code != 200 || json.Unmarshal(raw, &p) != nil {
		t.Fatal("preview", code)
	}
	body, _ := json.Marshal(api.SubmitRequest{PreviewID: p.ID, PlanHash: p.Plan.Hash})
	const pathSubmission = "/control/v1/projects/fixture-project/submission"
	code, _, _ = hostHTTP(t, h, "POST", pathSubmission, string(body), "private-original-key", "")
	if code != 200 {
		t.Fatal("prepare", code)
	}
	b, e := newNativeTestBridge(t, h)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	b.client.Transport = journalTrip{base: b.transport, after: func() { source.Revision++; writeSource(t, path, source) }}
	w := httptest.NewRecorder()
	b.ServeHTTP(w, nativeRequest("GET", pathSubmission, ""))
	if w.Code != 503 || strings.Contains(w.Body.String(), "private-original-key") || strings.Contains(w.Body.String(), "private original goal") || strings.Contains(w.Body.String(), p.ID) {
		t.Fatal("late source revocation disclosed response", w.Code)
	}
}

func TestSubmissionJournalOwnedHostNativeRecoveryAndSealing(t *testing.T) {
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
	const submissionPath = "/control/v1/projects/fixture-project/submission"
	code, raw, _ := hostHTTP(t, h, "POST", "/control/v1/tasks/preview", `{"project_id":"fixture-project","goal":"owned original goal","required_roles":["design"]}`, "", "")
	var p api.Preview
	if code != 200 || json.Unmarshal(raw, &p) != nil {
		t.Fatal("preview", code)
	}
	body, _ := json.Marshal(api.SubmitRequest{PreviewID: p.ID, PlanHash: p.Plan.Hash})
	code, raw, _ = hostHTTP(t, h, "POST", submissionPath, string(body), "owned-original-key", "")
	var prepared api.SubmissionReply
	if code != 200 || json.Unmarshal(raw, &prepared) != nil || prepared.Submission == nil || prepared.Submission.State != "prepared" {
		t.Fatal("prepare", code)
	}
	if e = h.Close(); e != nil {
		t.Fatal(e)
	}
	h, e = OpenExecutionControl(context.Background(), path, root, "127.0.0.1:0", factory)
	if e != nil {
		t.Fatal(e)
	}
	serveExecutionHost(t, h)
	b, e := newNativeTestBridge(t, h)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { b.Close() }()
	call := func(method, path, body, key string) *httptest.ResponseRecorder {
		r := nativeRequest(method, path, body)
		r.Header.Set("Content-Type", "application/json")
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		b.ServeHTTP(w, r)
		return w
	}
	read := func(w *httptest.ResponseRecorder) *api.SubmissionView {
		t.Helper()
		var out api.SubmissionReply
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatal("Native journal", w.Code)
		}
		return out.Submission
	}
	for _, method := range []string{"GET", "POST"} {
		got := read(call(method, submissionPath, string(body), "owned-original-key"))
		if got == nil || got.ProjectID != "fixture-project" || got.Goal != "owned original goal" || got.Key != "owned-original-key" || got.Preview.ID != p.ID || got.Preview.Plan.Hash != p.Plan.Hash || got.State != "prepared" {
			t.Fatal("original not restored")
		}
	}
	if w := call("POST", "/agent/v1/tasks", string(body), "owned-original-key"); w.Code != 409 {
		t.Fatal("missing preview recreated", w.Code)
	}
	if got := read(call("POST", submissionPath+"/abandon", string(body), "owned-original-key")); got.State != "abandoned" {
		t.Fatal("not sealed")
	}
	if read(call("GET", submissionPath, "", "")) != nil {
		t.Fatal("abandoned still pending")
	}
	if w := call("POST", "/agent/v1/tasks", string(body), "owned-original-key"); w.Code != 409 {
		t.Fatal("sealed original committed", w.Code)
	}
	// Prepare another genuine preview, commit, then reopen the actual host/Store.
	w := call("POST", "/control/v1/tasks/preview", `{"project_id":"fixture-project","goal":"owned committed goal","required_roles":["design"]}`, "")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil {
		t.Fatal("new preview", w.Code)
	}
	body, _ = json.Marshal(api.SubmitRequest{PreviewID: p.ID, PlanHash: p.Plan.Hash})
	read(call("POST", submissionPath, string(body), "owned-committed-key"))
	w = call("POST", "/agent/v1/tasks", string(body), "owned-committed-key")
	var original store.Task
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &original) != nil {
		t.Fatal("commit", w.Code)
	}
	b.Close()
	if e = h.Close(); e != nil {
		t.Fatal(e)
	}
	h, e = OpenExecutionControl(context.Background(), path, root, "127.0.0.1:0", factory)
	if e != nil {
		t.Fatal(e)
	}
	serveExecutionHost(t, h)
	b, e = newNativeTestBridge(t, h)
	if e != nil {
		t.Fatal(e)
	}
	got := read(call("GET", submissionPath, "", ""))
	if got == nil || got.State != "committed" || got.TaskID != original.ID || got.Key != "owned-committed-key" {
		t.Fatal("committed original lost")
	}
	w = call("POST", "/agent/v1/tasks", string(body), "owned-committed-key")
	var recovered store.Task
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &recovered) != nil || recovered != original {
		t.Fatal("original receipt changed", w.Code)
	}
	if got = read(call("POST", submissionPath+"/acknowledge", string(body), "owned-committed-key")); got.State != "acknowledged" || got.TaskID != original.ID {
		t.Fatal("ack failed")
	}
	if read(call("GET", submissionPath, "", "")) != nil {
		t.Fatal("ack still pending")
	}
	for _, tc := range []struct{ method, path string }{
		{"GET", submissionPath + "/acknowledge"}, {"PUT", submissionPath}, {"POST", submissionPath + "/unknown"},
		{"POST", submissionPath + "/abandon/extra"}, {"GET", "/control/v1/projects/foreign/submission"},
	} {
		if w = call(tc.method, tc.path, string(body), "owned-committed-key"); w.Code != 404 {
			t.Fatal("Native allowlist broadened", w.Code)
		}
	}
	r := nativeRequest("POST", submissionPath+"/acknowledge", string(body))
	r.Header.Set("Idempotency-Key", "owned-committed-key")
	r.Header.Set("Authorization", "Bearer client-supplied")
	w = httptest.NewRecorder()
	b.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("Native accepted page credential", w.Code)
	}
	events, e := h.store.Events(original.ID, 0)
	if e != nil || len(events) != 1 || events[0].Kind != "created" {
		t.Fatal("recovery changed history", e)
	}
	budget, e := h.store.Budget(original.ID)
	if e != nil || budget.UsedCalls != 0 || budget.UsedReworks != 0 || calls.Load() != 0 {
		t.Fatal("recovery executed", e)
	}
	source.Revision++
	writeSource(t, path, source)
	w = call("GET", submissionPath, "", "")
	if w.Code != 503 || strings.Contains(w.Body.String(), original.ID) || strings.Contains(w.Body.String(), "owned-committed-key") {
		t.Fatal("Source revocation disclosed journal", w.Code)
	}
}

//go:build darwin || linux

package bootstrap

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/api"
	"github.com/yetone/magpie/internal/fusion/store"
)

func nativeStartJournalReply(t *testing.T, w *httptest.ResponseRecorder) *api.StartRequestView {
	t.Helper()
	var reply api.StartRequestReply
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &reply) != nil {
		t.Fatal("Native start record", w.Code)
	}
	return reply.Request
}
func nativeJournalHost(t *testing.T) (string, Document, *ControlHost, *NativeStageBridge) {
	t.Helper()
	path, doc := sourceFixture(t)
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
	t.Cleanup(b.Close)
	return path, doc, h, b
}

func TestNativeStartJournalOriginalPreparedAndUnknownSurviveHostRestart(t *testing.T) {
	path, _, h, b := nativeJournalHost(t)
	root := h.root
	task := hostCreateTask(t, h)
	base := "/control/v1/tasks/" + task.ID + "/start-request"
	pending := "/control/v1/projects/fixture-project/start-request"
	key := "synthetic-native-start-original"
	tag := `"p1-g0-ready"`
	original := nativeStartJournalReply(t, nativeControlRequest(b, "POST", base, `{"role":"design"}`, key, tag))
	if original == nil || original.State != "prepared" || original.Key != key || original.ETag != tag {
		t.Fatal("original preparation missing")
	}
	b.Close()
	if e := h.Close(); e != nil {
		t.Fatal(e)
	}
	h, e := OpenControl(path, root, "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	serveExecutionHost(t, h)
	b, e = newNativeTestBridge(t, h)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	got := nativeStartJournalReply(t, nativeControlRequest(b, "GET", pending, "", "", ""))
	if got == nil || got.State != "prepared" || got.Key != key || got.Task.ID != task.ID || got.Task.Generation != 0 || got.Plan.Hash != original.Plan.Hash {
		t.Fatal("prepared restart lost original")
	}
	// Simulate a crash after the durable intent, before any Runtime launch.
	// This trusted Store fixture is not execution admission or a Native start.
	gen := int64(0)
	receipt, e := h.store.StartReservedOnce(store.StartRequest{TaskID: task.ID, Role: "design", PlanRevision: 1, ExpectedGeneration: &gen, IdempotencyKey: key, Owner: "fixture-crashed-owner", TTL: time.Minute, Target: *got.Plan.Bindings["design"].Target}, store.ReservationRequest{PoolKey: "fixture-start-pool", GlobalLimit: 2, AdmissionHash: strings.Repeat("a", 64)})
	if e != nil || !receipt.Created {
		t.Fatal("crash intent fixture", e)
	}
	b.Close()
	if e = h.Close(); e != nil {
		t.Fatal(e)
	}
	h, e = OpenControl(path, root, "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	serveExecutionHost(t, h)
	b, e = newNativeTestBridge(t, h)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	got = nativeStartJournalReply(t, nativeControlRequest(b, "GET", base, "", key, tag))
	run, e := h.store.Run(receipt.Run.ID)
	if e != nil || run.State != "unknown" || got.State != "committed" || got.RunID != run.ID || got.Task.State != "ready" || got.Task.Generation != 0 {
		t.Fatal("unknown restart reinterpreted original", e, run.State)
	}
	if w := nativeControlRequest(b, "POST", base+"/abandon", `{"role":"design"}`, key, tag); w.Code != 409 {
		t.Fatal("unknown run sealed", w.Code)
	}
	body := `{"role":"design","run_id":"` + run.ID + `"}`
	ack := nativeStartJournalReply(t, nativeControlRequest(b, "POST", base+"/acknowledge", body, key, tag))
	after, e := h.store.Run(run.ID)
	if e != nil || after.State != "unknown" || ack.State != "acknowledged" {
		t.Fatal("ack fabricated reconciliation", e)
	}
	if nativeStartJournalReply(t, nativeControlRequest(b, "GET", pending, "", "", "")) != nil {
		t.Fatal("ack pending")
	}
	if exact := nativeStartJournalReply(t, nativeControlRequest(b, "GET", base, "", key, tag)); exact.State != "acknowledged" || exact.RunID != run.ID {
		t.Fatal("lost ack unreadable")
	}
}

func TestNativeStartJournalOnlyExactWindowProjectMethodsAndHeaders(t *testing.T) {
	_, _, h, b := nativeJournalHost(t)
	task := hostCreateTask(t, h)
	base := "/control/v1/tasks/" + task.ID + "/start-request"
	key := "fixture-native-start"
	tag := `"p1-g0-ready"`
	original := nativeStartJournalReply(t, nativeControlRequest(b, "POST", base, `{"role":"design"}`, key, tag))
	got := nativeStartJournalReply(t, nativeControlRequest(b, "GET", base, "", key, tag))
	if got.Key != original.Key {
		t.Fatal("GET key not forwarded")
	}
	for _, tc := range []struct{ method, path string }{{"PUT", base}, {"HEAD", base}, {"GET", base + "/acknowledge"}, {"POST", base + "/unknown"}, {"GET", base + "/extra"}, {"POST", "/control/v1/projects/fixture-project/start-request"}, {"GET", "/control/v1/projects/other/start-request"}, {"GET", "/control/v1/tasks/missing/start-request"}, {"POST", base + "/abandon/extra"}} {
		if w := nativeControlRequest(b, tc.method, tc.path, `{}`, key, tag); w.Code != 404 {
			t.Fatal("route scope widened", tc.path, w.Code)
		}
	}
	for _, tc := range []struct {
		key, tag string
		status   int
	}{{"", tag, 400}, {key, "", 428}, {key, `"p1-g1-ready"`, 412}} {
		if w := nativeControlRequest(b, "GET", base, "", tc.key, tc.tag); w.Code != tc.status {
			t.Fatal("original headers lost", w.Code)
		}
	}
	for _, header := range []string{"Idempotency-Key", "If-Match"} {
		r := nativeRequest("GET", base, "")
		r.Header.Set("Idempotency-Key", key)
		r.Header.Set("If-Match", tag)
		r.Header.Add(header, r.Header.Get(header))
		w := httptest.NewRecorder()
		b.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal("duplicate header normalized", header, w.Code)
		}
	}
	r := nativeRequest("GET", base, "")
	r.Header.Set("X-Wails-Window-Id", "2")
	r.Header.Set("Idempotency-Key", key)
	r.Header.Set("If-Match", tag)
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("foreign window", w.Code)
	}
	foreign, e := h.store.Create("fixture-foreign-start", store.CreateRequest{ProjectID: "foreign-project", Goal: "private-foreign-goal", Plan: original.Plan})
	if e != nil {
		t.Fatal(e)
	}
	w = nativeControlRequest(b, "GET", "/control/v1/tasks/"+foreign.ID+"/start-request", "", key, tag)
	if w.Code != 404 || strings.Contains(w.Body.String(), "private-foreign-goal") {
		t.Fatal("foreign Task disclosed", w.Code)
	}
	sealed := nativeStartJournalReply(t, nativeControlRequest(b, "POST", base+"/abandon", `{"role":"design"}`, key, tag))
	if sealed.State != "abandoned" {
		t.Fatal("seal path unavailable")
	}
	b.Close()
	if w := nativeControlRequest(b, "GET", base, "", key, tag); w.Code != 503 {
		t.Fatal("closed window", w.Code)
	}
}

func TestNativeStartJournalLateSourceLossSuppressesPreparedReceipt(t *testing.T) {
	path, source, h, b := nativeJournalHost(t)
	task := hostCreateTask(t, h)
	key := "fixture-native-late-start"
	b.client.Transport = journalTrip{base: b.transport, after: func() { source.Revision++; writeSource(t, path, source) }}
	w := nativeControlRequest(b, "POST", "/control/v1/tasks/"+task.ID+"/start-request", `{"role":"design"}`, key, `"p1-g0-ready"`)
	if w.Code != 503 || strings.Contains(w.Body.String(), task.Goal) || strings.Contains(w.Body.String(), key) {
		t.Fatal("late source disclosed original", w.Code)
	}
	j, e := h.store.PendingStart("fixture-project")
	if e != nil || j.State != "prepared" || j.Draft.Key != key {
		t.Fatal("committed metadata lost after reply suppression", e)
	}
	current, e := h.store.Task(task.ID)
	if e != nil || current.State != "ready" || current.Generation != 0 {
		t.Fatal("metadata route launched")
	}
}

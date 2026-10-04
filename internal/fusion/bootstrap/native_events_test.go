//go:build darwin || linux

package bootstrap

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/fusion/api"
)

func TestNativeEventPageUsesOwnedTaskScopeWithoutExecution(t *testing.T) {
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
	base := "/agent/v1/tasks/" + task.ID + "/events/page/"
	w := nativeControlRequest(b, "GET", base+"0", "", "", "")
	var reply api.EventPageReply
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.TaskID != task.ID || len(reply.Events) != 1 || reply.Events[0].Kind != "created" {
		t.Fatal("Native event page unavailable", w.Code)
	}
	for _, tc := range []struct{ method, path string }{{"POST", base + "0"}, {"HEAD", base + "0"}, {"GET", base + "00"}, {"GET", base + "-1"}, {"GET", base + "0/extra"}, {"GET", "/agent/v1/tasks/missing/events/page/0"}, {"GET", "/agent/v1/tasks/" + task.ID + "/events"}} {
		if w := nativeControlRequest(b, tc.method, tc.path, "", "", ""); w.Code != 404 {
			t.Fatal("Native event scope widened", tc.path, w.Code)
		}
	}
	r := nativeRequest("GET", base+"0", "")
	r.Header.Set("X-Wails-Window-Id", "2")
	w = httptest.NewRecorder()
	b.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("foreign window read events", w.Code)
	}
	b.client.Transport = journalTrip{base: b.transport, after: func() { source.Revision++; writeSource(t, path, source) }}
	w = nativeControlRequest(b, "GET", base+"0", "", "", "")
	if w.Code != 503 || strings.Contains(w.Body.String(), task.ID) {
		t.Fatal("late source disclosed events", w.Code)
	}
	before, e := h.store.Task(task.ID)
	if e != nil || before.Generation != 0 || before.State != "ready" {
		t.Fatal("history reading executed Task")
	}
}

func TestNativeEventPageSurvivesHostRestartAndRejectsUnregisteredHistory(t *testing.T) {
	path, source := sourceFixture(t)
	root := filepath.Join(filepath.Dir(path), "control")
	h, e := OpenExecutionControl(context.Background(), path, root, "127.0.0.1:0", func(_ context.Context, env RuntimeEnvironment) (RuntimeRegistration, error) {
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
	task := hostCreateTask(t, h)
	w := nativeControlRequest(b, "POST", "/control/v1/tasks/"+task.ID+"/pause", "{}", "", nativeControlTag(task))
	if w.Code != 200 {
		t.Fatal("pause fixture", w.Code)
	}
	pathEvents := "/agent/v1/tasks/" + task.ID + "/events/page/1"
	w = nativeControlRequest(b, "GET", pathEvents, "", "", "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	original := w.Body.String()
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
	w = nativeControlRequest(b, "GET", pathEvents, "", "", "")
	if w.Code != 200 || w.Body.String() != original {
		t.Fatal("reopened history drift", w.Code)
	}
	b.Close()
	if e = h.Close(); e != nil {
		t.Fatal(e)
	}
	source.Projects[0].ID = "other-project"
	source.Revision++
	writeSource(t, path, source)
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
	w = nativeControlRequest(b, "GET", pathEvents, "", "", "")
	if w.Code != 404 || strings.Contains(w.Body.String(), task.ID) {
		t.Fatal("unregistered history disclosed", w.Code)
	}
}

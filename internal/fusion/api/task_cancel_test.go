package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTaskCancelAPIIdleAndPausedOverHTTP(t *testing.T) {
	for _, paused := range []bool{false, true} {
		t.Run(map[bool]string{false: "ready", true: "paused"}[paused], func(t *testing.T) {
			f := setupExecution(t, "")
			path := "/control/v1/tasks/" + f.task.ID
			if paused {
				w := executionRequest(f.h, "POST", path+"/pause", `{}`, "", f.tag(t), "fixture-management")
				if w.Code != 200 {
					t.Fatal(w.Code)
				}
			}
			before, e := f.st.Task(f.task.ID)
			if e != nil {
				t.Fatal(e)
			}
			server := httptest.NewServer(f.h)
			defer server.Close()
			call := func(tag string) TaskControlReply {
				req, _ := http.NewRequest("POST", server.URL+path+"/cancel", strings.NewReader(`{}`))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer fixture-management")
				req.Header.Set("If-Match", tag)
				resp, e := server.Client().Do(req)
				if e != nil {
					t.Fatal(e)
				}
				defer resp.Body.Close()
				var out TaskControlReply
				if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&out) != nil || resp.Header.Get("X-Fusion-Task-ETag") != taskETag(out.Task) || resp.Header.Get("Location") != "/agent/v1/tasks/"+f.task.ID {
					t.Fatal("actual HTTP cancellation", resp.StatusCode)
				}
				return out
			}
			x := call(taskETag(before))
			if !x.Changed || x.Task.State != "cancelled" || x.Task.Generation != before.Generation+1 || x.Run != nil {
				t.Fatal("idle cancelled")
			}
			again := call(taskETag(x.Task))
			if again.Changed || again.Task != x.Task {
				t.Fatal("repeat cancelled")
			}
			w := executionRequest(f.h, "POST", f.startPath(), `{"role":"design"}`, "fixture-task-cancel-stale", taskETag(before), "fixture-management")
			if w.Code != 412 || f.started.Load() != 0 {
				t.Fatal("old start after cancel", w.Code)
			}
		})
	}
}

func TestTaskCancelAPIRunningStopsWithoutReplay(t *testing.T) {
	f := setupExecution(t, "")
	started := executionRequest(f.h, "POST", f.startPath(), `{"role":"design"}`, "fixture-pause-http-start", f.tag(t), "fixture-management")
	run := executionReply(t, started)
	path := "/control/v1/tasks/" + f.task.ID
	w := executionRequest(f.h, "POST", path+"/cancel", `{}`, "", f.tag(t), "fixture-management")
	out := taskControlReply(t, w)
	if w.Code != 202 || !out.Changed || out.Run == nil || out.Run.ID != run.Run.ID {
		t.Fatal("pause did not retain accepted intent", w.Code)
	}
	waitAPIExecution(t, f, run.Run.ID)
	task, e := f.st.Task(f.task.ID)
	if e != nil || task.State != "cancelled" {
		t.Fatal("cancelled task replayable", e)
	}
	w = executionRequest(f.h, "POST", path+"/continue", `{}`, "", taskETag(task), "fixture-management")
	if w.Code != 412 || f.started.Load() != 1 {
		t.Fatal("blind continue replayed", w.Code)
	}
	retry := executionRequest(f.h, "POST", f.startPath(), `{"role":"design"}`, "fixture-pause-http-start", taskETag(f.task), "fixture-management")
	if retry.Code != 200 || executionReply(t, retry).Created || f.started.Load() != 1 {
		t.Fatal("old mapping replayed", retry.Code)
	}
}
func TestTaskCancelAPIRejectsUnsafeBodyHeadersScopeAndMethod(t *testing.T) {
	f := setupExecution(t, "")
	path := "/control/v1/tasks/" + f.task.ID + "/cancel"
	tag := f.tag(t)
	for _, body := range []string{`null`, `[]`, `{"role":"design"}`, `{"model":"fixture"}`, `{"account":"fixture"}`, `{"owner":"fixture"}`, `{"workspace":"/fixture"}`, `{"token":"fixture-sensitive"}`, `{"stopped_verified":true}`, `{"generation":0}`, `{} {}`} {
		w := executionRequest(f.h, "POST", path, body, "", tag, "fixture-management")
		if w.Code != 400 {
			t.Fatal("unsafe control body", w.Code, body)
		}
	}
	for _, v := range []struct {
		tag  string
		code int
	}{{"", 428}, {"*", 400}, {"W/" + tag, 400}, {tag + "," + tag, 400}, {`"p01-g0-ready"`, 400}, {`"p1-g9-ready"`, 412}, {`"p1-g0-paused"`, 412}} {
		w := executionRequest(f.h, "POST", path, `{}`, "", v.tag, "fixture-management")
		if w.Code != v.code {
			t.Fatal("unsafe control condition", v.tag, w.Code)
		}
	}
	for _, v := range []struct {
		credential string
		code       int
	}{{"", 401}, {"fixture-wrong", 401}, {"fgs_fixture", 403}} {
		w := executionRequest(f.h, "POST", path, `{}`, "", tag, v.credential)
		if w.Code != v.code {
			t.Fatal("control authorization", w.Code)
		}
	}
	if w := executionRequest(f.h, "GET", path, `{}`, "", tag, "fixture-management"); w.Code != 405 {
		t.Fatal("method", w.Code)
	}
	for _, special := range []string{"duplicate-tag", "origin", "query", "encoding"} {
		req := httptest.NewRequest("POST", path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer fixture-management")
		req.Header.Set("If-Match", tag)
		switch special {
		case "duplicate-tag":
			req.Header.Add("If-Match", tag)
		case "origin":
			req.Header.Set("Origin", "https://fixture-untrusted.invalid")
		case "query":
			req.URL.RawQuery = "token=fixture-sensitive"
		case "encoding":
			req.Header.Set("Content-Encoding", "gzip")
		}
		w := httptest.NewRecorder()
		f.h.ServeHTTP(w, req)
		if w.Code != 400 && w.Code != 403 {
			t.Fatal("unsafe request admitted", special, w.Code)
		}
	}
	if w := executionRequest(f.h, "POST", "/control/v1/tasks/fixture-missing/cancel", `{}`, "", tag, "fixture-management"); w.Code != 404 {
		t.Fatal("unknown task", w.Code)
	}
	task, e := f.st.Task(f.task.ID)
	if e != nil || task != f.task || f.started.Load() != 0 {
		t.Fatal("rejected control mutated", e)
	}
}

func TestTaskCancelAPIUnavailableRevokedAndUnknownStayClosed(t *testing.T) {
	_, _, h := setup(t)
	w := executionRequest(h, "POST", "/control/v1/tasks/fixture/cancel", `{}`, "", `"p1-g0-ready"`, "fixture-management")
	if w.Code != 503 {
		t.Fatal("missing controller", w.Code)
	}
	f := setupExecution(t, "")
	task, e := f.st.Task(f.task.ID)
	if e != nil {
		t.Fatal(e)
	}
	req := httptest.NewRequest("POST", "/control/v1/tasks/"+task.ID+"/cancel", nil)
	req.Body = io.NopCloser(revokingControlBody{Reader: strings.NewReader(`{}`), revoke: f.server.auth.RevokeManagement})
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer fixture-management")
	req.Header.Set("If-Match", taskETag(task))
	w = httptest.NewRecorder()
	f.h.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatal("revoked cancel", w.Code)
	}
	got, e := f.st.Task(task.ID)
	if e != nil || got != task {
		t.Fatal("revoked mutation", e)
	}
	unknown := setupExecution(t, "no_handle")
	started := executionRequest(unknown.h, "POST", unknown.startPath(), `{"role":"design"}`, "fixture-task-cancel-unknown", unknown.tag(t), "fixture-management")
	run := executionReply(t, started)
	waitAPIExecution(t, unknown, run.Run.ID)
	w = executionRequest(unknown.h, "POST", "/control/v1/tasks/"+unknown.task.ID+"/cancel", `{}`, "", unknown.tag(t), "fixture-management")
	if w.Code != 409 || !strings.Contains(w.Body.String(), "execution_requires_reconciliation") {
		t.Fatal("unknown adopted or error leaked", w.Code)
	}
}

func TestTaskCancelAPIUncertainStopKeepsSafeReceiptAndReservation(t *testing.T) {
	f := setupExecution(t, "no_stop")
	started := executionRequest(f.h, "POST", f.startPath(), `{"role":"design"}`, "fixture-http-held-start", f.tag(t), "fixture-management")
	run := executionReply(t, started)
	path := "/control/v1/tasks/" + f.task.ID + "/cancel"
	w := executionRequest(f.h, "POST", path, `{}`, "", f.tag(t), "fixture-management")
	if w.Code != 202 {
		t.Fatal(w.Code)
	}
	waitAPIExecution(t, f, run.Run.ID)
	w = executionRequest(f.h, "POST", path, `{}`, "", f.tag(t), "fixture-management")
	out := taskControlReply(t, w)
	if w.Code != 409 || out.Changed || out.Task.State != "cancelling" || out.Run == nil || out.Run.ID != run.Run.ID || out.Error == nil || out.Error.Code != "execution_requires_reconciliation" {
		t.Fatal("uncertain accepted intent lost", w.Code)
	}
	if _, e := f.st.Reservation(run.Run.ID); e != nil {
		t.Fatal("uncertain stop freed reservation", e)
	}
}

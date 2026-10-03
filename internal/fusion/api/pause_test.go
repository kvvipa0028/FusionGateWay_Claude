package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func taskControlReply(t *testing.T, w *httptest.ResponseRecorder) TaskControlReply {
	t.Helper()
	var out TaskControlReply
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Task.ID == "" {
		t.Fatal("task control receipt missing", w.Code)
	}
	if w.Header().Get("X-Fusion-Task-ETag") != taskETag(out.Task) {
		t.Fatal("Task DTO/header mismatched")
	}
	for _, s := range []string{"lease_owner", "native_session_id", "owner", "fixture-native-session", "fixture-sensitive"} {
		if strings.Contains(w.Body.String(), s) {
			t.Fatal("private runtime data leaked", s)
		}
	}
	return out
}
func TestTaskControlAPIIdlePauseContinueAndNoReplayOverHTTP(t *testing.T) {
	f := setupExecution(t, "")
	server := httptest.NewServer(f.h)
	defer server.Close()
	call := func(action, tag string) TaskControlReply {
		req, _ := http.NewRequest("POST", server.URL+"/control/v1/tasks/"+f.task.ID+"/"+action, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer fixture-management")
		req.Header.Set("If-Match", tag)
		resp, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		var out TaskControlReply
		if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&out) != nil || resp.Header.Get("X-Fusion-Task-ETag") != taskETag(out.Task) {
			t.Fatal("actual HTTP control failed", resp.StatusCode)
		}
		return out
	}
	before := f.tag(t)
	paused := call("pause", before)
	if !paused.Changed || paused.Task.State != "paused" || paused.Task.Generation != 1 || paused.Run != nil {
		t.Fatal("idle paused")
	}
	repeat := call("pause", taskETag(paused.Task))
	if repeat.Changed {
		t.Fatal("duplicate pause")
	}
	continued := call("continue", taskETag(paused.Task))
	if !continued.Changed || continued.Task.State != "ready" || continued.Task.Generation != 2 {
		t.Fatal("continue")
	}
	repeat = call("continue", taskETag(continued.Task))
	if repeat.Changed {
		t.Fatal("duplicate continue")
	}
	w := executionRequest(f.h, "POST", f.startPath(), `{"role":"design"}`, "fixture-stale-start", before, "fixture-management")
	if w.Code != 412 || f.started.Load() != 0 {
		t.Fatal("old request launched after ABA", w.Code)
	}
}
func TestTaskControlAPIPauseRunningStopsWithoutBlindContinue(t *testing.T) {
	f := setupExecution(t, "")
	started := executionRequest(f.h, "POST", f.startPath(), `{"role":"design"}`, "fixture-pause-http-start", f.tag(t), "fixture-management")
	run := executionReply(t, started)
	path := "/control/v1/tasks/" + f.task.ID
	w := executionRequest(f.h, "POST", path+"/pause", `{}`, "", f.tag(t), "fixture-management")
	out := taskControlReply(t, w)
	if w.Code != 202 || !out.Changed || out.Run == nil || out.Run.ID != run.Run.ID {
		t.Fatal("pause did not retain accepted intent", w.Code)
	}
	waitAPIExecution(t, f, run.Run.ID)
	task, e := f.st.Task(f.task.ID)
	if e != nil || task.State != "needs_review" {
		t.Fatal("cancelled task replayable", e)
	}
	w = executionRequest(f.h, "POST", path+"/continue", `{}`, "", taskETag(task), "fixture-management")
	if w.Code != 409 || f.started.Load() != 1 {
		t.Fatal("blind continue replayed", w.Code)
	}
	retry := executionRequest(f.h, "POST", f.startPath(), `{"role":"design"}`, "fixture-pause-http-start", taskETag(f.task), "fixture-management")
	if retry.Code != 200 || executionReply(t, retry).Created || f.started.Load() != 1 {
		t.Fatal("old mapping replayed", retry.Code)
	}
}
func TestTaskControlAPIRejectsUnsafeBodyHeadersScopeAndMethod(t *testing.T) {
	f := setupExecution(t, "")
	path := "/control/v1/tasks/" + f.task.ID + "/pause"
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
	if w := executionRequest(f.h, "POST", "/control/v1/tasks/fixture-missing/pause", `{}`, "", tag, "fixture-management"); w.Code != 404 {
		t.Fatal("unknown task", w.Code)
	}
	task, e := f.st.Task(f.task.ID)
	if e != nil || task != f.task || f.started.Load() != 0 {
		t.Fatal("rejected control mutated", e)
	}
}
func TestTaskControlAPIUnavailableControllerAndStalePostPauseStayClosed(t *testing.T) {
	_, _, h := setup(t)
	if w := executionRequest(h, "POST", "/control/v1/tasks/fixture/pause", `{}`, "", `"p1-g0-ready"`, "fixture-management"); w.Code != 503 {
		t.Fatal("missing controller", w.Code)
	}
	f := setupExecution(t, "")
	path := "/control/v1/tasks/" + f.task.ID
	tag := f.tag(t)
	if w := executionRequest(f.h, "POST", path+"/pause", `{}`, "", tag, "fixture-management"); w.Code != 200 {
		t.Fatal(w.Code)
	}
	for _, action := range []string{"pause", "continue"} {
		if w := executionRequest(f.h, "POST", path+"/"+action, `{}`, "", tag, "fixture-management"); w.Code != 412 {
			t.Fatal("stale control condition", w.Code)
		}
	}
}

type revokingControlBody struct {
	io.Reader
	revoke func()
}

func (b revokingControlBody) Read(p []byte) (int, error) { b.revoke(); return b.Reader.Read(p) }
func TestTaskControlAPIRevocationDuringBodyReadStopsMutation(t *testing.T) {
	for _, action := range []string{"pause", "continue"} {
		t.Run(action, func(t *testing.T) {
			f := setupExecution(t, "")
			path := "/control/v1/tasks/" + f.task.ID
			if action == "continue" {
				if w := executionRequest(f.h, "POST", path+"/pause", `{}`, "", f.tag(t), "fixture-management"); w.Code != 200 {
					t.Fatal(w.Code)
				}
			}
			task, e := f.st.Task(f.task.ID)
			if e != nil {
				t.Fatal(e)
			}
			req := httptest.NewRequest("POST", path+"/"+action, nil)
			req.Body = io.NopCloser(revokingControlBody{Reader: strings.NewReader(`{}`), revoke: f.server.auth.RevokeManagement})
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer fixture-management")
			req.Header.Set("If-Match", taskETag(task))
			w := httptest.NewRecorder()
			f.h.ServeHTTP(w, req)
			if w.Code != 401 {
				t.Fatal("revocation accepted", w.Code)
			}
			after, e := f.st.Task(f.task.ID)
			if e != nil || after != task {
				t.Fatal("revoked control mutated", e)
			}
		})
	}
}

func TestTaskControlAPIPauseUncertainStopKeepsSafeReceiptAndReservation(t *testing.T) {
	f := setupExecution(t, "no_stop")
	started := executionRequest(f.h, "POST", f.startPath(), `{"role":"design"}`, "fixture-http-held-start", f.tag(t), "fixture-management")
	run := executionReply(t, started)
	path := "/control/v1/tasks/" + f.task.ID + "/pause"
	w := executionRequest(f.h, "POST", path, `{}`, "", f.tag(t), "fixture-management")
	if w.Code != 202 {
		t.Fatal(w.Code)
	}
	waitAPIExecution(t, f, run.Run.ID)
	w = executionRequest(f.h, "POST", path, `{}`, "", f.tag(t), "fixture-management")
	out := taskControlReply(t, w)
	if w.Code != 409 || out.Changed || out.Task.State != "pausing" || out.Run == nil || out.Run.ID != run.Run.ID || out.Error == nil || out.Error.Code != "execution_requires_reconciliation" {
		t.Fatal("uncertain accepted intent lost", w.Code)
	}
	if _, e := f.st.Reservation(run.Run.ID); e != nil {
		t.Fatal("uncertain stop freed reservation", e)
	}
}

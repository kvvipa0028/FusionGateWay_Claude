package api

import (
	"encoding/json"
	"github.com/yetone/magpie/internal/fusion/control"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func apiCheckpointFixture(t *testing.T, mode string) (*executionFixture, string, string) {
	t.Helper()
	f := setupExecution(t, mode)
	w := executionRequest(f.h, "POST", f.startPath(), `{"role":"design"}`, "fixture-original", f.tag(t), "fixture-management")
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	run := executionReply(t, w)
	close(f.finish)
	waitAPIExecution(t, f, run.Run.ID)
	return f, "/control/v1/tasks/" + f.task.ID + "/runs/" + run.Run.ID + "/checkpoint", f.tag(t)
}
func TestCheckpointAPIRealHTTPPublishesSafeOwnedReference(t *testing.T) {
	f, path, tag := apiCheckpointFixture(t, "checkpoint")
	server := httptest.NewServer(f.h)
	defer server.Close()
	for n := 0; n < 2; n++ {
		req, _ := http.NewRequest("POST", server.URL+path, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer fixture-management")
		req.Header.Set("If-Match", tag)
		resp, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var reply struct {
			Checkpoint control.CheckpointRef `json:"checkpoint"`
			Run        RunView               `json:"run"`
		}
		if resp.StatusCode != 200 || json.Unmarshal(raw, &reply) != nil || reply.Run.State != "succeeded" || reply.Checkpoint.ID != strings.Repeat("b", 64) || reply.Checkpoint.Digest != strings.Repeat("c", 64) {
			t.Fatal("checkpoint reply absent", resp.StatusCode, string(raw))
		}
		if resp.Header.Get("X-Fusion-Task-ETag") != tag || resp.Header.Get("ETag") != "" {
			t.Fatal("wrong checkpoint resource tag")
		}
		for _, secret := range []string{"native_session_id", "fixture-native-session", "owner", "lease", "fixture prompt", "fixture-management"} {
			if strings.Contains(string(raw), secret) {
				t.Fatal("private checkpoint data disclosed")
			}
		}
	}
	if f.started.Load() != 1 {
		t.Fatal("checkpoint started another execution")
	}
}
func TestCheckpointAPIRefusesBodyConditionAuthorityAndUnboundProducer(t *testing.T) {
	f, path, tag := apiCheckpointFixture(t, "checkpoint")
	for _, v := range []struct {
		name, body, tag, auth string
		status                int
	}{
		{"body", `{"session_id":"foreign"}`, tag, "fixture-management", 400},
		{"ref", `{"id":"caller"}`, tag, "fixture-management", 400},
		{"null", "null", tag, "fixture-management", 400},
		{"missing_tag", "{}", "", "fixture-management", 428},
		{"weak", "{}", "W/" + tag, "fixture-management", 400},
		{"stale", "{}", `"p1-g0-ready"`, "fixture-management", 412},
		{"no_auth", "{}", tag, "", 401},
		{"stage", "{}", tag, "fgs_fixture", 403},
	} {
		t.Run(v.name, func(t *testing.T) {
			w := executionRequest(f.h, "POST", path, v.body, "", v.tag, v.auth)
			if w.Code != v.status {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	unbound, path, tag := apiCheckpointFixture(t, "")
	w := executionRequest(unbound.h, "POST", path, "{}", "", tag, "fixture-management")
	if w.Code != 503 {
		t.Fatal("unbound producer accepted", w.Code)
	}
}
func TestCheckpointAPIFailureAndRevocationExposeNoReference(t *testing.T) {
	for _, mode := range []string{"checkpoint_error", "checkpoint_revoke", "no_stop"} {
		t.Run(mode, func(t *testing.T) {
			f, path, tag := apiCheckpointFixture(t, mode)
			before, _ := f.st.Budget(f.task.ID)
			events, _ := f.st.Events(f.task.ID, 0)
			w := executionRequest(f.h, "POST", path, "{}", "", tag, "fixture-management")
			want := 409
			if mode == "checkpoint_revoke" {
				want = 401
			}
			if w.Code != want || strings.Contains(w.Body.String(), "checkpoint_digest") || strings.Contains(w.Body.String(), "fixture-sensitive") {
				t.Fatal("unsafe failure", w.Code, w.Body.String())
			}
			after, _ := f.st.Budget(f.task.ID)
			afterEvents, _ := f.st.Events(f.task.ID, 0)
			if before != after || len(events) != len(afterEvents) || f.started.Load() != 1 {
				t.Fatal("checkpoint failure changed execution")
			}
			if mode == "no_stop" {
				parts := strings.Split(path, "/")
				if _, e := f.st.Reservation(parts[len(parts)-2]); e != nil {
					t.Fatal("uncertain proof freed capacity")
				}
			}
		})
	}
}

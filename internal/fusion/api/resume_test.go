package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/yetone/magpie/internal/fusion/store"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func apiRestoreFixture(t *testing.T, mode string) (*executionFixture, string, string) {
	t.Helper()
	f := setupExecution(t, mode)
	first := executionRequest(f.h, "POST", f.startPath(), `{"role":"design"}`, "fixture-origin-start", f.tag(t), "fixture-management")
	if first.Code != 202 {
		t.Fatal("origin start failed", first.Code)
	}
	original := executionReply(t, first)
	close(f.finish)
	waitAPIExecution(t, f, original.Run.ID)
	ref := store.RestoreIdentity{OriginRunID: original.Run.ID, CheckpointID: strings.Repeat("b", 64), CheckpointDigest: strings.Repeat("c", 64)}
	body, _ := json.Marshal(map[string]any{"role": "design", "restore": ref})
	return f, string(body), f.tag(t)
}
func restorePath(f *executionFixture) string { return "/control/v1/tasks/" + f.task.ID + "/resume" }
func TestResumeAPIRealHTTPOnceAndIdentityConflict(t *testing.T) {
	f, body, tag := apiRestoreFixture(t, "restore")
	server := httptest.NewServer(f.h)
	defer server.Close()
	post := func(payload, key string) (int, ExecutionReply, string) {
		req, e := http.NewRequest("POST", server.URL+restorePath(f), strings.NewReader(payload))
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer fixture-management")
		req.Header.Set("If-Match", tag)
		req.Header.Set("Idempotency-Key", key)
		resp, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		raw, e := io.ReadAll(resp.Body)
		if e != nil {
			t.Fatal(e)
		}
		var out ExecutionReply
		json.Unmarshal(raw, &out)
		return resp.StatusCode, out, string(raw)
	}
	code, first, raw := post(body, "fixture-resume")
	if code != 202 || !first.Created || first.Run.Generation != 2 || first.Run.Attempt != 2 {
		t.Fatal("restore API did not create distinct run", code)
	}
	waitAPIExecution(t, f, first.Run.ID)
	code, again, _ := post(body, "fixture-resume")
	if code != 200 || again.Created || again.Run.ID != first.Run.ID || f.started.Load() != 2 {
		t.Fatal("restore HTTP replay", code)
	}
	for _, secret := range []string{"fixture-native-session", "owner", "lease_until", "fixture prompt", "fixture-management"} {
		if strings.Contains(raw, secret) {
			t.Fatal("private restore metadata disclosed")
		}
	}
	conflict := strings.Replace(body, strings.Repeat("c", 64), strings.Repeat("e", 64), 1)
	if code, _, _ = post(conflict, "fixture-resume"); code != 409 {
		t.Fatal("changed restore digest reused key", code)
	}
	plain := executionRequest(f.h, "POST", f.startPath(), `{"role":"design"}`, "fixture-resume", tag, "fixture-management")
	if plain.Code != 409 {
		t.Fatal("restore key became ordinary Start", plain.Code)
	}
	stale := executionRequest(f.h, "POST", restorePath(f), body, "fixture-new-key", tag, "fixture-management")
	if stale.Code != 412 || f.started.Load() != 2 {
		t.Fatal("old condition started new restore", stale.Code)
	}
}
func TestResumeAPIRefusesAuthorityBodyAndHeadersBeforeIntent(t *testing.T) {
	f, body, tag := apiRestoreFixture(t, "restore")
	task, _ := f.st.Task(f.task.ID)
	cases := []struct {
		name, body, key, tag, auth string
		status                     int
	}{
		{"missing_ref", `{"role":"design"}`, "fixture-bad", tag, "fixture-management", 400},
		{"null_ref", `{"role":"design","restore":null}`, "fixture-bad", tag, "fixture-management", 400},
		{"unknown", strings.TrimSuffix(body, "}") + `,"native_session_id":"evil"}`, "fixture-bad", tag, "fixture-management", 400},
		{"extra_restore", strings.Replace(body, `"restore":{`, `"restore":{"argv":[] ,`, 1), "fixture-bad", tag, "fixture-management", 400},
		{"bad_digest", strings.Replace(body, strings.Repeat("c", 64), "bad", 1), "fixture-bad", tag, "fixture-management", 400},
		{"no_key", body, "", tag, "fixture-management", 400},
		{"no_tag", body, "fixture-bad", "", "fixture-management", 428},
		{"weak_tag", body, "fixture-bad", "W/" + tag, "fixture-management", 400},
		{"no_auth", body, "fixture-bad", tag, "", 401},
		{"wrong_auth", body, "fixture-bad", tag, "fixture-wrong", 401},
		{"foreign_origin", strings.Replace(body, `"origin_run_id":"run-`, `"origin_run_id":"other-run-`, 1), "fixture-bad", tag, "fixture-management", 409},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := executionRequest(f.h, "POST", restorePath(f), c.body, c.key, c.tag, c.auth)
			if w.Code != c.status {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	after, _ := f.st.Task(f.task.ID)
	if after.Generation != task.Generation || after.State != task.State || f.started.Load() != 1 {
		t.Fatal("invalid resume wrote intent")
	}
	// The original Start DTO does not acquire restore fields.
	w := executionRequest(f.h, "POST", f.startPath(), body, "fixture-body-start", tag, "fixture-management")
	if w.Code != 400 {
		t.Fatal("ordinary Start accepted restore", w.Code)
	}
}
func TestResumeAPIPreflightRevocationAndUnknownReceipt(t *testing.T) {
	for _, mode := range []string{"restore_bad", "restore_revoke", "restore_no_handle", ""} {
		t.Run(mode, func(t *testing.T) {
			f, body, tag := apiRestoreFixture(t, mode)
			w := executionRequest(f.h, "POST", restorePath(f), body, "fixture-resume", tag, "fixture-management")
			want := 409
			if mode == "restore_revoke" {
				want = 401
			}
			if mode == "" {
				want = 503
			}
			if w.Code != want {
				t.Fatal(w.Code, w.Body.String())
			}
			if bytes.Contains(w.Body.Bytes(), []byte("fixture-sensitive")) {
				t.Fatal("raw error disclosed")
			}
			if mode == "restore_no_handle" {
				reply := executionReply(t, w)
				if !reply.Created || reply.Run.State != "interrupted" {
					t.Fatal("unknown intent lost")
				}
				if _, e := f.st.Reservation(reply.Run.ID); e != nil {
					t.Fatal("unknown restore freed capacity")
				}
				again := executionRequest(f.h, "POST", restorePath(f), body, "fixture-resume", tag, "fixture-management")
				got := executionReply(t, again)
				if again.Code != 200 || got.Created || got.Run.ID != reply.Run.ID || f.started.Load() != 2 {
					t.Fatal("unknown receipt replayed")
				}
				if _, e := f.st.Reservation(reply.Run.ID); errors.Is(e, store.ErrNotFound) {
					t.Fatal("retry released capacity")
				}
			} else if f.started.Load() != 1 {
				t.Fatal("preflight refusal invoked Runtime")
			}
		})
	}
}

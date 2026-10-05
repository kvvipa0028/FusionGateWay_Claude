//go:build darwin

package bootstrap

import (
	"encoding/json"
	"flag"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/fusion/api"
	"github.com/yetone/magpie/internal/fusion/evidence"
	"github.com/yetone/magpie/internal/fusion/store"
)

var humanCapture = flag.String("fusion-human-control-capture", "", "optional absolute actual Native bridge contract sample output")

type humanSample struct {
	Method         string            `json:"method"`
	Path           string            `json:"path"`
	Status         int               `json:"status"`
	MediaType      string            `json:"media_type"`
	Body           any               `json:"body"`
	Request        any               `json:"request,omitempty"`
	Headers        map[string]string `json:"headers"`
	RequestHeaders map[string]string `json:"request_headers,omitempty"`
}

func humanReply(t *testing.T, w *httptest.ResponseRecorder) api.HumanDecisionReply {
	t.Helper()
	var out api.HumanDecisionReply
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatal("Native human reply", w.Code, w.Body.String())
	}
	if w.Header().Get("ETag") != nativeControlTag(out.Task) {
		t.Fatal("Native human reply condition mismatch")
	}
	return out
}
func nativeHumanDecision(t *testing.T, h *ControlHost, taskID, action, reason string, probes bool) store.HumanAcceptanceDecision {
	t.Helper()
	bridge, err := newNativeTestBridge(t, h)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	path := "/control/v1/tasks/" + taskID + "/workflow/decision"
	w := nativeControlRequest(bridge, "GET", path, "", "", "")
	out := humanReply(t, w)
	if out.Report == nil || out.Unavailable != "" || out.Decision != nil || out.Task.ID != taskID {
		t.Fatal("current actual final report absent")
	}
	report := out.Report
	in := api.HumanDecisionRequest{RunID: report.RunID, TextHash: report.TextHash, TreeHash: report.TreeHash, SpecHash: report.SpecHash, DesignHash: report.DesignHash, AcceptanceHash: report.AcceptanceHash, Action: action, Reason: reason}
	raw, _ := json.Marshal(in)
	tag := w.Header().Get("ETag")
	var samples []humanSample
	capture := func(method, request, condition string, res *httptest.ResponseRecorder) {
		if *humanCapture == "" || !strings.HasSuffix(t.Name(), "/success") {
			return
		}
		var body, req any
		if json.Unmarshal(res.Body.Bytes(), &body) != nil {
			t.Fatal("actual contract response is not JSON")
		}
		if request != "" {
			if json.Unmarshal([]byte(request), &req) != nil {
				t.Fatal("actual contract request is not JSON")
			}
		}
		headers := map[string]string{}
		if condition != "" {
			headers["If-Match"] = condition
		}
		samples = append(samples, humanSample{method, path, res.Code, strings.Split(res.Header().Get("Content-Type"), ";")[0], body, req, map[string]string{"ETag": res.Header().Get("ETag")}, headers})
	}
	capture("GET", "", "", w)
	before, _ := h.store.Events(taskID, 0)
	if probes {
		wrong := in
		wrong.TextHash = strings.Repeat("f", 64)
		b, _ := json.Marshal(wrong)
		for _, tc := range []struct {
			body, tag, key string
			want           int
		}{
			{string(b), tag, "", 412}, {string(raw), "\"p1-g0-ready\"", "", 412}, {string(raw), "", "", 428},
			{strings.TrimSuffix(string(raw), "}") + `,"passed":true}`, tag, "", 400}, {string(raw), tag, "forbidden-key", 400},
		} {
			res := nativeControlRequest(bridge, "POST", path, tc.body, tc.key, tc.tag)
			if res.Code != tc.want {
				t.Fatal("human guard", res.Code, tc.want, res.Body.String())
			}
		}
		invalid := nativeRequest("GET", path, "")
		invalid.Header.Set("X-Wails-Window-Id", "2")
		res := httptest.NewRecorder()
		bridge.ServeHTTP(res, invalid)
		if res.Code == 200 {
			t.Fatal("foreign Native window read decision")
		}
		if report.Hard.Status == evidence.Superseded || !report.ModelValid || report.Model.Verdict != "accepted" {
			denied := in
			denied.Action = "accept"
			b, _ := json.Marshal(denied)
			res := nativeControlRequest(bridge, "POST", path, string(b), "", tag)
			if res.Code != 409 {
				t.Fatal("nonpassing evidence authorized human accept", res.Code, res.Body.String())
			}
		}
		after, _ := h.store.Events(taskID, 0)
		if !reflect.DeepEqual(before, after) {
			t.Fatal("rejected decision wrote events")
		}
	}
	w = nativeControlRequest(bridge, "POST", path, string(raw), "", tag)
	decided := humanReply(t, w)
	if decided.Decision == nil || decided.Decision.Action != action || decided.Decision.Reason != reason || decided.Decision.Authority != "management" || decided.Decision.TaskID != taskID || decided.Decision.RunID != report.RunID || decided.Decision.TreeHash != report.TreeHash {
		t.Fatal("Native decision lost current bound human receipt")
	}
	capture("POST", string(raw), tag, w)
	after, _ := h.store.Events(taskID, 0)
	repeated := humanReply(t, nativeControlRequest(bridge, "POST", path, string(raw), "", w.Header().Get("ETag")))
	final, _ := h.store.Events(taskID, 0)
	if !reflect.DeepEqual(repeated.Decision, decided.Decision) || !reflect.DeepEqual(after, final) {
		t.Fatal("exact retry changed durable human receipt or events")
	}
	conflicting := in
	conflicting.Reason = "other decision"
	b, _ := json.Marshal(conflicting)
	res := nativeControlRequest(bridge, "POST", path, string(b), "", w.Header().Get("ETag"))
	if res.Code != 409 {
		t.Fatal("conflicting human receipt replaced", res.Code, res.Body.String())
	}
	if len(samples) > 0 {
		if !filepath.IsAbs(*humanCapture) {
			t.Fatal("capture requires absolute owned path")
		}
		raw, _ := json.MarshalIndent(samples, "", "  ")
		if err := os.WriteFile(*humanCapture, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return *decided.Decision
}
func verifyNativeHumanReadback(t *testing.T, h *ControlHost, taskID string, expected store.HumanAcceptanceDecision) {
	t.Helper()
	bridge, err := newNativeTestBridge(t, h)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	out := humanReply(t, nativeControlRequest(bridge, "GET", "/control/v1/tasks/"+taskID+"/workflow/decision", "", "", ""))
	if out.Decision == nil || *out.Decision != expected || out.Report == nil {
		t.Fatal("restart Native read lost separate human decision")
	}
}

func verifyNativeHumanRevoked(t *testing.T, h *ControlHost, taskID string, commit bool, activate func()) {
	t.Helper()
	bridge, err := newNativeTestBridge(t, h)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	path := "/control/v1/tasks/" + taskID + "/workflow/decision"
	w := nativeControlRequest(bridge, "GET", path, "", "", "")
	out := humanReply(t, w)
	r := out.Report
	if r == nil {
		t.Fatal("missing owned report before revoke")
	}
	raw, _ := json.Marshal(api.HumanDecisionRequest{RunID: r.RunID, TextHash: r.TextHash, TreeHash: r.TreeHash, SpecHash: r.SpecHash, DesignHash: r.DesignHash, AcceptanceHash: r.AcceptanceHash, Action: "accept", Reason: "authorized before revocation"})
	before, _ := h.store.Events(taskID, 0)
	budget, _ := h.store.Budget(taskID)
	activate()
	var denied *httptest.ResponseRecorder
	want := 503
	if commit {
		want = 401
		denied = nativeControlRequest(bridge, "POST", path, string(raw), "", w.Header().Get("ETag"))
	} else {
		denied = nativeControlRequest(bridge, "GET", path, "", "", "")
	}
	if denied.Code != want {
		t.Fatal("late revoked human authority returned success", denied.Code, denied.Body.String())
	}
	after, _ := h.store.Events(taskID, 0)
	laterBudget, _ := h.store.Budget(taskID)
	task, _ := h.store.Task(taskID)
	if _, err := h.store.HumanDecision(taskID); err != store.ErrNotFound || task.State != "advisory_only" || budget != laterBudget || !reflect.DeepEqual(before, after) {
		t.Fatal("revoked human control wrote decision/task/event/budget", err)
	}
}

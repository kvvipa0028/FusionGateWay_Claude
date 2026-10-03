package api

import (
	"bufio"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var contractOutput = flag.String("fusion-api-contract-out", "", "optional absolute JSON output for offline OpenAPI contract verification")

type contractSample struct {
	Method         string            `json:"method"`
	Path           string            `json:"path"`
	Status         int               `json:"status"`
	MediaType      string            `json:"media_type"`
	Body           any               `json:"body"`
	Request        any               `json:"request,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	RequestHeaders map[string]string `json:"request_headers,omitempty"`
}

// These are serialized responses from the implemented Handler, not manually
// written schema examples. All accounts, routes and execution are fixtures.
func TestImplementedAPIContractSamples(t *testing.T) {
	var samples []contractSample
	add := func(method, path, body string, expected int, w *httptest.ResponseRecorder, headers map[string]string) {
		t.Helper()
		if w.Code != expected {
			t.Fatalf("contract capture %s %s: status %d", method, path, w.Code)
		}
		v := contractSample{Method: method, Path: path, Status: expected, MediaType: strings.Split(w.Header().Get("Content-Type"), ";")[0], RequestHeaders: headers, Headers: map[string]string{}}
		if v.MediaType == "application/json" {
			if json.Unmarshal(w.Body.Bytes(), &v.Body) != nil {
				t.Fatal("invalid Handler JSON")
			}
		} else {
			v.Body = w.Body.String()
		}
		if body != "" && json.Unmarshal([]byte(body), &v.Request) != nil {
			t.Fatal("invalid fixture request JSON")
		}
		for _, name := range []string{"ETag", "X-Fusion-Task-ETag", "Location"} {
			if value := w.Header().Get(name); value != "" {
				v.Headers[name] = value
			}
		}
		samples = append(samples, v)
	}
	_, _, h := presetSetup(t)
	pbody := `{"project_id":"fixture-project","goal":"fixture-goal","required_roles":["design"]}`
	w := request(h, "POST", "/control/v1/tasks/preview", pbody, "", "fixture-management")
	add("POST", "/control/v1/tasks/preview", pbody, 200, w, nil)
	var p Preview
	json.Unmarshal(w.Body.Bytes(), &p)
	sbody, _ := json.Marshal(SubmitRequest{PreviewID: p.ID, PlanHash: p.Plan.Hash})
	w = submit(t, h, p, "fixture-contract-submit")
	add("POST", "/agent/v1/tasks", string(sbody), 201, w, map[string]string{"Idempotency-Key": "fixture-contract-submit"})
	var task struct {
		ID string `json:"id"`
	}
	json.Unmarshal(w.Body.Bytes(), &task)
	for _, path := range []string{"/agent/v1/tasks/" + task.ID, "/control/v1/tasks/" + task.ID + "/budget", "/control/v1/tasks/" + task.ID + "/plan", "/control/v1/tasks/" + task.ID + "/preset", "/control/v1/projects/fixture-project/configuration"} {
		add("GET", path, "", 200, request(h, "GET", path, "", "", "fixture-management"), nil)
	}
	add("PUT", presetPath, firstPresetBody, 201, revisionRequest(h, "PUT", presetPath, firstPresetBody, `"0"`), map[string]string{"If-Match": `"0"`})
	add("PUT", presetPath, firstPresetBody, 200, revisionRequest(h, "PUT", presetPath, firstPresetBody, `"0"`), map[string]string{"If-Match": `"0"`})
	for _, path := range []string{presetsPath, presetPath, presetPath + "/versions/1"} {
		add("GET", path, "", 200, request(h, "GET", path, "", "", "fixture-management"), nil)
	}
	pbody = `{"project_id":"fixture-project","goal":"fixture-goal","required_roles":["design"],"preset":{"id":"fixture-preset","revision":1}}`
	w = request(h, "POST", "/control/v1/tasks/preview", pbody, "", "fixture-management")
	add("POST", "/control/v1/tasks/preview", pbody, 200, w, nil)
	json.Unmarshal(w.Body.Bytes(), &p)
	sbody, _ = json.Marshal(SubmitRequest{PreviewID: p.ID, PlanHash: p.Plan.Hash})
	w = submit(t, h, p, "fixture-contract-preset-submit")
	add("POST", "/agent/v1/tasks", string(sbody), 201, w, map[string]string{"Idempotency-Key": "fixture-contract-preset-submit"})
	json.Unmarshal(w.Body.Bytes(), &task)
	add("GET", "/control/v1/tasks/"+task.ID+"/preset", "", 200, request(h, "GET", "/control/v1/tasks/"+task.ID+"/preset", "", "", "fixture-management"), nil)
	for _, path := range []string{globalDefaultsPath, projectDefaultsPath} {
		add("GET", path, "", 200, request(h, "GET", path, "", "", "fixture-management"), nil)
		add("PUT", path, defaultsA, 201, revisionRequest(h, "PUT", path, defaultsA, `"0"`), map[string]string{"If-Match": `"0"`})
		add("GET", path, "", 200, request(h, "GET", path, "", "", "fixture-management"), nil)
		add("GET", path+"/versions/1", "", 200, request(h, "GET", path+"/versions/1", "", "", "fixture-management"), nil)
	}
	add("PUT", projectDefaultsPath, defaultsA, 200, revisionRequest(h, "PUT", projectDefaultsPath, defaultsA, `"0"`), map[string]string{"If-Match": `"0"`})
	_, _, rh, rt := revisionSetup(t)
	rpath := "/control/v1/tasks/" + rt.ID + "/plan/preview"
	w = revisionRequest(rh, "POST", rpath, reviewChange, `"1"`)
	add("POST", rpath, reviewChange, 200, w, map[string]string{"If-Match": `"1"`})
	json.Unmarshal(w.Body.Bytes(), &p)
	sbody, _ = json.Marshal(SubmitRequest{PreviewID: p.ID, PlanHash: p.Plan.Hash})
	add("PUT", "/control/v1/tasks/"+rt.ID+"/plan", string(sbody), 200, applyRevision(rh, rt.ID, p, `"1"`), map[string]string{"If-Match": `"1"`})
	q := setupQuotaAPI(t)
	add("GET", quotaPath, "", 200, request(q.h, "GET", quotaPath, "", "", "fixture-management"), nil)
	add("POST", quotaRefreshPath, `{}`, 200, request(q.h, "POST", quotaRefreshPath, `{}`, "", "fixture-management"), nil)
	pausedFixture := setupExecution(t, "")
	controlPath := "/control/v1/tasks/" + pausedFixture.task.ID
	for _, action := range []string{"pause", "pause", "continue", "continue"} {
		tag := pausedFixture.tag(t)
		path := controlPath + "/" + action
		add("POST", path, `{}`, 200, executionRequest(pausedFixture.h, "POST", path, `{}`, "", tag, "fixture-management"), map[string]string{"If-Match": tag})
	}
	held := setupExecution(t, "no_stop")
	tag := held.tag(t)
	w = executionRequest(held.h, "POST", held.startPath(), `{"role":"design"}`, "fixture-contract-held", tag, "fixture-management")
	add("POST", held.startPath(), `{"role":"design"}`, 202, w, map[string]string{"If-Match": tag, "Idempotency-Key": "fixture-contract-held"})
	var heldReply ExecutionReply
	json.Unmarshal(w.Body.Bytes(), &heldReply)
	tag = held.tag(t)
	heldPath := "/control/v1/tasks/" + held.task.ID
	add("POST", heldPath+"/pause", `{}`, 202, executionRequest(held.h, "POST", heldPath+"/pause", `{}`, "", tag, "fixture-management"), map[string]string{"If-Match": tag})
	waitAPIExecution(t, held, heldReply.Run.ID)
	tag = held.tag(t)
	for _, action := range []string{"pause", "continue"} {
		add("POST", heldPath+"/"+action, `{}`, 409, executionRequest(held.h, "POST", heldPath+"/"+action, `{}`, "", tag, "fixture-management"), map[string]string{"If-Match": tag})
	}
	f := setupExecution(t, "normal")
	startPath := "/control/v1/tasks/" + f.task.ID + "/start"
	w = executionRequest(f.h, "POST", startPath, `{"role":"design"}`, "fixture-contract-start", taskETag(f.task), "fixture-management")
	add("POST", startPath, `{"role":"design"}`, 202, w, map[string]string{"If-Match": taskETag(f.task), "Idempotency-Key": "fixture-contract-start"})
	var started ExecutionReply
	json.Unmarshal(w.Body.Bytes(), &started)
	runPath := "/agent/v1/tasks/" + f.task.ID + "/runs/" + started.Run.ID
	add("GET", runPath, "", 200, request(f.h, "GET", runPath, "", "", "fixture-management"), nil)
	current, e := f.st.Task(f.task.ID)
	if e != nil {
		t.Fatal(e)
	}
	cancelPath := "/control/v1/tasks/" + f.task.ID + "/runs/" + started.Run.ID + "/cancel"
	add("POST", cancelPath, `{}`, 202, executionRequest(f.h, "POST", cancelPath, `{}`, "", taskETag(current), "fixture-management"), map[string]string{"If-Match": taskETag(current)})
	for _, v := range []struct {
		credential string
		status     int
	}{{"", 401}, {"fgs_fixture", 403}} {
		add("POST", "/control/v1/tasks/preview", pbody, v.status, request(h, "POST", "/control/v1/tasks/preview", pbody, "", v.credential), nil)
	}
	add("GET", "/control/v1/tasks/preview", "", 405, request(h, "GET", "/control/v1/tasks/preview", "", "", "fixture-management"), nil)
	add("POST", "/control/v1/tasks/preview", `{}`, 400, request(h, "POST", "/control/v1/tasks/preview", `{}`, "", "fixture-management"), nil)
	// Actual loopback SSE: capture its first durable event, then disconnect.
	ts := httptest.NewServer(h)
	defer ts.Close()
	eventPath := "/agent/v1/tasks/" + task.ID + "/events"
	req, _ := http.NewRequest("GET", ts.URL+eventPath, nil)
	req.Header.Set("Authorization", "Bearer fixture-management")
	resp, e := ts.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	found := false
	for scanner.Scan() {
		if raw, ok := strings.CutPrefix(scanner.Text(), "data: "); ok {
			var event any
			if json.Unmarshal([]byte(raw), &event) != nil || resp.StatusCode != 200 {
				t.Fatal("invalid durable SSE")
			}
			samples = append(samples, contractSample{Method: "GET", Path: eventPath, Status: 200, MediaType: "text/event-stream", Body: event})
			found = true
			break
		}
	}
	resp.Body.Close()
	if !found {
		t.Fatal("missing durable event", scanner.Err())
	}
	if *contractOutput != "" {
		if !filepath.IsAbs(*contractOutput) {
			t.Fatal("contract capture requires absolute output")
		}
		raw, e := json.MarshalIndent(samples, "", "  ")
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(*contractOutput, append(raw, '\n'), 0600); e != nil {
			t.Fatal(e)
		}
	}
	t.Logf("captured %d actual Handler response samples, including real loopback SSE; real models/quota 0", len(samples))
}

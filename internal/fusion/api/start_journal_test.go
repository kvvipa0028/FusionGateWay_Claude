package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

const pendingStartPath = "/control/v1/projects/fixture-project/start-request"

type startFixtureView struct {
	Key   string             `json:"key"`
	Task  store.Task         `json:"task"`
	Plan  stageplan.Snapshot `json:"plan"`
	Role  stageplan.Role     `json:"role"`
	ETag  string             `json:"etag"`
	State string             `json:"state"`
	RunID string             `json:"run_id"`
}

func readStartFixture(t *testing.T, w *httptest.ResponseRecorder) *startFixtureView {
	t.Helper()
	var reply struct {
		Request *startFixtureView `json:"request"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &reply) != nil {
		t.Fatal("start journal response", w.Code, w.Body.String())
	}
	return reply.Request
}
func startJournalTask(t *testing.T) (*Server, *store.Store, http.Handler, store.Task) {
	t.Helper()
	s, st, h := setup(t)
	w := submit(t, h, preview(t, h), "fixture-journal-task")
	var task store.Task
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &task) != nil {
		t.Fatal("task fixture", w.Code)
	}
	return s, st, h, task
}
func startJournalCall(h http.Handler, method, path, body, key, tag string) *httptest.ResponseRecorder {
	return executionRequest(h, method, path, body, key, tag, "fixture-management")
}

// Removing durable preparation/replay or deriving the original draft from the
// current Task would break this test after pause and Server restart.
func TestStartJournalAPIPrepareReplayAndSealWithoutController(t *testing.T) {
	s, st, h, task := startJournalTask(t)
	path := "/control/v1/tasks/" + task.ID + "/start-request"
	tag := `"p1-g0-ready"`
	key := "fixture-start-original"
	if readStartFixture(t, startJournalCall(h, "GET", pendingStartPath, "", "", "")) != nil {
		t.Fatal("invented pending start")
	}
	original := readStartFixture(t, startJournalCall(h, "POST", path, `{"role":"design"}`, key, tag))
	if original == nil || original.Key != key || original.Role != "design" || original.ETag != tag || original.State != "prepared" || original.RunID != "" || original.Task.ID != task.ID || original.Task.State != "ready" || original.Task.Generation != 0 || original.Plan.Revision != 1 || stageplan.VerifySnapshot(original.Plan) != nil {
		t.Fatal("original draft not trusted/frozen")
	}
	if got, e := st.Task(task.ID); e != nil || got.State != "ready" || got.Generation != 0 {
		t.Fatal("preparation launched task", e)
	}
	if w := startJournalCall(h, "POST", "/control/v1/tasks/"+task.ID+"/start", `{"role":"design"}`, key, tag); w.Code != 503 {
		t.Fatal("journal granted runtime", w.Code)
	}
	// This fixture has no Controller; use the trusted Store to advance its
	// idle Task, then prove the journal API still returns the old ready draft.
	if _, e := st.PauseTask(task.ID, store.TaskVersion{PlanRevision: 1, Generation: 0, State: "ready"}, "fixture-owner"); e != nil {
		t.Fatal("pause fixture", e)
	}
	// Reconfiguration/preview loss do not reinterpret saved metadata.
	if e := s.SetProject("fixture-project", configuration()); e != nil {
		t.Fatal(e)
	}
	restarted, e := New(st, policy.NewManager("fixture-management", nil, nil))
	if e != nil {
		t.Fatal(e)
	}
	if e := restarted.SetProject("fixture-project", configuration()); e != nil {
		t.Fatal(e)
	}
	h = restarted.Handler()
	for _, w := range []*httptest.ResponseRecorder{
		startJournalCall(h, "GET", pendingStartPath, "", "", ""),
		startJournalCall(h, "GET", path, "", key, tag),
		startJournalCall(h, "POST", path, `{"role":"design"}`, key, tag),
	} {
		if got := readStartFixture(t, w); !reflect.DeepEqual(got, original) {
			t.Fatal("original draft changed")
		}
	}
	sealed := readStartFixture(t, startJournalCall(h, "POST", path+"/abandon", `{"role":"design"}`, key, tag))
	if sealed.State != "abandoned" || sealed.RunID != "" {
		t.Fatal("seal unavailable")
	}
	if readStartFixture(t, startJournalCall(h, "GET", pendingStartPath, "", "", "")) != nil {
		t.Fatal("seal still pending")
	}
	for _, w := range []*httptest.ResponseRecorder{startJournalCall(h, "GET", path, "", key, tag), startJournalCall(h, "POST", path+"/abandon", `{"role":"design"}`, key, tag)} {
		if !reflect.DeepEqual(readStartFixture(t, w), sealed) {
			t.Fatal("lost seal reply not recoverable")
		}
	}
	if _, e := st.LookupStart(key, store.StartIdentity{TaskID: task.ID, Role: "design", PlanRevision: 1, Generation: 0}); e != store.ErrConflict {
		t.Fatal("seal did not fence late start", e)
	}
}

func TestStartJournalAPIExactCommittedRunAndAck(t *testing.T) {
	f := setupExecution(t, "")
	path := "/control/v1/tasks/" + f.task.ID + "/start-request"
	key := "fixture-start-execute"
	tag := `"p1-g0-ready"`
	original := readStartFixture(t, startJournalCall(f.h, "POST", path, `{"role":"design"}`, key, tag))
	if f.started.Load() != 0 {
		t.Fatal("prepare executed")
	}
	w := startJournalCall(f.h, "POST", f.startPath(), `{"role":"design"}`, key, tag)
	var run ExecutionReply
	if w.Code != 202 || json.Unmarshal(w.Body.Bytes(), &run) != nil || !run.Created {
		t.Fatal("start fixture", w.Code)
	}
	got := readStartFixture(t, startJournalCall(f.h, "GET", path, "", key, tag))
	if got.State != "committed" || got.RunID != run.Run.ID || got.Task.State != "ready" || !reflect.DeepEqual(got.Plan, original.Plan) {
		t.Fatal("committed original not preserved")
	}
	if w := startJournalCall(f.h, "POST", f.startPath(), `{"role":"design"}`, key, tag); w.Code != 200 || f.started.Load() != 1 {
		t.Fatal("original retry launched twice", w.Code)
	}
	if w := startJournalCall(f.h, "POST", path+"/abandon", `{"role":"design"}`, key, tag); w.Code != 409 {
		t.Fatal("written run sealed", w.Code)
	}
	if w := startJournalCall(f.h, "POST", path+"/acknowledge", `{"role":"design","run_id":"wrong-run"}`, key, tag); w.Code != 409 {
		t.Fatal("wrong run ack", w.Code)
	}
	before, e := f.st.Task(f.task.ID)
	if e != nil {
		t.Fatal(e)
	}
	body := `{"role":"design","run_id":"` + run.Run.ID + `"}`
	ack := readStartFixture(t, startJournalCall(f.h, "POST", path+"/acknowledge", body, key, tag))
	after, e := f.st.Task(f.task.ID)
	if e != nil || !reflect.DeepEqual(before, after) || ack.State != "acknowledged" || ack.RunID != run.Run.ID {
		t.Fatal("ack mutated Task/run")
	}
	if readStartFixture(t, startJournalCall(f.h, "GET", pendingStartPath, "", "", "")) != nil {
		t.Fatal("ack still pending")
	}
	if !reflect.DeepEqual(readStartFixture(t, startJournalCall(f.h, "GET", path, "", key, tag)), ack) || !reflect.DeepEqual(readStartFixture(t, startJournalCall(f.h, "POST", path+"/acknowledge", body, key, tag)), ack) {
		t.Fatal("lost ack not recoverable")
	}
}

func TestStartJournalAPIRejectsIdentityAndUntrustedInputs(t *testing.T) {
	_, st, h, task := startJournalTask(t)
	path := "/control/v1/tasks/" + task.ID + "/start-request"
	key := "fixture-original-key"
	tag := `"p1-g0-ready"`
	readStartFixture(t, startJournalCall(h, "POST", path, `{"role":"design"}`, key, tag))
	for _, tc := range []struct {
		method, path, body, key, tag string
		status                       int
	}{
		{"POST", path, `{"role":"design"}`, "another-key", tag, 409},
		{"POST", path, `{"role":"review"}`, key, tag, 409},
		{"POST", path, `{"role":"design"}`, key, `"p1-g1-ready"`, 409},
		{"GET", path, "", key, `"p1-g1-ready"`, 412},
		{"GET", path, "", key, `"p1-g0-paused"`, 412},
		{"GET", path, "", "missing-key", tag, 404},
		{"GET", path, "", key, "", 428},
		{"POST", path, `{"role":"design"}`, key, "", 428},
		{"GET", path, "", "", tag, 400},
		{"POST", path, `{"role":"design"}`, "", tag, 400},
		{"POST", path, `{"role":"design"}`, key, `W/"p1-g0-ready"`, 400},
		{"POST", path + "/abandon", `{"role":"review"}`, key, tag, 409},
		{"POST", path + "/acknowledge", `{"role":"design","run_id":"fixture-no-run"}`, key, tag, 409},
		{"GET", "/control/v1/projects/unregistered/start-request", "", "", "", 404},
		{"GET", "/control/v1/tasks/missing/start-request", "", key, tag, 404},
		{"POST", pendingStartPath, `{}`, "", "", 405},
		{"PUT", path, `{}`, key, tag, 405},
		{"GET", path + "/abandon", "", key, tag, 405},
		{"POST", path + "/unknown", `{}`, key, tag, 400},
		{"GET", path + "/abandon/extra", "", key, tag, 400},
		{"GET", path + "?after=0", "", key, tag, 400},
	} {
		t.Run(tc.method+tc.path+tc.key+tc.tag, func(t *testing.T) {
			w := startJournalCall(h, tc.method, tc.path, tc.body, tc.key, tc.tag)
			if w.Code != tc.status {
				t.Fatalf("expected %d got %d", tc.status, w.Code)
			}
		})
	}
	for _, body := range []string{`null`, `{}`, `{"role":"bad"}`, `{"role":"design","role":"review"}`, `{"role":"design","task":{}}`, `{"role":"design","plan":{}}`, `{"role":"design","token":"do-not-echo"}`, `{"role":"design","run_id":"arbitrary"}`} {
		if w := startJournalCall(h, "POST", path, body, key, tag); w.Code != 400 || strings.Contains(w.Body.String(), "do-not-echo") {
			t.Fatal("untrusted input", w.Code)
		}
	}
	for _, header := range []string{"If-Match", "Idempotency-Key"} {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Authorization", "Bearer fixture-management")
		r.Header.Set("If-Match", tag)
		r.Header.Set("Idempotency-Key", key)
		r.Header.Add(header, r.Header.Get(header))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal("duplicate header", header, w.Code)
		}
	}
	if w := executionRequest(h, "GET", pendingStartPath, "", "", "", ""); w.Code != 401 {
		t.Fatal("unauthorized", w.Code)
	}
	if w := executionRequest(h, "GET", pendingStartPath, "", "", "", "fgs_fixture-stage"); w.Code != 403 {
		t.Fatal("stage credential", w.Code)
	}
	current, e := st.Task(task.ID)
	if e != nil || current.State != "ready" || current.Generation != 0 {
		t.Fatal("rejections changed Task")
	}
}

func TestStartJournalAPINewPrepareRequiresCurrentReadyTask(t *testing.T) {
	for _, tc := range []struct {
		tag    string
		status int
	}{{`"p2-g0-ready"`, 412}, {`"p1-g1-ready"`, 412}, {`"p1-g0-paused"`, 412}} {
		t.Run(tc.tag, func(t *testing.T) {
			_, st, h, task := startJournalTask(t)
			path := "/control/v1/tasks/" + task.ID + "/start-request"
			if w := startJournalCall(h, "POST", path, `{"role":"design"}`, "new-key", tc.tag); w.Code != tc.status {
				t.Fatal(w.Code)
			}
			if _, e := st.PendingStart("fixture-project"); e != store.ErrNotFound {
				t.Fatal("rejection persisted", e)
			}
		})
	}
}

func TestStartJournalAPIRechecksManagementAfterWaiting(t *testing.T) {
	for _, action := range []string{"read", "abandon"} {
		for _, loss := range []string{"revoke", "cancel"} {
			t.Run(action+"-"+loss, func(t *testing.T) {
				s, st, h, task := startJournalTask(t)
				path := "/control/v1/tasks/" + task.ID + "/start-request"
				key := "fixture-waiting-key"
				readStartFixture(t, startJournalCall(h, "POST", path, `{"role":"design"}`, key, `"p1-g0-ready"`))
				entered := make(chan struct{})
				// Exercise the actual locked consumer after a deterministic mutex wait.
				// A second Management middleware would reject earlier and mask its guard.
				wrapped := s.auth.Management(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					close(entered)
					s.mu.Lock()
					parts := []string{"fixture-project", "start-request"}
					project := true
					if action == "abandon" {
						parts = []string{task.ID, "start-request", "abandon"}
						project = false
					}
					value, e := s.startJournalLocked(r, parts, project, key, taskCondition{revision: 1, generation: 0, state: "ready", raw: `"p1-g0-ready"`}, AcknowledgeStartRequest{Role: "design"})
					s.mu.Unlock()
					if e != nil {
						controlFailure(w, e)
						return
					}
					respond(w, 200, StartRequestReply{Request: startRequestView(value)})
				}))
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				method, target := "GET", pendingStartPath
				if action == "abandon" {
					method, target = "POST", path+"/abandon"
				}
				r := httptest.NewRequest(method, target, nil).WithContext(ctx)
				r.Header.Set("Authorization", "Bearer fixture-management")
				s.mu.Lock()
				result := make(chan *httptest.ResponseRecorder, 1)
				go func() { w := httptest.NewRecorder(); wrapped.ServeHTTP(w, r); result <- w }()
				<-entered
				if loss == "revoke" {
					s.auth.RevokeManagement()
				} else {
					cancel()
				}
				s.mu.Unlock()
				w := <-result
				if w.Code != 401 || strings.Contains(w.Body.String(), key) || strings.Contains(w.Body.String(), task.Goal) {
					t.Fatal("revoked consumer disclosed original", w.Code)
				}
				if got, e := st.PendingStart("fixture-project"); e != nil || got.State != "prepared" {
					t.Fatal("revoked consumer mutated journal", e)
				}
			})
		}
	}
}

func TestStartJournalAPIHonorsCancelledContext(t *testing.T) {
	_, st, h, task := startJournalTask(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("POST", "/control/v1/tasks/"+task.ID+"/start-request", strings.NewReader(`{"role":"design"}`)).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer fixture-management")
	r.Header.Set("If-Match", `"p1-g0-ready"`)
	r.Header.Set("Idempotency-Key", "cancelled-key")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("cancelled prepare accepted", w.Code)
	}
	if _, e := st.PendingStart("fixture-project"); e != store.ErrNotFound {
		t.Fatal("cancelled prepare persisted", e)
	}
}

func TestStartJournalAPIScopesCurrentRegisteredTaskAndSinglePendingProject(t *testing.T) {
	s, st, h, task := startJournalTask(t)
	path := "/control/v1/tasks/" + task.ID + "/start-request"
	key := "fixture-scoped-original"
	tag := `"p1-g0-ready"`
	original := readStartFixture(t, startJournalCall(h, "POST", path, `{"role":"design"}`, key, tag))
	other, e := st.Create("fixture-other-start", store.CreateRequest{ProjectID: "fixture-project", Goal: "another-goal", Plan: original.Plan})
	if e != nil {
		t.Fatal(e)
	}
	otherPath := "/control/v1/tasks/" + other.ID + "/start-request"
	if w := startJournalCall(h, "GET", otherPath, "", key, tag); w.Code != 404 {
		t.Fatal("foreign request returned", w.Code)
	}
	if w := startJournalCall(h, "POST", otherPath, `{"role":"design"}`, "different-start-key", tag); w.Code != 409 {
		t.Fatal("two pending project requests", w.Code)
	}
	foreign, e := st.Create("fixture-foreign-project-start", store.CreateRequest{ProjectID: "unregistered-project", Goal: "foreign-private-goal", Plan: original.Plan})
	if e != nil {
		t.Fatal(e)
	}
	if w := startJournalCall(h, "GET", "/control/v1/tasks/"+foreign.ID+"/start-request", "", key, tag); w.Code != 404 || strings.Contains(w.Body.String(), foreign.Goal) {
		t.Fatal("unregistered task disclosed", w.Code)
	}
	s.mu.Lock()
	delete(s.projects, "fixture-project")
	s.mu.Unlock()
	for _, target := range []string{path, pendingStartPath} {
		if w := startJournalCall(h, "GET", target, "", key, tag); w.Code != 404 || strings.Contains(w.Body.String(), task.Goal) || strings.Contains(w.Body.String(), key) {
			t.Fatal("removed project disclosed history", w.Code)
		}
	}
}

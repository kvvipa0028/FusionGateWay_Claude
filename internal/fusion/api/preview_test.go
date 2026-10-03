package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

func configuration() ProjectConfiguration {
	effort := "medium"
	route := stageplan.Route{ID: "fixture-route", Revision: 1, Model: "fixture-model-a", Account: "fixture-account", Workspace: "fixture-workspace", CredentialIdentity: "fixture-credential", RuntimeVersion: "fixture-runtime", BillingPath: "fixture-subscription", BillingKnown: true, Admitted: true, Efforts: []string{effort}, DefaultEffort: &effort, LockEnforcement: stageplan.ControlledCalls}
	binding := stageplan.Binding{Mode: stageplan.Locked, Model: route.Model, Route: &stageplan.RouteRef{ID: route.ID, Revision: 1}, Effort: &stageplan.EffortSelection{Mode: stageplan.EffortDefault}}
	return ProjectConfiguration{Global: stageplan.Layer{Roles: map[stageplan.Role]stageplan.Binding{stageplan.Design: binding}}, Routes: []stageplan.Route{route}}
}
func setup(t *testing.T) (*Server, *store.Store, http.Handler) {
	t.Helper()
	root := t.TempDir()
	os.Chmod(root, 0700)
	root, _ = filepath.EvalSymlinks(root)
	st, e := store.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { st.Close() })
	s, e := New(st, policy.NewManager("fixture-management", nil, nil))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.SetProject("fixture-project", configuration()); e != nil {
		t.Fatal(e)
	}
	return s, st, s.Handler()
}
func request(h http.Handler, method, path, body, key, credential string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")
	if credential != "" {
		r.Header.Set("Authorization", "Bearer "+credential)
	}
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func preview(t *testing.T, h http.Handler) Preview {
	t.Helper()
	w := request(h, "POST", "/control/v1/tasks/preview", `{"project_id":"fixture-project","goal":"fixture-goal","required_roles":["design"]}`, "", "fixture-management")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var p Preview
	if e := json.Unmarshal(w.Body.Bytes(), &p); e != nil {
		t.Fatal(e)
	}
	return p
}
func submit(t *testing.T, h http.Handler, p Preview, key string) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(SubmitRequest{PreviewID: p.ID, PlanHash: p.Plan.Hash})
	return request(h, "POST", "/agent/v1/tasks", string(raw), key, "fixture-management")
}
func TestPreviewAndSubmissionFreezeExactServerPlan(t *testing.T) {
	_, st, h := setup(t)
	p := preview(t, h)
	if e := stageplan.VerifySnapshot(p.Plan); e != nil {
		t.Fatal(e)
	}
	w := submit(t, h, p, "fixture-key")
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var task store.Task
	json.Unmarshal(w.Body.Bytes(), &task)
	got, e := st.Plan(task.ID, task.PlanRevision)
	if e != nil || got.Hash != p.Plan.Hash {
		t.Fatal("preview replaced on submit", got, e)
	}
	events, e := st.Events(task.ID, 0)
	if e != nil || len(events) != 1 || events[0].Kind != "created" {
		t.Fatal("preview/submission started a model attempt", events, e)
	}
	w = request(h, "GET", "/agent/v1/tasks/"+task.ID, "", "", "fixture-management")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
}
func TestStalePreviewAndTamperedHashCannotCreateTask(t *testing.T) {
	s, _, h := setup(t)
	p := preview(t, h)
	if e := s.SetProject("fixture-project", configuration()); e != nil {
		t.Fatal(e)
	}
	if w := submit(t, h, p, "fixture-key"); w.Code != 409 {
		t.Fatal("stale preview accepted", w.Code)
	}
	p = preview(t, h)
	p.Plan.Hash = "fixture-tampered"
	if w := submit(t, h, p, "fixture-key"); w.Code != 409 {
		t.Fatal("client hash adopted", w.Code)
	}
}
func TestConcurrentIdempotentSubmitCreatesOnePersistedTask(t *testing.T) {
	_, st, h := setup(t)
	p := preview(t, h)
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	failures := make(chan int, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := submit(t, h, p, "fixture-key")
			var task store.Task
			json.Unmarshal(w.Body.Bytes(), &task)
			ids <- task.ID
			failures <- w.Code
		}()
	}
	wg.Wait()
	close(ids)
	close(failures)
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		if id == "" || id != first {
			t.Fatal("duplicate or lost task")
		}
	}
	for code := range failures {
		if code != 201 {
			t.Fatal(code)
		}
	}
	events, e := st.Events(first, 0)
	if e != nil || len(events) != 1 {
		t.Fatal("duplicate creation", events, e)
	}
	if w := submit(t, h, p, "fixture-other-key"); w.Code != 409 {
		t.Fatal("receipt reused under different key", w.Code)
	}
}
func TestBodyCannotSupplyCredentialsWorkspaceGlobalOrFrozenAdmission(t *testing.T) {
	_, _, h := setup(t)
	for _, body := range []string{
		`{"project_id":"fixture-project","goal":"x","required_roles":["design"],"api_key":"fixture-secret"}`,
		`{"project_id":"fixture-project","goal":"x","required_roles":["design"],"workspace":"/fixture-outside"}`,
		`{"project_id":"fixture-project","goal":"x","required_roles":["design"],"global":{}}`,
		`{"project_id":"fixture-project","goal":"x","required_roles":["design"],"task":{"roles":{"design":{"mode":"locked","admitted":true}}}}`,
		`{"project_id":"fixture-other-project","goal":"x","required_roles":["design"]}`,
	} {
		w := request(h, "POST", "/control/v1/tasks/preview", body, "", "fixture-management")
		if w.Code < 400 {
			t.Fatal("untrusted execution authority accepted")
		}
		if bytes.Contains(w.Body.Bytes(), []byte("fixture-secret")) {
			t.Fatal("secret echoed")
		}
	}
}
func TestMalformedOrAmbiguousRequestIsRejected(t *testing.T) {
	_, _, h := setup(t)
	for _, body := range []string{`null`, `{} {}`, `{"project_id":"fixture-project","project_id":"fixture-other"}`, `{"project_id":"fixture-project","PROJECT_ID":"fixture-other"}`, `{"project_id":"fixture-project","goal":"x","required_roles":["design"],"task":{"roles":{"design":{"mode":"inherit","Mode":"locked"}}}}`} {
		if w := request(h, "POST", "/control/v1/tasks/preview", body, "", "fixture-management"); w.Code != 400 {
			t.Fatal("ambiguous request accepted", w.Code)
		}
	}
}
func TestEveryTaskEndpointRequiresManagementAuthentication(t *testing.T) {
	_, _, h := setup(t)
	for _, path := range []string{"/control/v1/tasks/preview", "/agent/v1/tasks", "/agent/v1/tasks/fixture-task"} {
		for _, secret := range []string{"", "fixture-wrong", "fgs_fixture-stage"} {
			w := request(h, "POST", path, `{}`, "", secret)
			if w.Code != 401 && w.Code != 403 {
				t.Fatal("unauthorized endpoint", path, w.Code)
			}
		}
	}
	w := request(h, "POST", "/control/v1/tasks/preview?key=fixture-management", `{}`, "", "fixture-management")
	if w.Code != 403 {
		t.Fatal("query key accepted")
	}
}
func TestExpiredPreviewDoesNotCreateOrReplayExecution(t *testing.T) {
	s, _, h := setup(t)
	now := time.Now()
	s.now = func() time.Time { return now }
	p := preview(t, h)
	now = now.Add(6 * time.Minute)
	if w := submit(t, h, p, "fixture-key"); w.Code != 409 {
		t.Fatal("expired preview accepted")
	}
}
func TestConfigurationAndPreviewCannotMutateServerReferences(t *testing.T) {
	s, _, h := setup(t)
	c := configuration()
	if e := s.SetProject("fixture-project", c); e != nil {
		t.Fatal(e)
	}
	c.Routes[0].Model = "fixture-other"
	*c.Routes[0].DefaultEffort = "fixture-bad"
	p := preview(t, h)
	p.Plan.Bindings[stageplan.Design].Target.ResolvedModel = "fixture-other"
	w := submit(t, h, p, "fixture-key")
	if w.Code != 201 {
		t.Fatal("caller mutated stored preview", w.Code)
	}
}
func TestUnadmittedRouteHasNoPreviewFallback(t *testing.T) {
	s, _, h := setup(t)
	c := configuration()
	c.Routes[0].Admitted = false
	if e := s.SetProject("fixture-project", c); e != nil {
		t.Fatal(e)
	}
	w := request(h, "POST", "/control/v1/tasks/preview", `{"project_id":"fixture-project","goal":"x","required_roles":["design"]}`, "", "fixture-management")
	if w.Code != 422 {
		t.Fatal("unadmitted route previewed", w.Code)
	}
}

func TestCommittedRetryPreservesTaskAfterConfigurationChange(t *testing.T) {
	s, st, h := setup(t)
	p := preview(t, h)
	first := submit(t, h, p, "fixture-key")
	if first.Code != 201 {
		t.Fatal(first.Code)
	}
	var task store.Task
	json.Unmarshal(first.Body.Bytes(), &task)
	c := configuration()
	c.Routes[0].Admitted = false
	if e := s.SetProject("fixture-project", c); e != nil {
		t.Fatal(e)
	}
	retry := submit(t, h, p, "fixture-key")
	if retry.Code != 201 {
		t.Fatal(retry.Code)
	}
	var got store.Task
	json.Unmarshal(retry.Body.Bytes(), &got)
	if got.ID != task.ID {
		t.Fatal("committed request reinterpreted")
	}
	events, e := st.Events(task.ID, 0)
	if e != nil || len(events) != 1 {
		t.Fatal("retry launched/recreated work")
	}
}
func TestSameIdempotencyKeyCannotAdoptChangedGoal(t *testing.T) {
	_, _, h := setup(t)
	p := preview(t, h)
	if w := submit(t, h, p, "fixture-key"); w.Code != 201 {
		t.Fatal(w.Code)
	}
	w := request(h, "POST", "/control/v1/tasks/preview", `{"project_id":"fixture-project","goal":"fixture-changed-goal","required_roles":["design"]}`, "", "fixture-management")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	var changed Preview
	json.Unmarshal(w.Body.Bytes(), &changed)
	if w := submit(t, h, changed, "fixture-key"); w.Code != 409 {
		t.Fatal("idempotency payload conflict ignored", w.Code)
	}
}
func TestRequestSizeEncodingAndContentTypeAreBounded(t *testing.T) {
	_, _, h := setup(t)
	for _, body := range []string{string(bytes.Repeat([]byte("x"), maxBody+1)), string([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'})} {
		if w := request(h, "POST", "/control/v1/tasks/preview", body, "", "fixture-management"); w.Code != 400 {
			t.Fatal("invalid bytes accepted", w.Code)
		}
	}
	for _, mode := range []string{"compressed", "content_type"} {
		r := httptest.NewRequest("POST", "/control/v1/tasks/preview", bytes.NewBufferString(`{}`))
		r.Header.Set("Authorization", "Bearer fixture-management")
		r.Header.Set("Content-Type", "application/json")
		if mode == "compressed" {
			r.Header.Set("Content-Encoding", "gzip")
		} else {
			r.Header.Set("Content-Type", "text/plain")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal("unsupported request encoding", w.Code)
		}
	}
}

func TestPreviewFailureHasSafeActionableReason(t *testing.T) {
	s, _, h := setup(t)
	c := configuration()
	c.Routes[0].Admitted = false
	s.SetProject("fixture-project", c)
	w := request(h, "POST", "/control/v1/tasks/preview", `{"project_id":"fixture-project","goal":"fixture-private-prompt","required_roles":["design"]}`, "", "fixture-management")
	var got struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &got); e != nil || got.Error.Code != "route_revision_not_admitted" {
		t.Fatal("missing safe blocked reason")
	}
	if bytes.Contains(w.Body.Bytes(), []byte("fixture-private-prompt")) {
		t.Fatal("request leaked into error")
	}
}
func TestControllerRestartRequiresNewPreviewAndPreservesTaskIdempotency(t *testing.T) {
	_, st, h := setup(t)
	p := preview(t, h)
	first := submit(t, h, p, "fixture-key")
	var task store.Task
	json.Unmarshal(first.Body.Bytes(), &task)
	next, e := New(st, policy.NewManager("fixture-management", nil, nil))
	if e != nil {
		t.Fatal(e)
	}
	if e = next.SetProject("fixture-project", configuration()); e != nil {
		t.Fatal(e)
	}
	h = next.Handler()
	if w := submit(t, h, p, "fixture-key"); w.Code != 409 {
		t.Fatal("old process preview survived without proof")
	}
	fresh := preview(t, h)
	w := submit(t, h, fresh, "fixture-key")
	if w.Code != 201 {
		t.Fatal(w.Code)
	}
	var got store.Task
	json.Unmarshal(w.Body.Bytes(), &got)
	if got.ID != task.ID {
		t.Fatal("restart duplicated submitted task")
	}
	events, e := st.Events(task.ID, 0)
	if e != nil || len(events) != 1 {
		t.Fatal("restart reran creation")
	}
}

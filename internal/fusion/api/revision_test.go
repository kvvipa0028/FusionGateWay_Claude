package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

func revisionSetup(t *testing.T) (*Server, *store.Store, http.Handler, store.Task) {
	t.Helper()
	s, st, h := setup(t)
	c := configuration()
	c.Global.Roles[stageplan.Review] = c.Global.Roles[stageplan.Design]
	r := c.Routes[0]
	r.ID, r.Model = "fixture-route-b", "fixture-model-b"
	c.Routes = append(c.Routes, r)
	if e := s.SetProject("fixture-project", c); e != nil {
		t.Fatal(e)
	}
	w := request(h, "POST", "/control/v1/tasks/preview", `{"project_id":"fixture-project","goal":"fixture-goal","required_roles":["design","review"],"budget":{"max_calls":3,"max_reworks":1}}`, "", "fixture-management")
	var p Preview
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	w = submit(t, h, p, "fixture-key")
	var task store.Task
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &task) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	return s, st, h, task
}
func revisionRequest(h http.Handler, method, path, body string, tags ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer fixture-management")
	for _, tag := range tags {
		r.Header.Add("If-Match", tag)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

const reviewChange = `{"task":{"roles":{"review":{"mode":"locked","route":{"id":"fixture-route-b","revision":1},"model":"fixture-model-b","effort":{"mode":"default"}}}}}`

func revisionPreview(t *testing.T, h http.Handler, id string) Preview {
	t.Helper()
	w := revisionRequest(h, "POST", "/control/v1/tasks/"+id+"/plan/preview", reviewChange, `"1"`)
	var p Preview
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	return p
}
func applyRevision(h http.Handler, id string, p Preview, tag string) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(SubmitRequest{PreviewID: p.ID, PlanHash: p.Plan.Hash})
	return revisionRequest(h, "PUT", "/control/v1/tasks/"+id+"/plan", string(raw), tag)
}
func beginDesign(t *testing.T, st *store.Store, task store.Task) store.StageRun {
	t.Helper()
	p, e := st.Plan(task.ID, 1)
	if e != nil {
		t.Fatal(e)
	}
	r, e := st.StartReserved(store.StartRequest{TaskID: task.ID, Role: stageplan.Design, PlanRevision: 1, Owner: "fixture-worker", TTL: time.Minute, Target: *p.Bindings[stageplan.Design].Target}, store.ReservationRequest{PoolKey: "fixture-pool", GlobalLimit: 2, AdmissionHash: p.Hash})
	if e != nil {
		t.Fatal(e)
	}
	if e = st.ConfirmStarted(r.ID, r.Generation, r.Owner, "fixture-session"); e != nil {
		t.Fatal(e)
	}
	if e = st.ReserveCall(r.ID, r.Generation); e != nil {
		t.Fatal(e)
	}
	if e = st.ReserveRework(task.ID, r.Generation); e != nil {
		t.Fatal(e)
	}
	r, e = st.Run(r.ID)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestRevisionPreviewApplyPreservesRunningAttemptAndConsumedBudget(t *testing.T) {
	_, st, h, task := revisionSetup(t)
	run := beginDesign(t, st, task)
	claims := policy.Claims{TaskID: task.ID, RunID: run.ID, Role: run.Role, Attempt: run.Attempt, PlanRevision: run.PlanRevision, Generation: run.Generation, ProjectID: task.ProjectID, Audience: policy.ModelAudience}
	manager := policy.NewManager("fixture-admin", policy.StoreValidator(st), nil)
	secret, e := manager.Issue(claims, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	before, _ := st.Events(task.ID, 0)
	budget, _ := st.Budget(task.ID)
	p := revisionPreview(t, h, task.ID)
	after, _ := st.Events(task.ID, 0)
	if len(before) != len(after) || p.Plan.Revision != 2 || p.Budget.MaxCalls != 3 {
		t.Fatal("preview changed task or budget")
	}
	w := applyRevision(h, task.ID, p, `"1"`)
	if w.Code != 200 || w.Header().Get("ETag") != `"2"` {
		t.Fatal(w.Code, w.Body.String())
	}
	got, _ := st.Task(task.ID)
	currentRun, _ := st.Run(run.ID)
	currentBudget, _ := st.Budget(task.ID)
	old, _ := st.Plan(task.ID, 1)
	next, _ := st.Plan(task.ID, 2)
	if got.PlanRevision != 2 || got.Generation != run.Generation || got.State != "running" || !reflect.DeepEqual(run, currentRun) || !reflect.DeepEqual(budget, currentBudget) || next.Hash != p.Plan.Hash || !reflect.DeepEqual(old.Bindings[stageplan.Design], next.Bindings[stageplan.Design]) {
		t.Fatal("revision changed active attempt/history/budget")
	}
	if next.Bindings[stageplan.Review].Target.ResolvedModel != "fixture-model-b" {
		t.Fatal("selected role not changed")
	}
	if _, e = manager.AuthenticateStage(secret, claims); e != nil {
		t.Fatal("future-role revision invalidated current capability", e)
	}
	bad := claims
	bad.PlanRevision = 2
	if _, e = manager.AuthenticateStage(secret, bad); e == nil {
		t.Fatal("current capability adopted a different plan")
	}
	after, _ = st.Events(task.ID, 0)
	if len(after) != len(before)+1 || after[len(after)-1].Kind != "plan_revised" {
		t.Fatal("revision launched or duplicated work")
	}
	w = revisionRequest(h, "GET", "/control/v1/tasks/"+task.ID+"/plan", "")
	if w.Code != 200 || w.Header().Get("ETag") != `"2"` {
		t.Fatal("current revision unavailable")
	}
}
func TestRevisionStartedRoleAndPreviewStartRaceAreRejected(t *testing.T) {
	_, st, h, task := revisionSetup(t)
	w := revisionRequest(h, "POST", "/control/v1/tasks/"+task.ID+"/plan/preview", strings.Replace(reviewChange, "review", "design", 1), `"1"`)
	var p Preview
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil {
		t.Fatal(w.Code)
	}
	run := beginDesign(t, st, task)
	w = applyRevision(h, task.ID, p, `"1"`)
	if w.Code != 409 {
		t.Fatal("stage start after preview not rechecked", w.Code)
	}
	w = revisionRequest(h, "POST", "/control/v1/tasks/"+task.ID+"/plan/preview", `{"task":{"roles":{"design":{"mode":"locked","route":{"id":"fixture-route-b","revision":1},"model":"fixture-model-b","effort":{"mode":"default"}}}}}`, `"1"`)
	if w.Code != 409 {
		t.Fatal("running binding replaced", w.Code)
	}
	if e := st.Finish(run.ID, run.Generation, run.Owner, "succeeded"); e != nil {
		t.Fatal(e)
	}
	w = applyRevision(h, task.ID, p, `"1"`)
	if w.Code != 409 {
		t.Fatal("finished role revised", w.Code)
	}
}
func TestConcurrentRevisionReceiptsHaveOneWinnerAndReplayAddsNoEvent(t *testing.T) {
	_, st, h, task := revisionSetup(t)
	a, b := revisionPreview(t, h, task.ID), revisionPreview(t, h, task.ID)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for _, p := range []Preview{a, b} {
		wg.Add(1)
		go func(p Preview) { defer wg.Done(); codes <- applyRevision(h, task.ID, p, `"1"`).Code }(p)
	}
	wg.Wait()
	close(codes)
	winners, conflicts := 0, 0
	for code := range codes {
		if code == 200 {
			winners++
		}
		if code == 409 {
			conflicts++
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatal("concurrent overwrite", winners, conflicts)
	}
	before, _ := st.Events(task.ID, 0)
	winners, conflicts = 0, 0
	for _, p := range []Preview{a, b} {
		switch applyRevision(h, task.ID, p, `"1"`).Code {
		case 200:
			winners++
		case 409:
			conflicts++
		}
	}
	after, _ := st.Events(task.ID, 0)
	if len(after) != len(before) || winners != 1 || conflicts != 1 {
		t.Fatal("receipt replay wrote another revision")
	}
}
func TestRevisionReceiptBoundToTaskBaseHashPurposeAndConfiguration(t *testing.T) {
	s, st, h, task := revisionSetup(t)
	p := revisionPreview(t, h, task.ID)
	other, e := st.Create("fixture-other", store.CreateRequest{ProjectID: task.ProjectID, Goal: "fixture-other", Plan: p.Plan})
	// Create requires revision 1; use the original immutable plan instead.
	if e == nil {
		t.Fatal("store accepted revision 2 as initial plan", other)
	}
	old, _ := st.Plan(task.ID, 1)
	other, e = st.Create("fixture-other", store.CreateRequest{ProjectID: task.ProjectID, Goal: "fixture-other", Plan: old})
	if e != nil {
		t.Fatal(e)
	}
	if w := applyRevision(h, other.ID, p, `"1"`); w.Code != 409 {
		t.Fatal("cross-task receipt accepted", w.Code)
	}
	if w := applyRevision(h, task.ID, p, `"2"`); w.Code != 409 {
		t.Fatal("wrong receipt base accepted", w.Code)
	}
	bad := p
	bad.Plan.Hash = "fixture-tampered"
	if w := applyRevision(h, task.ID, bad, `"1"`); w.Code != 409 {
		t.Fatal("tampered receipt accepted", w.Code)
	}
	if w := submit(t, h, p, "fixture-create"); w.Code != 409 {
		t.Fatal("revision receipt created task", w.Code)
	}
	creation := preview(t, h)
	if w := applyRevision(h, task.ID, creation, `"1"`); w.Code != 409 {
		t.Fatal("create receipt revised task", w.Code)
	}
	if e = s.SetProject("fixture-project", configuration()); e != nil {
		t.Fatal(e)
	}
	if w := applyRevision(h, task.ID, p, `"1"`); w.Code != 409 {
		t.Fatal("stale configuration accepted", w.Code)
	}
}
func TestRevisionIfMatchRequiresOneCanonicalStrongPositiveTag(t *testing.T) {
	_, _, h, task := revisionSetup(t)
	path := "/control/v1/tasks/" + task.ID + "/plan/preview"
	if w := revisionRequest(h, "POST", path, reviewChange); w.Code != 428 {
		t.Fatal("missing precondition", w.Code)
	}
	for _, tags := range [][]string{{`1`}, {`"0"`}, {`"01"`}, {`W/"1"`}, {`*`}, {`"-1"`}, {`"+1"`}, {`"9223372036854775808"`}, {`"1", "2"`}, {`"1"`, `"1"`}} {
		if w := revisionRequest(h, "POST", path, reviewChange, tags...); w.Code != 400 {
			t.Fatal("noncanonical If-Match accepted", tags, w.Code)
		}
	}
	if w := revisionRequest(h, "POST", path, reviewChange, `"2"`); w.Code != 409 {
		t.Fatal("stale base accepted", w.Code)
	}
}
func TestRevisionBodyCannotChangeBudgetStagesOrImportSecrets(t *testing.T) {
	_, st, h, task := revisionSetup(t)
	path := "/control/v1/tasks/" + task.ID + "/plan/preview"
	for _, body := range []string{`{"task":{},"budget":{"max_calls":999}}`, `{"task":{},"required_roles":["testing"]}`, `{"task":{},"api_key":"fixture-secret"}`, `{"task":null}`, `{"task":{},"Task":{}}`} {
		if w := revisionRequest(h, "POST", path, body, `"1"`); w.Code != 400 {
			t.Fatal("invalid revision body accepted", body, w.Code)
		}
	}
	for _, body := range []string{`{"task":{}}`, `{"task":{"roles":{"testing":{"mode":"inherit"}}}}`} {
		if w := revisionRequest(h, "POST", path, body, `"1"`); w.Code != 422 {
			t.Fatal("empty/additional role accepted", w.Code)
		}
	}
	got, _ := st.Task(task.ID)
	events, _ := st.Events(task.ID, 0)
	if got.PlanRevision != 1 || len(events) != 1 {
		t.Fatal("rejected request wrote task")
	}
}
func TestRevisionExpiryRestartAndAuthorizationRequireFreshPreview(t *testing.T) {
	s, st, h, task := revisionSetup(t)
	p := revisionPreview(t, h, task.ID)
	restarted, e := New(st, policy.NewManager("fixture-management", nil, nil))
	if e != nil {
		t.Fatal(e)
	}
	if e = restarted.SetProject("fixture-project", configuration()); e != nil {
		t.Fatal(e)
	}
	if w := applyRevision(restarted.Handler(), task.ID, p, `"1"`); w.Code != 409 {
		t.Fatal("restart adopted old receipt", w.Code)
	}
	s.now = func() time.Time { return p.ExpiresAt }
	if w := applyRevision(h, task.ID, p, `"1"`); w.Code != 409 {
		t.Fatal("expired receipt accepted", w.Code)
	}
	for _, cred := range []string{"", "fixture-stage-secret"} {
		if w := request(h, "GET", "/control/v1/tasks/"+task.ID+"/plan", "", "", cred); w.Code != 401 {
			t.Fatal("unauthorized plan read", w.Code)
		}
	}
}

func TestAppliedRevisionReceiptReturnsItsCommittedSnapshotAfterLaterRevision(t *testing.T) {
	s, st, h, task := revisionSetup(t)
	p := revisionPreview(t, h, task.ID)
	if w := applyRevision(h, task.ID, p, `"1"`); w.Code != 200 {
		t.Fatal(w.Code)
	}
	w := revisionRequest(h, "POST", "/control/v1/tasks/"+task.ID+"/plan/preview", `{"task":{"roles":{"review":{"mode":"inherit"}}}}`, `"2"`)
	var next Preview
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &next) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = applyRevision(h, task.ID, next, `"2"`); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if e := s.SetProject("fixture-project", configuration()); e != nil {
		t.Fatal(e)
	}
	before, _ := st.Events(task.ID, 0)
	w = applyRevision(h, task.ID, p, `"1"`)
	var committed stageplan.Snapshot
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &committed) != nil || committed.Hash != p.Plan.Hash || w.Header().Get("ETag") != `"2"` {
		t.Fatal("replay did not return original committed snapshot", w.Code)
	}
	current, _ := st.Task(task.ID)
	after, _ := st.Events(task.ID, 0)
	if current.PlanRevision != 3 || len(before) != len(after) {
		t.Fatal("receipt replay rewound task")
	}
}

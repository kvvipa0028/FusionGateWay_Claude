package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
	"github.com/yetone/magpie/internal/fusion/workflow"
)

type workflowFixtureReply struct {
	Task     store.Task          `json:"task"`
	Workflow *store.WorkflowView `json:"workflow"`
}

func TestWorkflowManagementRejectsScopeHeadersBodiesAndStaleConditionsWithoutDecision(t *testing.T) {
	f := workflowManagementFixture(t)
	base := "/control/v1/tasks/" + f.task.ID + "/workflow"
	tag := f.tag(t)
	before, e := f.st.Events(f.task.ID, 0)
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		method, path, body, tag string
		status                  int
	}{
		{"GET", base, `{}`, "", 400}, {"PUT", base, `{}`, tag, 405}, {"GET", base + "/design", "", "", 405}, {"GET", base + "/approve", "", "", 405},
		{"POST", base, `{"kind":"change"}`, "", 428}, {"POST", base, `{"kind":"change"}`, `"p1-g1-ready"`, 412}, {"POST", base, `{"kind":"change"}`, `"p1-g0-running"`, 412},
		{"POST", base, `{"kind":"continue"}`, tag, 400}, {"POST", base, `{"kind":"change","role":"implementation"}`, tag, 400}, {"POST", base, `{"kind":"change","kind":"investigate"}`, tag, 400}, {"POST", base, `{"Kind":"change"}`, tag, 400},
		{"POST", base + "/extra", `{}`, tag, 400}, {"POST", base + "/approve/extra", `{}`, tag, 400}, {"POST", base + "?after=1", `{"kind":"change"}`, tag, 400},
		{"POST", base + "/design", `{"run_id":"missing","document":{}}`, tag, 400}, {"POST", base + "/approve", `{"design_hash":"bad","acceptance_hash":"bad"}`, tag, 400},
		{"POST", base + "/approve", `{"design_hash":"` + strings.Repeat("a", 64) + `","acceptance_hash":"` + strings.Repeat("b", 64) + `"}`, tag, 404},
	} {
		t.Run(tc.method+tc.path+tc.body+tc.tag, func(t *testing.T) {
			w := workflowCall(f.h, tc.method, tc.path, tc.body, tc.tag)
			if w.Code != tc.status {
				t.Fatal("unsafe input", w.Code, w.Body.String())
			}
		})
	}
	for _, header := range []string{"If-Match", "Authorization"} {
		r := httptest.NewRequest("POST", base, strings.NewReader(`{"kind":"change"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer fixture-management")
		r.Header.Set("If-Match", tag)
		r.Header.Add(header, r.Header.Get(header))
		w := httptest.NewRecorder()
		f.h.ServeHTTP(w, r)
		want := 400
		if header == "Authorization" {
			want = 401
		}
		if w.Code != want {
			t.Fatal("duplicate header normalized", header, w.Code)
		}
	}
	for _, credential := range []string{"", "fgs_fixture", "fixture-other"} {
		w := executionRequest(f.h, "POST", base, `{"kind":"change"}`, "", tag, credential)
		want := 401
		if strings.HasPrefix(credential, "fgs_") {
			want = 403
		}
		if w.Code != want {
			t.Fatal("non-human credential authorized", w.Code)
		}
	}
	if _, e = f.st.Workflow(f.task.ID); !errors.Is(e, store.ErrNotFound) {
		t.Fatal("rejected metadata persisted", e)
	}
	after, e := f.st.Events(f.task.ID, 0)
	if e != nil || !reflect.DeepEqual(before, after) || f.started.Load() != 0 {
		t.Fatal("rejected request changed events/Runtime", e)
	}
	p, e := f.st.Plan(f.task.ID, 1)
	if e != nil {
		t.Fatal(e)
	}
	foreign, e := f.st.Create("foreign-workflow-task", store.CreateRequest{ProjectID: "unregistered", Goal: "private-foreign-goal", Plan: p})
	if e != nil {
		t.Fatal(e)
	}
	for _, method := range []string{"GET", "POST"} {
		for _, id := range []string{foreign.ID, "missing"} {
			w := workflowCall(f.h, method, "/control/v1/tasks/"+id+"/workflow", func() string {
				if method == "POST" {
					return `{"kind":"change"}`
				}
				return ""
			}(), tag)
			if w.Code != 404 || strings.Contains(w.Body.String(), foreign.Goal) {
				t.Fatal("foreign Task disclosed", w.Code)
			}
		}
	}
	readWorkflowFixture(t, workflowCall(f.h, "POST", base, `{"kind":"change"}`, tag))
	if w := workflowCall(f.h, "POST", base, `{"kind":"bugfix"}`, tag); w.Code != 409 {
		t.Fatal("attached kind overwritten", w.Code)
	}
	f.server.mu.Lock()
	delete(f.server.projects, "fixture-project")
	f.server.mu.Unlock()
	if w := workflowCall(f.h, "GET", base, "", ""); w.Code != 404 || strings.Contains(w.Body.String(), f.task.Goal) {
		t.Fatal("removed registration disclosed", w.Code)
	}
}

func TestWorkflowManagementRevokedOrCancelledBeforeAndDuringWaitCannotDiscloseOrWrite(t *testing.T) {
	for _, operation := range []string{"GET", "POST"} {
		for _, loss := range []string{"cancel", "revoke"} {
			t.Run(operation+loss, func(t *testing.T) {
				f := workflowManagementFixture(t)
				base := "/control/v1/tasks/" + f.task.ID + "/workflow"
				if operation == "GET" {
					readWorkflowFixture(t, workflowCall(f.h, "POST", base, `{"kind":"change"}`, f.tag(t)))
				}
				before, e := f.st.Events(f.task.ID, 0)
				if e != nil {
					t.Fatal(e)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				entered := make(chan struct{})
				wrapped := f.server.auth.Management(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					close(entered)
					f.server.mu.Lock()
					value, e := f.server.workflowLocked(r, []string{f.task.ID, "workflow"}, taskCondition{revision: 1, generation: 0, state: "ready", raw: `"p1-g0-ready"`}, AttachWorkflowRequest{Kind: "change"}, WorkflowDesignRequest{}, ApproveWorkflowRequest{})
					f.server.mu.Unlock()
					if e != nil {
						controlFailure(w, e)
						return
					}
					respond(w, 200, value)
				}))
				r := httptest.NewRequest(operation, base, nil).WithContext(ctx)
				r.Header.Set("Authorization", "Bearer fixture-management")
				f.server.mu.Lock()
				done := make(chan *httptest.ResponseRecorder, 1)
				go func() { w := httptest.NewRecorder(); wrapped.ServeHTTP(w, r); done <- w }()
				<-entered
				if loss == "cancel" {
					cancel()
				} else {
					f.server.auth.RevokeManagement()
				}
				f.server.mu.Unlock()
				w := <-done
				if w.Code != 401 || strings.Contains(w.Body.String(), f.task.Goal) {
					t.Fatal("late authority disclosed/write", w.Code)
				}
				after, e := f.st.Events(f.task.ID, 0)
				if e != nil || !reflect.DeepEqual(before, after) {
					t.Fatal("revoked wait persisted", e)
				}
			})
		}
	}
	f := workflowManagementFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("POST", "/control/v1/tasks/"+f.task.ID+"/workflow", strings.NewReader(`{"kind":"change"}`)).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer fixture-management")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("If-Match", f.tag(t))
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("cancelled context accepted", w.Code)
	}
	if _, e := f.st.Workflow(f.task.ID); !errors.Is(e, store.ErrNotFound) {
		t.Fatal("cancelled request persisted", e)
	}
}

func workflowFrozenFixture(t *testing.T, f *executionFixture) (WorkflowReply, string) {
	t.Helper()
	base := "/control/v1/tasks/" + f.task.ID + "/workflow"
	readWorkflowFixture(t, workflowCall(f.h, "POST", base, `{"kind":"change"}`, f.tag(t)))
	w := executionRequest(f.h, "POST", f.startPath(), `{"role":"design"}`, "workflow-frozen-design", f.tag(t), "fixture-management")
	if w.Code != 202 {
		t.Fatal("design start", w.Code, w.Body.String())
	}
	r := executionReply(t, w)
	close(f.finish)
	waitAPIExecution(t, f, r.Run.ID)
	body, _ := json.Marshal(WorkflowDesignRequest{RunID: r.Run.ID, Document: workflow.DesignDocument{Goal: f.task.Goal, Scope: []string{"src"}, Constraints: []string{"preserve API"}, Interfaces: []string{"typed API"}, Acceptance: []string{"tests pass"}}})
	w = workflowCall(f.h, "POST", base+"/design", string(body), f.tag(t))
	readWorkflowFixture(t, w)
	var typed WorkflowReply
	if json.Unmarshal(w.Body.Bytes(), &typed) != nil {
		t.Fatal("typed workflow reply")
	}
	return typed, string(body)
}

func TestWorkflowManagementHashRunAndNewPlanRemainBoundToCurrentTask(t *testing.T) {
	f := workflowManagementFixture(t)
	base := "/control/v1/tasks/" + f.task.ID + "/workflow"
	frozen, body := workflowFrozenFixture(t, f)
	tag := f.tag(t)
	var design WorkflowDesignRequest
	if json.Unmarshal([]byte(body), &design) != nil {
		t.Fatal("fixture design")
	}
	for _, change := range []func(*WorkflowDesignRequest){func(d *WorkflowDesignRequest) { d.RunID = "other-task-run" }, func(d *WorkflowDesignRequest) { d.Document.Constraints = []string{"replace frozen API"} }, func(d *WorkflowDesignRequest) { d.Document.Goal = "different-goal" }} {
		copy := design
		copy.Document.Constraints = append([]string(nil), design.Document.Constraints...)
		change(&copy)
		raw, _ := json.Marshal(copy)
		if w := workflowCall(f.h, "POST", base+"/design", string(raw), tag); w.Code != 409 {
			t.Fatal("frozen design replacement accepted", w.Code)
		}
	}
	approval := ApproveWorkflowRequest{DesignHash: frozen.Workflow.Design.Snapshot.Hash, AcceptanceHash: frozen.Workflow.Design.Snapshot.AcceptanceHash}
	bad := approval
	bad.AcceptanceHash = strings.Repeat("0", 64)
	raw, _ := json.Marshal(bad)
	if w := workflowCall(f.h, "POST", base+"/approve", string(raw), tag); w.Code != 409 {
		t.Fatal("wrong criteria approved", w.Code)
	}
	raw, _ = json.Marshal(approval)
	approved := readWorkflowFixture(t, workflowCall(f.h, "POST", base+"/approve", string(raw), tag))
	if approved.Workflow.Approval.PlanRevision != 1 {
		t.Fatal("wrong approved revision")
	}
	c := configuration()
	for _, role := range stageplan.AllRoles() {
		c.Global.Roles[role] = c.Global.Roles[stageplan.Design]
	}
	p, e := stageplan.Compile(2, stageplan.AllRoles(), c.Global, stageplan.Layer{}, stageplan.Layer{}, c.Routes)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.st.RevisePlan(f.task.ID, 1, p); e != nil {
		t.Fatal(e)
	}
	if w := workflowCall(f.h, "POST", base+"/approve", string(raw), tag); w.Code != 412 {
		t.Fatal("old Task condition approved revised plan", w.Code)
	}
	v := readWorkflowFixture(t, workflowCall(f.h, "GET", base, "", ""))
	if v.Task.PlanRevision != 2 || v.Workflow.Approval != nil || v.Workflow.Blocker != "approval_required" {
		t.Fatal("old approval granted new plan")
	}
	v = readWorkflowFixture(t, workflowCall(f.h, "POST", base+"/approve", string(raw), f.tag(t)))
	if v.Workflow.Approval.PlanRevision != 2 || v.Workflow.Approval.PlanHash != p.Hash || f.started.Load() != 1 {
		t.Fatal("current approval not explicit/exact")
	}
}

func TestWorkflowManagementNeverAcceptsUnsupportedKeyHeaders(t *testing.T) {
	f := workflowManagementFixture(t)
	base := "/control/v1/tasks/" + f.task.ID + "/workflow"
	for _, key := range []string{"", "unexpected-key"} {
		r := httptest.NewRequest("POST", base, strings.NewReader(`{"kind":"change"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer fixture-management")
		r.Header.Set("If-Match", f.tag(t))
		r.Header.Add("Idempotency-Key", key)
		w := httptest.NewRecorder()
		f.h.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal("unsupported header accepted", w.Code)
		}
	}
	if _, e := f.st.Workflow(f.task.ID); !errors.Is(e, store.ErrNotFound) {
		t.Fatal("unsupported header persisted decision", e)
	}
}

func readWorkflowFixture(t *testing.T, w *httptest.ResponseRecorder) workflowFixtureReply {
	t.Helper()
	var out workflowFixtureReply
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Task.ID == "" || w.Header().Get("ETag") != taskETag(out.Task) {
		t.Fatal("workflow response", w.Code, w.Body.String())
	}
	return out
}
func workflowCall(h http.Handler, method, path, body, tag string) *httptest.ResponseRecorder {
	return executionRequest(h, method, path, body, "", tag, "fixture-management")
}
func workflowManagementFixture(t *testing.T) *executionFixture {
	t.Helper()
	f := setupExecution(t, "")
	c := configuration()
	for _, role := range stageplan.AllRoles() {
		c.Global.Roles[role] = c.Global.Roles[stageplan.Design]
	}
	if e := f.server.SetProject("fixture-project", c); e != nil {
		t.Fatal(e)
	}
	w := request(f.h, "POST", "/control/v1/tasks/preview", `{"project_id":"fixture-project","goal":"fixture-goal","required_roles":["design","implementation","testing","review","acceptance"],"budget":{"max_calls":8,"max_reworks":1}}`, "", "fixture-management")
	var p Preview
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil {
		t.Fatal("workflow preview", w.Code)
	}
	w = submit(t, f.h, p, "fixture-workflow-management")
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &f.task) != nil {
		t.Fatal("workflow task", w.Code)
	}
	return f
}

// Missing routes or approving a model result/continue without matching current
// human hashes would break this real Handler + Store + Controller lifecycle.
func TestWorkflowManagementFreezesDesignRequiresExplicitApprovalAndReplaysWithoutRuntime(t *testing.T) {
	f := workflowManagementFixture(t)
	base := "/control/v1/tasks/" + f.task.ID + "/workflow"
	got := readWorkflowFixture(t, workflowCall(f.h, "GET", base, "", ""))
	if got.Workflow != nil {
		t.Fatal("invented workflow")
	}
	tag := f.tag(t)
	attached := readWorkflowFixture(t, workflowCall(f.h, "POST", base, `{"kind":"change"}`, tag))
	if attached.Workflow == nil || attached.Workflow.Next != "design" || attached.Task != f.task || f.started.Load() != 0 {
		t.Fatal("attach started Runtime or wrong definition")
	}
	if got = readWorkflowFixture(t, workflowCall(f.h, "POST", base, `{"kind":"change"}`, tag)); !reflect.DeepEqual(got, attached) {
		t.Fatal("attach replay changed contract")
	}
	w := executionRequest(f.h, "POST", f.startPath(), `{"role":"implementation"}`, "before-design", tag, "fixture-management")
	if w.Code != 409 || f.started.Load() != 0 {
		t.Fatal("implementation bypassed design", w.Code)
	}
	w = executionRequest(f.h, "POST", f.startPath(), `{"role":"design"}`, "workflow-controller-design", tag, "fixture-management")
	if w.Code != 202 {
		t.Fatal("design start", w.Code, w.Body.String())
	}
	r := executionReply(t, w)
	close(f.finish)
	waitAPIExecution(t, f, r.Run.ID)
	doc := workflow.DesignDocument{Goal: f.task.Goal, Scope: []string{"src"}, Constraints: []string{"preserve public API"}, Interfaces: []string{"typed API"}, Acceptance: []string{"tests pass"}}
	body, _ := json.Marshal(map[string]any{"run_id": r.Run.ID, "document": doc})
	tag = f.tag(t)
	frozen := readWorkflowFixture(t, workflowCall(f.h, "POST", base+"/design", string(body), tag))
	if frozen.Workflow.Design == nil || frozen.Workflow.Approval != nil || frozen.Workflow.Blocker != "approval_required" {
		t.Fatal("freezing design granted approval")
	}
	approval, _ := json.Marshal(map[string]string{"design_hash": frozen.Workflow.Design.Snapshot.Hash, "acceptance_hash": frozen.Workflow.Design.Snapshot.AcceptanceHash})
	w = workflowCall(f.h, "POST", "/control/v1/tasks/"+f.task.ID+"/continue", `{}`, tag)
	if w.Code != 200 {
		t.Fatal("idle continue", w.Code)
	}
	if readWorkflowFixture(t, workflowCall(f.h, "GET", base, "", "")).Workflow.Approval != nil {
		t.Fatal("continue approved design")
	}
	approved := readWorkflowFixture(t, workflowCall(f.h, "POST", base+"/approve", string(approval), tag))
	if approved.Workflow.Approval == nil || approved.Workflow.Blocker != "" || approved.Workflow.Next != "implementation" || f.started.Load() != 1 {
		t.Fatal("explicit approval missing or auto Runtime")
	}
	before, e := f.st.Events(f.task.ID, 0)
	if e != nil {
		t.Fatal(e)
	}
	if got = readWorkflowFixture(t, workflowCall(f.h, "POST", base+"/approve", string(approval), tag)); !reflect.DeepEqual(got, approved) {
		t.Fatal("lost approval response not idempotent")
	}
	if got = readWorkflowFixture(t, workflowCall(f.h, "POST", base+"/design", string(body), tag)); !reflect.DeepEqual(got, approved) {
		t.Fatal("frozen document replay changed approval")
	}
	after, e := f.st.Events(f.task.ID, 0)
	if e != nil || !reflect.DeepEqual(before, after) || f.started.Load() != 1 {
		t.Fatal("read/replay wrote new decision or Runtime", e)
	}
	budget, e := f.st.Budget(f.task.ID)
	if e != nil || budget.UsedCalls != 0 || budget.UsedReworks != 0 {
		t.Fatal("metadata charged budget", e)
	}
	for _, private := range []string{"fixture-management", "fixture-native-session", "lease_owner", "stop_proof_hash", "fixture prompt"} {
		if strings.Contains(workflowCall(f.h, "GET", base, "", "").Body.String(), private) {
			t.Fatal("private workflow output leaked")
		}
	}
}

//go:build darwin || linux

package bootstrap

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/api"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
	"github.com/yetone/magpie/internal/fusion/workflow"
)

func nativeWorkflowReply(t *testing.T, w *httptest.ResponseRecorder) api.WorkflowReply {
	t.Helper()
	var out api.WorkflowReply
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Task.ID == "" || w.Header().Get("ETag") != nativeControlTag(out.Task) {
		t.Fatal("Native workflow response", w.Code, w.Body.String())
	}
	return out
}
func nativeWorkflowHost(t *testing.T) (string, Document, *ControlHost, *NativeStageBridge, store.Task) {
	t.Helper()
	path, doc := sourceFixture(t)
	groups := doc.Projects[0].Layer.Groups
	groups[stageplan.ImplementationTesting] = groups[stageplan.DesignPlanning]
	groups[stageplan.ReviewAcceptance] = groups[stageplan.DesignPlanning]
	writeSource(t, path, doc)
	h, e := OpenExecutionControl(context.Background(), path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0", func(_ context.Context, env RuntimeEnvironment) (RuntimeRegistration, error) {
		return hostRegistration(env), nil
	})
	if e != nil {
		t.Fatal(e)
	}
	serveExecutionHost(t, h)
	b, e := newNativeTestBridge(t, h)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(b.Close)
	w := nativeControlRequest(b, "POST", "/control/v1/tasks/preview", `{"project_id":"fixture-project","goal":"fixture workflow goal","required_roles":["design","implementation","testing","review","acceptance"],"budget":{"max_calls":8,"max_reworks":1}}`, "", "")
	var p api.Preview
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil {
		t.Fatal("Native workflow preview", w.Code)
	}
	body, _ := json.Marshal(api.SubmitRequest{PreviewID: p.ID, PlanHash: p.Plan.Hash})
	w = nativeControlRequest(b, "POST", "/agent/v1/tasks", string(body), "native-workflow-create", "")
	var task store.Task
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &task) != nil {
		t.Fatal("Native workflow task", w.Code)
	}
	return path, doc, h, b, task
}
func nativeFrozenWorkflow(t *testing.T, h *ControlHost, b *NativeStageBridge, task store.Task) api.WorkflowReply {
	t.Helper()
	base := "/control/v1/tasks/" + task.ID + "/workflow"
	nativeWorkflowReply(t, nativeControlRequest(b, "POST", base, `{"kind":"change"}`, "", nativeControlTag(task)))
	p, e := h.store.Plan(task.ID, 1)
	if e != nil {
		t.Fatal(e)
	}
	gen := int64(0)
	// Trusted synthetic Store lifecycle, not an actual Native model run.
	r, e := h.store.StartReservedOnce(store.StartRequest{TaskID: task.ID, Role: stageplan.Design, PlanRevision: 1, ExpectedGeneration: &gen, IdempotencyKey: "native-synthetic-design", Owner: "fixture-owner", TTL: time.Minute, Target: *p.Bindings[stageplan.Design].Target}, store.ReservationRequest{PoolKey: "fixture-pool", GlobalLimit: 2, AdmissionHash: strings.Repeat("a", 64)})
	if e != nil {
		t.Fatal(e)
	}
	if e = h.store.ConfirmStarted(r.Run.ID, r.Run.Generation, r.Run.Owner, "fixture-session"); e != nil {
		t.Fatal(e)
	}
	if e = h.store.Finish(r.Run.ID, r.Run.Generation, r.Run.Owner, "succeeded"); e != nil {
		t.Fatal(e)
	}
	if e = h.store.ReleaseReserved(r.Run.ID, r.Run.Generation, strings.Repeat("b", 64), true); e != nil {
		t.Fatal(e)
	}
	body, _ := json.Marshal(api.WorkflowDesignRequest{RunID: r.Run.ID, Document: workflow.DesignDocument{Goal: task.Goal, Scope: []string{"src"}, Constraints: []string{"preserve API"}, Interfaces: []string{"typed API"}, Acceptance: []string{"tests pass"}}})
	return nativeWorkflowReply(t, nativeControlRequest(b, "POST", base+"/design", string(body), "", `"p1-g1-ready"`))
}

func TestNativeWorkflowDesignApprovalSurvivesHostRestartWithoutLaunchOrCredential(t *testing.T) {
	path, _, h, b, task := nativeWorkflowHost(t)
	base := "/control/v1/tasks/" + task.ID + "/workflow"
	if nativeWorkflowReply(t, nativeControlRequest(b, "GET", base, "", "", "")).Workflow != nil {
		t.Fatal("invented workflow")
	}
	frozen := nativeFrozenWorkflow(t, h, b, task)
	if frozen.Workflow.Approval != nil || frozen.Workflow.Blocker != "approval_required" {
		t.Fatal("design auto-approved")
	}
	body, _ := json.Marshal(api.ApproveWorkflowRequest{DesignHash: frozen.Workflow.Design.Snapshot.Hash, AcceptanceHash: frozen.Workflow.Design.Snapshot.AcceptanceHash})
	approved := nativeWorkflowReply(t, nativeControlRequest(b, "POST", base+"/approve", string(body), "", `"p1-g1-ready"`))
	if approved.Workflow.Approval == nil || approved.Workflow.Next != "implementation" || approved.Task.State != "ready" || approved.Task.Generation != 1 {
		t.Fatal("approval launched stage")
	}
	for _, private := range []string{"management.token", "fixture-session", "lease_owner", "stop_proof_hash"} {
		if strings.Contains(nativeControlRequest(b, "GET", base, "", "", "").Body.String(), private) {
			t.Fatal("Native metadata leaked private field")
		}
	}
	root := h.root
	b.Close()
	if e := h.Close(); e != nil {
		t.Fatal(e)
	}
	h, e := OpenControl(path, root, "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	serveExecutionHost(t, h)
	b, e = newNativeTestBridge(t, h)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	got := nativeWorkflowReply(t, nativeControlRequest(b, "GET", base, "", "", ""))
	if got.Workflow.Approval == nil || got.Workflow.Approval.DesignHash != approved.Workflow.Approval.DesignHash || got.Task != approved.Task {
		t.Fatal("reopen lost exact human approval")
	}
	if nativeWorkflowReply(t, nativeControlRequest(b, "POST", base+"/approve", string(body), "", `"p1-g1-ready"`)).Task != approved.Task {
		t.Fatal("replay reset Task")
	}
}

func TestNativeWorkflowOnlyExactWindowProjectMethodsAndTaskConditions(t *testing.T) {
	_, _, h, b, task := nativeWorkflowHost(t)
	base := "/control/v1/tasks/" + task.ID + "/workflow"
	for _, tc := range []struct{ method, path string }{{"HEAD", base}, {"PUT", base}, {"GET", base + "/approve"}, {"GET", base + "/design"}, {"POST", base + "/continue"}, {"POST", base + "/approve/extra"}, {"GET", "/control/v1/projects/fixture-project/workflow"}} {
		if w := nativeControlRequest(b, tc.method, tc.path, `{}`, "", `"p1-g0-ready"`); w.Code != 404 {
			t.Fatal("generic Native control widened", w.Code, tc.path)
		}
	}
	for _, operation := range []string{"", "/design", "/approve"} {
		r := nativeRequest("POST", base+operation, `{}`)
		r.Header.Set("X-Wails-Window-Id", "2")
		w := httptest.NewRecorder()
		b.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("foreign window authorized workflow", w.Code)
		}
	}
	for _, tc := range []struct {
		tag    string
		status int
	}{{"", 428}, {`"p1-g1-ready"`, 412}} {
		if w := nativeControlRequest(b, "POST", base, `{"kind":"change"}`, "", tc.tag); w.Code != tc.status {
			t.Fatal("Task condition lost", w.Code)
		}
	}
	if w := nativeControlRequest(b, "POST", base, `{"kind":"change"}`, "unexpected-key", `"p1-g0-ready"`); w.Code != 400 {
		t.Fatal("unsupported key silently discarded", w.Code)
	}
	p, e := h.store.Plan(task.ID, 1)
	if e != nil {
		t.Fatal(e)
	}
	foreign, e := h.store.Create("foreign-native-workflow", store.CreateRequest{ProjectID: "foreign", Goal: "private-foreign-goal", Plan: p})
	if e != nil {
		t.Fatal(e)
	}
	if w := nativeControlRequest(b, "GET", "/control/v1/tasks/"+foreign.ID+"/workflow", "", "", ""); w.Code != 404 || strings.Contains(w.Body.String(), foreign.Goal) {
		t.Fatal("foreign task disclosed", w.Code)
	}
	r := nativeRequest("POST", base, `{"kind":"change"}`)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("If-Match", `"p1-g0-ready"`)
	r.Header.Add("If-Match", `"p1-g0-ready"`)
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("duplicate condition normalized", w.Code)
	}
	b.Close()
	if w = nativeControlRequest(b, "GET", base, "", "", ""); w.Code != 503 {
		t.Fatal("closed window disclosed", w.Code)
	}
}

func TestNativeWorkflowLateSourceLossSuppressesCommittedApprovalReceipt(t *testing.T) {
	path, doc, h, b, task := nativeWorkflowHost(t)
	base := "/control/v1/tasks/" + task.ID + "/workflow"
	frozen := nativeFrozenWorkflow(t, h, b, task)
	body, _ := json.Marshal(api.ApproveWorkflowRequest{DesignHash: frozen.Workflow.Design.Snapshot.Hash, AcceptanceHash: frozen.Workflow.Design.Snapshot.AcceptanceHash})
	b.client.Transport = journalTrip{base: b.transport, after: func() { doc.Revision++; writeSource(t, path, doc) }}
	w := nativeControlRequest(b, "POST", base+"/approve", string(body), "", `"p1-g1-ready"`)
	if w.Code != 503 || strings.Contains(w.Body.String(), task.Goal) || strings.Contains(w.Body.String(), frozen.Workflow.Design.Snapshot.Hash) {
		t.Fatal("late source disclosed approval", w.Code)
	}
	v, e := h.store.Workflow(task.ID)
	if e != nil || v.Approval == nil {
		t.Fatal("committed decision hidden but discarded", e)
	}
	current, e := h.store.Task(task.ID)
	if e != nil || current.State != "ready" || current.Generation != 1 {
		t.Fatal("approval launched new stage", e)
	}
}

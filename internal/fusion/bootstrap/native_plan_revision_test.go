//go:build darwin || linux

package bootstrap

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/api"
	"github.com/yetone/magpie/internal/fusion/control"
	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

const nativeReviewRevision = `{"task":{"roles":{"review":{"mode":"locked","route":{"id":"fixture-glm-other","revision":1},"model":"fixture-model-other","effort":{"mode":"none"}}}}}`

func nativeRevisionHost(t *testing.T) (string, Document, *ControlHost, *NativeStageBridge, store.Task, *atomic.Int64) {
	t.Helper()
	path, doc := sourceFixture(t)
	other := doc.Routes[0]
	other.ID, other.Model = "fixture-glm-other", "fixture-model-other"
	doc.Routes = append(doc.Routes, other)
	doc.Projects[0].Routes = append(doc.Projects[0].Routes, stageplan.RouteRef{ID: other.ID, Revision: 1})
	groups := doc.Projects[0].Layer.Groups
	groups[stageplan.ImplementationTesting] = groups[stageplan.DesignPlanning]
	groups[stageplan.ReviewAcceptance] = groups[stageplan.DesignPlanning]
	writeSource(t, path, doc)
	var calls atomic.Int64
	h, err := OpenExecutionControl(context.Background(), path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0", func(_ context.Context, env RuntimeEnvironment) (RuntimeRegistration, error) {
		project, err := env.Source.Project("fixture-project")
		if err != nil {
			return RuntimeRegistration{}, err
		}
		routes := project.Configuration.Routes
		for i := range routes {
			routes[i].Admitted, routes[i].BillingKnown = true, true
			routes[i].LockEnforcement = stageplan.ControlledCalls
			routes[i].Capabilities = []string{"text"}
		}
		return RuntimeRegistration{Routes: map[string][]stageplan.Route{"fixture-project": routes}, Inspect: func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (policy.Inspection, error) {
			calls.Add(1)
			return policy.Inspection{}, control.ErrUnsupported
		}, Resolve: func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (control.Launch, error) {
			calls.Add(1)
			return control.Launch{}, control.ErrUnsupported
		}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	serveExecutionHost(t, h)
	b, err := newNativeTestBridge(t, h)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	w := nativeControlRequest(b, "POST", "/control/v1/tasks/preview", `{"project_id":"fixture-project","goal":"native revision fixture","required_roles":["design","implementation","testing","review","acceptance"],"budget":{"max_calls":8,"max_reworks":1}}`, "", "")
	var preview api.Preview
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &preview) != nil {
		t.Fatal("create preview", w.Code, w.Body.String())
	}
	raw, _ := json.Marshal(api.SubmitRequest{PreviewID: preview.ID, PlanHash: preview.Plan.Hash})
	w = nativeControlRequest(b, "POST", "/agent/v1/tasks", string(raw), "native-revision-create", "")
	var task store.Task
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &task) != nil {
		t.Fatal("create task", w.Code, w.Body.String())
	}
	return path, doc, h, b, task, &calls
}

func nativeRevisionPreview(t *testing.T, b *NativeStageBridge, taskID, body, tag string) api.Preview {
	t.Helper()
	w := nativeControlRequest(b, "POST", "/control/v1/tasks/"+taskID+"/plan/preview", body, "", tag)
	var p api.Preview
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil || w.Header().Get("ETag") != tag || stageplan.VerifySnapshot(p.Plan) != nil {
		t.Fatal("Native revision preview", w.Code, w.Body.String())
	}
	return p
}

func nativeRevisionApply(b *NativeStageBridge, taskID string, p api.Preview, tag string) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(api.SubmitRequest{PreviewID: p.ID, PlanHash: p.Plan.Hash})
	return nativeControlRequest(b, "PUT", "/control/v1/tasks/"+taskID+"/plan", string(raw), "", tag)
}

// This creates only synthetic Store state, not a Native execution/StopProof.
func nativeRevisionStartedRole(t *testing.T, h *ControlHost, task store.Task, role stageplan.Role) store.StageRun {
	t.Helper()
	plan, err := h.store.Plan(task.ID, task.PlanRevision)
	if err != nil {
		t.Fatal(err)
	}
	run, err := h.store.StartReserved(store.StartRequest{TaskID: task.ID, Role: role, PlanRevision: task.PlanRevision, Owner: "revision-fixture-owner", TTL: time.Minute, Target: *plan.Bindings[role].Target}, store.ReservationRequest{PoolKey: "revision-fixture-pool", GlobalLimit: 2, AdmissionHash: plan.Hash})
	if err != nil {
		t.Fatal(err)
	}
	if err = h.store.ConfirmStarted(run.ID, run.Generation, run.Owner, "revision-fixture-session"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := h.store.Finish(run.ID, run.Generation, run.Owner, "succeeded"); err != nil {
			t.Error(err)
		}
		if err := h.store.ReleaseReserved(run.ID, run.Generation, strings.Repeat("a", 64), true); err != nil {
			t.Error(err)
		}
	})
	return run
}

func TestNativePlanRevisionPreviewApplyPreservesStartedRunBudgetAndHistory(t *testing.T) {
	_, _, h, b, task, calls := nativeRevisionHost(t)
	run := nativeRevisionStartedRole(t, h, task, stageplan.Design)
	if err := h.store.ReserveCall(run.ID, run.Generation); err != nil {
		t.Fatal(err)
	}
	if err := h.store.ReserveRework(task.ID, run.Generation); err != nil {
		t.Fatal(err)
	}
	old, _ := h.store.Plan(task.ID, 1)
	beforeRun, _ := h.store.Run(run.ID)
	beforeTask, _ := h.store.Task(task.ID)
	budget, _ := h.store.Budget(task.ID)
	before, _ := h.store.Events(task.ID, 0)
	p := nativeRevisionPreview(t, b, task.ID, nativeReviewRevision, `"1"`)
	after, _ := h.store.Events(task.ID, 0)
	if !reflect.DeepEqual(before, after) || p.Plan.Revision != 2 || p.Plan.Bindings[stageplan.Review].Target.ResolvedModel != "fixture-model-other" || p.Budget.MaxCalls != budget.MaxCalls || p.Budget.MaxReworks != budget.MaxReworks {
		t.Fatal("preview wrote state or lost frozen limits")
	}
	w := nativeRevisionApply(b, task.ID, p, `"1"`)
	var applied stageplan.Snapshot
	if w.Code != 200 || w.Header().Get("ETag") != `"2"` || json.Unmarshal(w.Body.Bytes(), &applied) != nil || !reflect.DeepEqual(applied, p.Plan) {
		t.Fatal("apply lost exact preview", w.Code, w.Body.String())
	}
	for _, role := range old.RequiredRoles {
		if role != stageplan.Review && !reflect.DeepEqual(applied.Bindings[role], old.Bindings[role]) {
			t.Fatal("changed unrelated frozen role", role)
		}
	}
	afterRun, _ := h.store.Run(run.ID)
	afterTask, _ := h.store.Task(task.ID)
	afterBudget, _ := h.store.Budget(task.ID)
	historical, _ := h.store.Plan(task.ID, 1)
	if !reflect.DeepEqual(beforeRun, afterRun) || beforeTask.Generation != afterTask.Generation || beforeTask.State != afterTask.State || afterTask.PlanRevision != 2 || budget != afterBudget || !reflect.DeepEqual(historical, old) || calls.Load() != 0 {
		t.Fatal("revision rewrote active execution/history/counters or launched runtime")
	}
	second := nativeRevisionPreview(t, b, task.ID, strings.Replace(nativeReviewRevision, `"review"`, `"acceptance"`, 1), `"2"`)
	if w := nativeRevisionApply(b, task.ID, second, `"2"`); w.Code != 200 {
		t.Fatal("second revision", w.Code)
	}
	before, _ = h.store.Events(task.ID, 0)
	w = nativeRevisionApply(b, task.ID, p, `"1"`)
	after, _ = h.store.Events(task.ID, 0)
	if w.Code != 200 || w.Header().Get("ETag") != `"2"` || json.Unmarshal(w.Body.Bytes(), &applied) != nil || !reflect.DeepEqual(applied, p.Plan) || !reflect.DeepEqual(before, after) {
		t.Fatal("exact old apply replay changed current revision or event")
	}
	if w := nativeControlRequest(b, "POST", "/control/v1/tasks/"+task.ID+"/plan/preview", strings.Replace(nativeReviewRevision, `"review"`, `"design"`, 1), "", `"3"`); w.Code != 409 {
		t.Fatal("started role became mutable", w.Code)
	}
}

func TestNativePlanRevisionRejectsForeignWindowProjectPathsHeadersAndBodies(t *testing.T) {
	_, _, h, b, task, calls := nativeRevisionHost(t)
	base := "/control/v1/tasks/" + task.ID + "/plan"
	for _, tc := range []struct{ method, path string }{
		{"HEAD", base}, {"POST", base}, {"DELETE", base}, {"GET", base + "/preview"}, {"PUT", base + "/preview"},
		{"POST", base + "/preview/extra"}, {"PUT", base + "/versions/2"}, {"POST", "/control/v1/projects/fixture-project/plan/preview"},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			if w := nativeControlRequest(b, tc.method, tc.path, `{}`, "", `"1"`); w.Code != 404 {
				t.Fatal("generic Native authority widened", w.Code)
			}
		})
	}
	for _, method := range []string{"GET", "POST", "PUT"} {
		path := base
		if method == "POST" {
			path += "/preview"
		}
		t.Run(method+" window", func(t *testing.T) {
			r := nativeRequest(method, path, `{}`)
			r.Header.Set("X-Wails-Window-Id", "2")
			w := httptest.NewRecorder()
			b.ServeHTTP(w, r)
			if w.Code != 403 {
				t.Fatal("foreign window authorized plan", w.Code)
			}
		})
		for _, key := range []string{"unsupported-key", ""} {
			t.Run(method+" unsupported key "+key, func(t *testing.T) {
				r := nativeRequest(method, path, `{}`)
				r.Header.Set("Idempotency-Key", key)
				w := httptest.NewRecorder()
				b.ServeHTTP(w, r)
				if w.Code != 400 {
					t.Fatal("unsupported key discarded", w.Code)
				}
			})
		}
	}
	for _, tc := range []struct {
		tag  string
		want int
	}{{"", 428}, {`"01"`, 400}, {`W/"1"`, 400}, {"*", 400}, {`"0"`, 400}, {`"9223372036854775808"`, 400}, {`"p1-g0-ready"`, 400}, {`"2"`, 409}} {
		t.Run("If-Match "+tc.tag, func(t *testing.T) {
			if w := nativeControlRequest(b, "POST", base+"/preview", nativeReviewRevision, "", tc.tag); w.Code != tc.want {
				t.Fatal("plan precondition lost", w.Code, tc.want)
			}
		})
	}
	r := nativeRequest("POST", base+"/preview", nativeReviewRevision)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Add("If-Match", `"1"`)
	r.Header.Add("If-Match", `"1"`)
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("duplicate plan condition normalized", w.Code)
	}
	before, _ := h.store.Events(task.ID, 0)
	for _, body := range []string{`{"task":{},"api_key":"fixture-forbidden"}`, `{"task":{},"budget":{"max_calls":100}}`, `{"task":{},"required_roles":["design"]}`, `{"task":{},"independence":[]}`, `{"Task":{}}`, `{"task":{},"task":{}}`, `{"task":{}} {}`} {
		if w := nativeControlRequest(b, "POST", base+"/preview", body, "", `"1"`); w.Code != 400 {
			t.Fatal("invalid revision body admitted", w.Code, body)
		}
	}
	// An empty Layer is valid JSON/DTO but fails semantic compilation (422).
	if w := nativeControlRequest(b, "POST", base+"/preview", `{"task":{}}`, "", `"1"`); w.Code != 422 {
		t.Fatal("empty revision compiled", w.Code)
	}
	plan, _ := h.store.Plan(task.ID, 1)
	foreign, err := h.store.Create("native-revision-foreign", store.CreateRequest{ProjectID: "unregistered", Goal: "private-foreign-goal", Plan: plan})
	if err != nil {
		t.Fatal(err)
	}
	var forwarded atomic.Int64
	b.client.Transport = journalTrip{base: b.transport, after: func() { forwarded.Add(1) }}
	for _, id := range []string{foreign.ID, "missing"} {
		for _, method := range []string{"GET", "POST", "PUT"} {
			path := "/control/v1/tasks/" + id + "/plan"
			if method == "POST" {
				path += "/preview"
			}
			if w := nativeControlRequest(b, method, path, `{}`, "", `"1"`); w.Code != 404 || strings.Contains(w.Body.String(), foreign.Goal) || strings.Contains(w.Body.String(), plan.Hash) {
				t.Fatal("foreign/unknown task reached plan handler", method, w.Code)
			}
		}
	}
	after, _ := h.store.Events(task.ID, 0)
	if forwarded.Load() != 0 || calls.Load() != 0 || !reflect.DeepEqual(before, after) {
		t.Fatal("rejected plan request executed or changed Task")
	}
}

func TestNativePlanRevisionRacingStartAndConcurrentApplyRequireFreshPreview(t *testing.T) {
	t.Run("started after preview", func(t *testing.T) {
		_, _, h, b, task, _ := nativeRevisionHost(t)
		p := nativeRevisionPreview(t, b, task.ID, nativeReviewRevision, `"1"`)
		nativeRevisionStartedRole(t, h, task, stageplan.Review)
		before, _ := h.store.Events(task.ID, 0)
		if w := nativeRevisionApply(b, task.ID, p, `"1"`); w.Code != 409 {
			t.Fatal("late started role overwritten", w.Code)
		}
		after, _ := h.store.Events(task.ID, 0)
		current, _ := h.store.Task(task.ID)
		if !reflect.DeepEqual(before, after) || current.PlanRevision != 1 {
			t.Fatal("failed revision changed history")
		}
	})
	t.Run("concurrent apply and cross-task receipt", func(t *testing.T) {
		_, _, h, b, task, _ := nativeRevisionHost(t)
		a := nativeRevisionPreview(t, b, task.ID, nativeReviewRevision, `"1"`)
		c := nativeRevisionPreview(t, b, task.ID, nativeReviewRevision, `"1"`)
		plan, _ := h.store.Plan(task.ID, 1)
		other, err := h.store.Create("native-revision-other", store.CreateRequest{ProjectID: task.ProjectID, Goal: "other revision task", Plan: plan})
		if err != nil {
			t.Fatal(err)
		}
		if w := nativeRevisionApply(b, other.ID, a, `"1"`); w.Code != 409 {
			t.Fatal("receipt used for another task", w.Code)
		}
		var wg sync.WaitGroup
		codes := make(chan int, 2)
		for _, p := range []api.Preview{a, c} {
			wg.Add(1)
			go func(p api.Preview) { defer wg.Done(); codes <- nativeRevisionApply(b, task.ID, p, `"1"`).Code }(p)
		}
		wg.Wait()
		close(codes)
		counts := map[int]int{}
		for code := range codes {
			counts[code]++
		}
		if counts[200] != 1 || counts[409] != 1 {
			t.Fatal("concurrent revision overwrote another", counts)
		}
		events, _ := h.store.Events(task.ID, 0)
		revised := 0
		for _, event := range events {
			if event.Kind == "plan_revised" {
				revised++
			}
		}
		if revised != 1 {
			t.Fatal("concurrent revision event count", revised)
		}
	})
}

func TestNativePlanRevisionLateSourceLossSuppressesReplyAndKeepsCommittedSnapshot(t *testing.T) {
	for _, operation := range []string{"preview", "apply"} {
		t.Run(operation, func(t *testing.T) {
			path, doc, h, b, task, calls := nativeRevisionHost(t)
			p := nativeRevisionPreview(t, b, task.ID, nativeReviewRevision, `"1"`)
			b.client.Transport = journalTrip{base: b.transport, after: func() { doc.Revision++; writeSource(t, path, doc) }}
			var w *httptest.ResponseRecorder
			if operation == "apply" {
				w = nativeRevisionApply(b, task.ID, p, `"1"`)
			} else {
				w = nativeControlRequest(b, "POST", "/control/v1/tasks/"+task.ID+"/plan/preview", nativeReviewRevision, "", `"1"`)
			}
			if w.Code != 503 || strings.Contains(w.Body.String(), task.Goal) || strings.Contains(w.Body.String(), p.Plan.Hash) {
				t.Fatal("late revoked host disclosed receipt", w.Code)
			}
			current, err := h.store.Task(task.ID)
			if err != nil || current.Generation != 0 || current.State != "ready" || calls.Load() != 0 {
				t.Fatal("revision launched/reset Task", err)
			}
			want := int64(1)
			if operation == "apply" {
				want = 2
			}
			if current.PlanRevision != want {
				t.Fatal("lost response lost committed revision", current.PlanRevision)
			}
			stored, err := h.store.Plan(task.ID, want)
			if err != nil || operation == "apply" && !reflect.DeepEqual(stored, p.Plan) {
				t.Fatal("committed snapshot differs from preview", err)
			}
		})
	}
}

func TestNativePlanRevisionReopensImmutableCurrentAndHistoricalPlansWithoutExecution(t *testing.T) {
	path, _, h, b, task, calls := nativeRevisionHost(t)
	old, _ := h.store.Plan(task.ID, 1)
	budget, _ := h.store.Budget(task.ID)
	p := nativeRevisionPreview(t, b, task.ID, nativeReviewRevision, `"1"`)
	if w := nativeRevisionApply(b, task.ID, p, `"1"`); w.Code != 200 {
		t.Fatal("commit revision", w.Code)
	}
	root := h.root
	b.Close()
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	h, err := OpenControl(path, root, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serveExecutionHost(t, h)
	b, err = newNativeTestBridge(t, h)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	w := nativeControlRequest(b, "GET", "/control/v1/tasks/"+task.ID+"/plan", "", "", "")
	var current stageplan.Snapshot
	if w.Code != 200 || w.Header().Get("ETag") != `"2"` || json.Unmarshal(w.Body.Bytes(), &current) != nil || !reflect.DeepEqual(current, p.Plan) {
		t.Fatal("reopen lost committed snapshot", w.Code)
	}
	historical, err := h.store.Plan(task.ID, 1)
	if err != nil || !reflect.DeepEqual(historical, old) {
		t.Fatal("reopen rewrote history", err)
	}
	afterBudget, _ := h.store.Budget(task.ID)
	before, _ := h.store.Events(task.ID, 0)
	// Preview receipts are intentionally in-process; a restart cannot replay it.
	w = nativeRevisionApply(b, task.ID, p, `"1"`)
	after, _ := h.store.Events(task.ID, 0)
	if w.Code != 409 || budget != afterBudget || calls.Load() != 0 || !reflect.DeepEqual(before, after) {
		t.Fatal("reopen granted execution/replay or reset counters", w.Code)
	}
}

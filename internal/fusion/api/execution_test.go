package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/control"
	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/quota"
	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

type apiExecution struct {
	done, end chan struct{}
	once      sync.Once
	result    managed.Result
}

func (f *apiExecution) Cancel() error { f.once.Do(func() { close(f.end) }); return nil }
func (f *apiExecution) Wait(ctx context.Context) (managed.Result, error) {
	select {
	case <-ctx.Done():
		return managed.Result{}, ctx.Err()
	case <-f.done:
		return f.result, nil
	}
}

type executionFixture struct {
	server  *Server
	st      *store.Store
	h       http.Handler
	task    store.Task
	c       *control.Controller
	config  control.Config
	started atomic.Int64
	finish  chan struct{}
	mode    string
}

func executionPrivate(t *testing.T) string {
	t.Helper()
	p, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(p, 0700); e != nil {
		t.Fatal(e)
	}
	return p
}
func setupExecution(t *testing.T, mode string) *executionFixture {
	t.Helper()
	var s *Server
	var st *store.Store
	var h http.Handler
	var task store.Task
	if mode == "revision" {
		s, st, h, task = revisionSetup(t)
	} else {
		s, st, h = setup(t)
		p := preview(t, h)
		w := submit(t, h, p, "fixture-execution-submit")
		if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &task) != nil {
			t.Fatal("task setup failed")
		}
	}
	f := &executionFixture{server: s, st: st, h: h, task: task, finish: make(chan struct{}), mode: mode}
	route := configuration().Routes[0]
	now := time.Now()
	used := float64(10)
	inspection := policy.Inspection{Route: route, Provider: "fixture-provider", QueryAdmitted: true, DataAllowed: true, SandboxVerified: true, VerificationAvailable: true, AllCallsCounted: true, ManagedExecutions: 1, ProofHash: strings.Repeat("a", 64), Quota: quota.Snapshot{Identity: quota.Identity{Provider: "fixture-provider", Account: route.Account, Workspace: route.Workspace, Region: "fixture-region", Generation: 1}, Source: "fixture-source", ObservedAt: &now, ReceivedAt: now, Complete: true, Status: quota.Available, Pool: quota.Pool{ID: "fixture-pool", Provider: "fixture-provider", Region: "fixture-region", Scope: "account", Owner: route.Account, Verified: true}, Windows: []quota.Window{{Kind: "subscription", Unit: "percent", UsedPercent: &used}}}}
	root, workspace := executionPrivate(t), executionPrivate(t)
	f.config.Scheduler = policy.Scheduler{Store: st, Inspect: func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (policy.Inspection, error) {
		if mode == "revoke_inspect" {
			s.auth.RevokeManagement()
		}
		return inspection, nil
	}}
	f.config.Resolve = func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (control.Launch, error) {
		if mode == "revoke_resolve" {
			s.auth.RevokeManagement()
		}
		launch := control.Launch{Spec: managed.Spec{Root: root, Workspace: workspace, Timeout: time.Second, Input: []byte("fixture prompt")}, Backend: control.Backend{Probe: func(context.Context) (managed.Capabilities, error) {
			return managed.Capabilities{Probe: true, Start: true, Events: true, Cancel: true}, nil
		}, Start: func(ctx context.Context, r store.StageRun, _ managed.Spec) (control.Execution, error) {
			f.started.Add(1)
			if mode == "no_handle" {
				return nil, errors.New("fixture-sensitive-launch-error")
			}
			if e := st.ConfirmStarted(r.ID, r.Generation, r.Owner, "fixture-native-session"); e != nil {
				return nil, e
			}
			h := &apiExecution{done: make(chan struct{}), end: make(chan struct{})}
			go func() {
				state := "succeeded"
				select {
				case <-f.finish:
				case <-ctx.Done():
					state = "cancelled"
				case <-h.end:
					state = "cancelled"
				}
				if e := st.Finish(r.ID, r.Generation, r.Owner, state); e != nil {
					t.Error(e)
				}
				h.result = managed.Result{State: state, StoppedVerified: mode != "no_stop", Proof: policy.StopProof{RunID: r.ID, Generation: r.Generation, NativeSessionID: "fixture-native-session", ProcessIdentityHash: strings.Repeat("b", 64), ReportHash: strings.Repeat("c", 64), DescendantsStopped: true}}
				close(h.done)
			}()
			return h, nil
		}, Release: func(p policy.StopProof) error { return st.ReleaseReserved(p.RunID, p.Generation, p.ReportHash, true) }}}
		if strings.HasPrefix(mode, "checkpoint") {
			launch.Backend.Checkpoint = func(context.Context, store.StageRun) (control.CheckpointRef, error) {
				if mode == "checkpoint_revoke" {
					s.auth.RevokeManagement()
				}
				if mode == "checkpoint_error" {
					return control.CheckpointRef{}, errors.New("fixture-sensitive-checkpoint")
				}
				return control.CheckpointRef{ID: strings.Repeat("b", 64), Digest: strings.Repeat("c", 64)}, nil
			}
		}
		if strings.HasPrefix(mode, "restore") {
			launch.Backend.CheckRestore = func(context.Context, store.StageRun, stageplan.ExecutionTarget, managed.Spec, store.RestoreIdentity) error {
				if mode == "restore_bad" {
					return control.ErrIdentity
				}
				if mode == "restore_revoke" {
					s.auth.RevokeManagement()
				}
				return nil
			}
			start := launch.Backend.Start
			launch.Backend.Restore = func(ctx context.Context, r store.StageRun, spec managed.Spec, ref store.RestoreIdentity) (control.Execution, error) {
				if mode == "restore_no_handle" {
					f.started.Add(1)
					return nil, errors.New("fixture-sensitive-restore-error")
				}
				return start(ctx, r, spec)
			}
		}
		return launch, nil
	}
	c, e := control.New(context.Background(), f.config)
	if e != nil {
		t.Fatal(e)
	}
	f.c = c
	if e = s.SetController(c); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := c.Close(ctx); e != nil {
			t.Error(e)
		}
	})
	return f
}
func executionRequest(h http.Handler, method, path, body, key, tag, credential string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")
	if credential != "" {
		r.Header.Set("Authorization", "Bearer "+credential)
	}
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	if tag != "" {
		r.Header.Set("If-Match", tag)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func (f *executionFixture) startPath() string { return "/control/v1/tasks/" + f.task.ID + "/start" }
func (f *executionFixture) tag(t *testing.T) string {
	t.Helper()
	w := request(f.h, "GET", "/agent/v1/tasks/"+f.task.ID, "", "", "fixture-management")
	if w.Code != 200 || w.Header().Get("ETag") == "" {
		t.Fatal("task ETag unavailable", w.Code)
	}
	return w.Header().Get("ETag")
}
func executionReply(t *testing.T, w *httptest.ResponseRecorder) ExecutionReply {
	t.Helper()
	var reply ExecutionReply
	if json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.Run.ID == "" {
		t.Fatal("run response missing", w.Code, w.Body.String())
	}
	return reply
}
func waitAPIExecution(t *testing.T, f *executionFixture, id string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, e := f.c.Wait(ctx, id); e != nil {
		t.Fatal(e)
	}
}
func TestAuthenticatedExecutionStartRetryCancelAndReadContract(t *testing.T) {
	f := setupExecution(t, "")
	tag := f.tag(t)
	first := executionRequest(f.h, "POST", f.startPath(), `{"role":"design"}`, "fixture-start", tag, "fixture-management")
	if first.Code != 202 {
		t.Fatal(first.Code, first.Body.String())
	}
	reply := executionReply(t, first)
	if !reply.Created || first.Header().Get("Location") != "/agent/v1/tasks/"+f.task.ID+"/runs/"+reply.Run.ID {
		t.Fatal("run location/created missing")
	}
	for _, forbidden := range []string{`"owner"`, `"lease_until"`, `"native_session_id"`, "fixture-management", "fixture prompt"} {
		if strings.Contains(first.Body.String(), forbidden) {
			t.Fatal("private lifecycle data in response", forbidden)
		}
	}
	retry := executionRequest(f.h, "POST", f.startPath(), `{"role":"design"}`, "fixture-start", tag, "fixture-management")
	if retry.Code != 200 || executionReply(t, retry).Created || f.started.Load() != 1 {
		t.Fatal("HTTP retry replayed", retry.Code)
	}
	current := f.tag(t)
	cancelled := executionRequest(f.h, "POST", "/control/v1/tasks/"+f.task.ID+"/runs/"+reply.Run.ID+"/cancel", `{}`, "", current, "fixture-management")
	if cancelled.Code != 202 {
		t.Fatal(cancelled.Code, cancelled.Body.String())
	}
	waitAPIExecution(t, f, reply.Run.ID)
	repeat := executionRequest(f.h, "POST", "/control/v1/tasks/"+f.task.ID+"/runs/"+reply.Run.ID+"/cancel", `{}`, "", current, "fixture-management")
	if repeat.Code != 200 {
		t.Fatal("terminal cancel not idempotent", repeat.Code)
	}
	get := request(f.h, "GET", first.Header().Get("Location"), "", "", "fixture-management")
	if get.Code != 200 || executionReply(t, get).Run.State != "cancelled" {
		t.Fatal("run read failed", get.Code)
	}
}
func TestExecutionPreconditionsAreStrongAndCheckedBeforeLaunch(t *testing.T) {
	f := setupExecution(t, "")
	valid := f.tag(t)
	for _, v := range []struct {
		tag  string
		code int
	}{{"", 428}, {"*", 400}, {"W/" + valid, 400}, {"1", 400}, {valid + ", " + valid, 400}, {`"p01-g0-ready"`, 400}, {`"p1-g-1-ready"`, 400}, {`"p1-g9-ready"`, 412}, {`"p1-g0-running"`, 412}} {
		w := executionRequest(f.h, "POST", f.startPath(), `{"role":"design"}`, "fixture-start", v.tag, "fixture-management")
		if w.Code != v.code {
			t.Fatal("bad precondition admitted", v.tag, w.Code, w.Body.String())
		}
	}
	if f.started.Load() != 0 {
		t.Fatal("invalid preconditions launched")
	}
	task, e := f.st.Task(f.task.ID)
	if e != nil || task.Generation != 0 {
		t.Fatal("bad precondition consumed generation")
	}
}
func TestExecutionBodyCannotSelectWorkspaceCredentialsOrRuntime(t *testing.T) {
	f := setupExecution(t, "")
	tag := f.tag(t)
	for _, body := range []string{`{}`, `null`, `{"role":"unknown"}`, `{"Role":"design"}`, `{"role":"design","role":"review"}`, `{"role":"design","token":"fixture-secret"}`, `{"role":"design","workspace":"/tmp/project"}`, `{"role":"design","model":"fixture-other"}`, `{"role":"design","effort":"high"}`, `{"role":"design","owner":"caller"}`, `{"role":"design","attempt":1}`, `{"role":"design","generation":0}`, `{"role":"design","account":"caller"}`, `{"role":"design","args":["--bypass"]}`, `{"role":"design","endpoint":"http://localhost"}`} {
		w := executionRequest(f.h, "POST", f.startPath(), body, "fixture-start", tag, "fixture-management")
		if w.Code != 400 {
			t.Fatal("unsafe body accepted", w.Code, body)
		}
	}
	if f.started.Load() != 0 {
		t.Fatal("unsafe body launched")
	}
}
func TestExecutionRequiresManagementAndRejectsDuplicateHeaders(t *testing.T) {
	f := setupExecution(t, "")
	tag := f.tag(t)
	for _, credential := range []string{"", "fixture-wrong", "fgs_fixture-stage-secret"} {
		w := executionRequest(f.h, "POST", f.startPath(), `{"role":"design"}`, "fixture-start", tag, credential)
		want := 401
		// WP-06 distinguishes forbidden stage scope from absent/invalid
		// management credentials; do not change that shared auth contract.
		if strings.HasPrefix(credential, "fgs_") {
			want = 403
		}
		if w.Code != want {
			t.Fatal("unauthenticated control admitted", w.Code)
		}
	}
	for _, header := range []string{"If-Match", "Idempotency-Key"} {
		r := httptest.NewRequest("POST", f.startPath(), strings.NewReader(`{"role":"design"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer fixture-management")
		r.Header.Set("If-Match", tag)
		r.Header.Set("Idempotency-Key", "fixture-start")
		r.Header.Add(header, r.Header.Get(header))
		w := httptest.NewRecorder()
		f.h.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal("duplicate header admitted", header, w.Code)
		}
	}
	if f.started.Load() != 0 {
		t.Fatal("unauthorized launch")
	}
}
func TestExecutionRevokedManagementDuringPreflightDoesNotCommit(t *testing.T) {
	for _, mode := range []string{"revoke_resolve", "revoke_inspect"} {
		t.Run(mode, func(t *testing.T) {
			f := setupExecution(t, mode)
			tag := f.tag(t)
			w := executionRequest(f.h, "POST", f.startPath(), `{"role":"design"}`, "fixture-start", tag, "fixture-management")
			if w.Code != 401 {
				t.Fatal("revoked management committed", w.Code, w.Body.String())
			}
			task, e := f.st.Task(f.task.ID)
			if e != nil || task.State != "ready" || task.Generation != 0 || f.started.Load() != 0 {
				t.Fatal("revocation left intent", e)
			}
		})
	}
}
func TestExecutionTaskETagChangesOnCompletionWithoutAnotherGeneration(t *testing.T) {
	f := setupExecution(t, "")
	original := f.tag(t)
	w := executionRequest(f.h, "POST", f.startPath(), `{"role":"design"}`, "fixture-start", original, "fixture-management")
	reply := executionReply(t, w)
	running := f.tag(t)
	close(f.finish)
	waitAPIExecution(t, f, reply.Run.ID)
	ready := f.tag(t)
	if running == ready || running == original {
		t.Fatal("strong Task ETag did not track state")
	}
	task, e := f.st.Task(f.task.ID)
	if e != nil || task.Generation != 1 {
		t.Fatal("fixture did not preserve generation")
	}
	stale := executionRequest(f.h, "POST", f.startPath(), `{"role":"design"}`, "fixture-next-start", running, "fixture-management")
	if stale.Code != 412 {
		t.Fatal("stale state token accepted", stale.Code)
	}
	retry := executionRequest(f.h, "POST", f.startPath(), `{"role":"design"}`, "fixture-start", original, "fixture-management")
	if retry.Code != 200 || executionReply(t, retry).Created || f.started.Load() != 1 {
		t.Fatal("terminal start retry launched", retry.Code)
	}
}
func TestExecutionConflictAndPartialLaunchFailureAreRedacted(t *testing.T) {
	f := setupExecution(t, "no_handle")
	tag := f.tag(t)
	w := executionRequest(f.h, "POST", f.startPath(), `{"role":"design"}`, "fixture-start", tag, "fixture-management")
	if w.Code != 409 || strings.Contains(w.Body.String(), "fixture-sensitive") {
		t.Fatal("partial launch error exposed", w.Code, w.Body.String())
	}
	reply := executionReply(t, w)
	if reply.Run.State != "interrupted" || !reply.Created {
		t.Fatal("partial durable intent hidden")
	}
	if _, e := f.st.Reservation(reply.Run.ID); e != nil {
		t.Fatal("failed launch released", e)
	}
	conflict := executionRequest(f.h, "POST", f.startPath(), `{"role":"review"}`, "fixture-start", tag, "fixture-management")
	if conflict.Code != 409 {
		t.Fatal("key payload rebound", conflict.Code)
	}
	retry := executionRequest(f.h, "POST", f.startPath(), `{"role":"design"}`, "fixture-start", tag, "fixture-management")
	if retry.Code != 200 || executionReply(t, retry).Created || f.started.Load() != 1 {
		t.Fatal("failed intent replayed")
	}
}
func TestExecutionReadAndCancelCannotAdoptAnotherTaskOrGeneration(t *testing.T) {
	f := setupExecution(t, "")
	w := executionRequest(f.h, "POST", f.startPath(), `{"role":"design"}`, "fixture-start", f.tag(t), "fixture-management")
	reply := executionReply(t, w)
	get := request(f.h, "GET", "/agent/v1/tasks/fixture-other/runs/"+reply.Run.ID, "", "", "fixture-management")
	if get.Code != 404 {
		t.Fatal("run returned under another task", get.Code)
	}
	w = executionRequest(f.h, "POST", "/control/v1/tasks/"+f.task.ID+"/runs/"+reply.Run.ID+"/cancel", `{}`, "", `"p1-g0-ready"`, "fixture-management")
	if w.Code != 412 {
		t.Fatal("stale generation cancelled", w.Code)
	}
	w = executionRequest(f.h, "POST", "/control/v1/tasks/"+f.task.ID+"/runs/"+reply.Run.ID+"/cancel", `{"token":"fixture"}`, "", f.tag(t), "fixture-management")
	if w.Code != 400 {
		t.Fatal("cancel body carried authority", w.Code)
	}
	r, e := f.st.Run(reply.Run.ID)
	if e != nil || r.State != "running" {
		t.Fatal("invalid cancel changed state", e)
	}
}
func TestExecutionControllerMustUseSameStoreAndCannotBeHotReplaced(t *testing.T) {
	f := setupExecution(t, "")
	if e := f.server.SetController(f.c); !errors.Is(e, store.ErrConflict) {
		t.Fatal("hot controller rebind admitted", e)
	}
	other, _, h := setup(t)
	if e := other.SetController(f.c); !errors.Is(e, errInvalid) {
		t.Fatal("cross-store controller admitted", e)
	}
	w := executionRequest(h, "POST", "/control/v1/tasks/fixture/start", `{"role":"design"}`, "fixture-start", `"p1-g0-ready"`, "fixture-management")
	if w.Code != 503 {
		t.Fatal("missing controller enabled control", w.Code)
	}
}
func TestExecutionHTTPResponseAndReadUseRealAuthenticatedTransport(t *testing.T) {
	f := setupExecution(t, "")
	server := httptest.NewServer(f.h)
	defer server.Close()
	r, e := http.NewRequest("POST", server.URL+f.startPath(), strings.NewReader(`{"role":"design"}`))
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Set("Authorization", "Bearer fixture-management")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "fixture-live-start")
	r.Header.Set("If-Match", f.tag(t))
	resp, e := server.Client().Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	raw, e := io.ReadAll(resp.Body)
	if e != nil || resp.StatusCode != 202 || resp.Header.Get("X-Fusion-Task-ETag") == "" {
		t.Fatal("actual HTTP execution response missing", resp.StatusCode, e)
	}
	var reply ExecutionReply
	if json.Unmarshal(raw, &reply) != nil || !reply.Created {
		t.Fatal("actual response invalid")
	}
	close(f.finish)
	waitAPIExecution(t, f, reply.Run.ID)
}

package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/quota"
	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

type fixtureExecution struct {
	end     chan struct{}
	done    chan struct{}
	once    sync.Once
	result  managed.Result
	waitErr error
}

func (f *fixtureExecution) Cancel() error { f.once.Do(func() { close(f.end) }); return nil }
func (f *fixtureExecution) Wait(ctx context.Context) (managed.Result, error) {
	select {
	case <-ctx.Done():
		return managed.Result{}, ctx.Err()
	case <-f.done:
		return f.result, f.waitErr
	}
}

type controlFixture struct {
	st                          *store.Store
	root                        string
	config                      Config
	in                          store.StartIdentity
	launch                      Launch
	started, resolved, released atomic.Int64
	finish                      chan struct{}
	mode                        string
}

func fixturePrivate(t *testing.T) string {
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
func newControlFixture(t *testing.T) *controlFixture {
	t.Helper()
	root := fixturePrivate(t)
	st, e := store.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { st.Close() })
	effort := "high"
	route := stageplan.Route{ID: "fixture-route", Revision: 1, Model: "glm-5.3", Account: "fixture-account", Workspace: "fixture-workspace", CredentialIdentity: "fixture-credential", RuntimeVersion: "2.1.287", BillingPath: "coding_plan", BillingKnown: true, Admitted: true, Efforts: []string{effort}, DefaultEffort: &effort, Capabilities: []string{"text"}, LockEnforcement: stageplan.ControlledCalls}
	plan, e := stageplan.Compile(1, []stageplan.Role{stageplan.Design}, stageplan.Layer{Roles: map[stageplan.Role]stageplan.Binding{stageplan.Design: {Mode: stageplan.Locked, Model: route.Model, Route: &stageplan.RouteRef{ID: route.ID, Revision: 1}, Effort: &stageplan.EffortSelection{Mode: stageplan.EffortExplicit, Value: effort}}}}, stageplan.Layer{}, stageplan.Layer{}, []stageplan.Route{route})
	if e != nil {
		t.Fatal(e)
	}
	task, e := st.Create("fixture-control-task", store.CreateRequest{ProjectID: "fixture-project", Goal: "fixture goal", Plan: plan, Budget: &store.Budget{MaxCalls: 3}})
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	used := float64(20)
	env := policy.Inspection{Route: route, Provider: "fixture-provider", QueryAdmitted: true, DataAllowed: true, SandboxVerified: true, VerificationAvailable: true, AllCallsCounted: true, ManagedExecutions: 1, ProofHash: strings.Repeat("a", 64), Quota: quota.Snapshot{Identity: quota.Identity{Provider: "fixture-provider", Account: route.Account, Workspace: route.Workspace, Region: "CN", Generation: 1}, Source: "fixture-source", ObservedAt: &now, ReceivedAt: now, Complete: true, Status: quota.Available, Pool: quota.Pool{ID: "fixture-pool", Provider: "fixture-provider", Region: "CN", Scope: "account", Owner: route.Account, Verified: true}, Windows: []quota.Window{{Kind: "subscription", Unit: "percent", UsedPercent: &used}}}}
	f := &controlFixture{st: st, root: root, in: store.StartIdentity{TaskID: task.ID, Role: stageplan.Design, PlanRevision: 1, Generation: 0}, finish: make(chan struct{})}
	f.config.Scheduler = policy.Scheduler{Store: st, Inspect: func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (policy.Inspection, error) {
		return env, nil
	}}
	f.launch.Spec = managed.Spec{Root: fixturePrivate(t), Workspace: fixturePrivate(t), Timeout: time.Second, Input: []byte("fixture prompt")}
	f.launch.Backend = Backend{Probe: func(context.Context) (managed.Capabilities, error) {
		return managed.Capabilities{Probe: true, Start: true, Events: true, Cancel: true}, nil
	}, Release: func(p policy.StopProof) error {
		f.released.Add(1)
		if f.mode == "release_error" {
			return errors.New("fixture secret release failure")
		}
		return st.ReleaseReserved(p.RunID, p.Generation, p.ReportHash, true)
	}, Start: func(ctx context.Context, r store.StageRun, _ managed.Spec) (Execution, error) {
		f.started.Add(1)
		if f.mode == "no_handle" {
			return nil, errors.New("fixture secret prelaunch failure")
		}
		if e := st.ConfirmStarted(r.ID, r.Generation, r.Owner, "fixture-session"); e != nil {
			return nil, e
		}
		h := &fixtureExecution{end: make(chan struct{}), done: make(chan struct{})}
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
			h.result = managed.Result{State: state, StoppedVerified: f.mode != "no_stop", Proof: policy.StopProof{RunID: r.ID, Generation: r.Generation, NativeSessionID: "fixture-session", ProcessIdentityHash: strings.Repeat("b", 64), ReportHash: strings.Repeat("c", 64), DescendantsStopped: true}}
			if f.mode == "wrong_proof" {
				h.result.Proof.NativeSessionID = "fixture-unrelated-session"
			}
			if f.mode == "wait_error" {
				h.waitErr = errors.New("fixture secret wait failure")
			}
			close(h.done)
		}()
		if f.mode == "known_handle_error" {
			return h, errors.New("fixture secret activation failure")
		}
		return h, nil
	}}
	f.config.Resolve = func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (Launch, error) {
		f.resolved.Add(1)
		return f.launch, nil
	}
	return f
}
func (f *controlFixture) controller(t *testing.T) *Controller {
	t.Helper()
	c, e := New(context.Background(), f.config)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := c.Close(ctx); e != nil {
			t.Error(e)
		}
	})
	return c
}
func waitControl(t *testing.T, c *Controller, id string) Completion {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, e := c.Wait(ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	return got
}
func TestControllerRetriesStartRuntimeOnlyOnceAndHTTPDisconnectDoesNotCancel(t *testing.T) {
	f := newControlFixture(t)
	c := f.controller(t)
	request, cancel := context.WithCancel(context.Background())
	first, e := c.Start(request, "fixture-start", f.in)
	if e != nil || !first.Created {
		t.Fatal(e)
	}
	cancel()
	var wg sync.WaitGroup
	for n := 0; n < 12; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, e := c.Start(context.Background(), "fixture-start", f.in)
			if e != nil || got.Created || got.Run.ID != first.Run.ID {
				t.Errorf("retry replayed: %v", e)
			}
		}()
	}
	wg.Wait()
	r, e := f.st.Run(first.Run.ID)
	if e != nil || r.State != "running" || f.started.Load() != 1 || f.resolved.Load() != 1 {
		t.Fatal("disconnect cancelled or replayed", e)
	}
	close(f.finish)
	done := waitControl(t, c, r.ID)
	if done.State != "succeeded" || !done.Released || f.released.Load() != 1 {
		t.Fatal(done)
	}
	task, e := f.st.Task(f.in.TaskID)
	if e != nil || task.State != "ready" {
		t.Fatal("Native completion became engineering acceptance", e)
	}
}
func TestControllerNoHandleRetainsReservationForReconciliation(t *testing.T) {
	f := newControlFixture(t)
	f.mode = "no_handle"
	c := f.controller(t)
	got, e := c.Start(context.Background(), "fixture-start", f.in)
	if !errors.Is(e, ErrLaunch) || !got.Created {
		t.Fatal(e)
	}
	r, e := f.st.Run(got.Run.ID)
	if e != nil || r.State != "interrupted" {
		t.Fatal("failed intent not held for review", e)
	}
	if _, e = f.st.Reservation(r.ID); e != nil || f.released.Load() != 0 {
		t.Fatal("missing handle released capacity", e)
	}
	retry, e := c.Start(context.Background(), "fixture-start", f.in)
	if e != nil || retry.Created || f.started.Load() != 1 {
		t.Fatal("failed launch replayed", e)
	}
	done := waitControl(t, c, r.ID)
	if done.Released || done.StoppedVerified {
		t.Fatal("invented no-spawn stop proof")
	}
}
func TestControllerKnownHandleErrorIsCancelledWaitedAndReleased(t *testing.T) {
	f := newControlFixture(t)
	f.mode = "known_handle_error"
	c := f.controller(t)
	got, e := c.Start(context.Background(), "fixture-start", f.in)
	if !errors.Is(e, ErrLaunch) || got.Run.ID == "" {
		t.Fatal(e)
	}
	done := waitControl(t, c, got.Run.ID)
	if done.State != "cancelled" || !done.StoppedVerified || !done.Released || f.released.Load() != 1 {
		t.Fatal("known handle lost", done)
	}
}
func TestControllerStopAndReleaseFailuresDoNotFreeCapacity(t *testing.T) {
	for _, mode := range []string{"no_stop", "wait_error", "release_error", "wrong_proof"} {
		t.Run(mode, func(t *testing.T) {
			f := newControlFixture(t)
			f.mode = mode
			c := f.controller(t)
			got, e := c.Start(context.Background(), "fixture-start", f.in)
			if e != nil {
				t.Fatal(e)
			}
			close(f.finish)
			done := waitControl(t, c, got.Run.ID)
			if done.Released {
				t.Fatal("unverified release", mode)
			}
			if _, e = f.st.Reservation(got.Run.ID); e != nil {
				t.Fatal("capacity was freed", e)
			}
		})
	}
}
func TestControllerCancelRequiresOwnedRunAndGeneration(t *testing.T) {
	f := newControlFixture(t)
	c := f.controller(t)
	got, e := c.Start(context.Background(), "fixture-start", f.in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.Cancel("fixture-other-task", got.Run.ID, got.Run.Generation); !errors.Is(e, ErrIdentity) {
		t.Fatal(e)
	}
	if _, e = c.Cancel(f.in.TaskID, got.Run.ID, got.Run.Generation+1); !errors.Is(e, ErrIdentity) {
		t.Fatal(e)
	}
	if _, e = c.Cancel(f.in.TaskID, got.Run.ID, got.Run.Generation); e != nil {
		t.Fatal(e)
	}
	done := waitControl(t, c, got.Run.ID)
	if done.State != "cancelled" || !done.Released {
		t.Fatal(done)
	}
	if _, e = c.Cancel(f.in.TaskID, got.Run.ID, got.Run.Generation); e != nil {
		t.Fatal("terminal cancel not idempotent", e)
	}
}
func TestControllerCloseStopsOwnedExecutionAndRefusesNewLaunch(t *testing.T) {
	f := newControlFixture(t)
	c := f.controller(t)
	got, e := c.Start(context.Background(), "fixture-start", f.in)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if e = c.Close(ctx); e != nil {
		t.Fatal(e)
	}
	done := waitControl(t, c, got.Run.ID)
	if !done.Released || done.State != "cancelled" {
		t.Fatal(done)
	}
	if _, e = c.Start(context.Background(), "fixture-new-key", f.in); !errors.Is(e, ErrClosed) {
		t.Fatal("closed controller started", e)
	}
}
func TestControllerRejectsUntrustedSpecBeforeIntent(t *testing.T) {
	for _, mode := range []string{"argv", "env", "session", "executable", "validator", "write", "timeout", "input", "path", "backend"} {
		t.Run(mode, func(t *testing.T) {
			f := newControlFixture(t)
			switch mode {
			case "argv":
				f.launch.Spec.Args = []string{"fixture"}
			case "env":
				f.launch.Spec.FixtureEnvironment = map[string]string{"fixture": "value"}
			case "session":
				f.launch.Spec.NativeSessionID = "caller-session"
			case "executable":
				f.launch.Spec.Executable = "/fixture/executable"
			case "validator":
				f.launch.Spec.ValidateOutcome = func([]byte) bool { return true }
			case "write":
				f.launch.Spec.Writable = true
			case "timeout":
				f.launch.Spec.Timeout = 11 * time.Minute
			case "input":
				f.launch.Spec.Input = []byte{0xff}
			case "path":
				f.launch.Spec.Workspace = "relative"
			case "backend":
				f.launch.Backend.Release = nil
			}
			c := f.controller(t)
			if _, e := c.Start(context.Background(), "fixture-start", f.in); !errors.Is(e, ErrUnsupported) {
				t.Fatal(e)
			}
			task, e := f.st.Task(f.in.TaskID)
			if e != nil || task.State != "ready" || task.Generation != 0 || f.started.Load() != 0 {
				t.Fatal("bad trusted wiring recorded an intent", e)
			}
		})
	}
}
func TestControllerRestartReadsUnknownWithoutResolvingOrLaunching(t *testing.T) {
	f := newControlFixture(t)
	f.mode = "no_handle"
	c := f.controller(t)
	got, _ := c.Start(context.Background(), "fixture-start", f.in)
	// Explicitly create an active intent with a new fixture task to exercise
	// Store recovery rather than changing the failed intent into a new launch.
	plan, e := f.st.Plan(f.in.TaskID, 1)
	if e != nil {
		t.Fatal(e)
	}
	task, e := f.st.Create("fixture-recovery-task", store.CreateRequest{ProjectID: "fixture-project", Goal: "fixture", Plan: plan, Budget: &store.Budget{MaxCalls: 3}})
	if e != nil {
		t.Fatal(e)
	}
	in := f.in
	in.TaskID = task.ID
	gen := int64(0)
	r, e := f.st.StartReservedOnce(store.StartRequest{TaskID: in.TaskID, Role: in.Role, PlanRevision: 1, Owner: "fixture-recovery-owner", TTL: time.Minute, Target: *plan.Bindings[in.Role].Target, IdempotencyKey: "fixture-recovery-start", ExpectedGeneration: &gen}, store.ReservationRequest{PoolKey: "fixture-other-pool", GlobalLimit: 2, AdmissionHash: strings.Repeat("d", 64)})
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if e = c.Close(ctx); e != nil {
		t.Fatal(e)
	}
	if e = f.st.Close(); e != nil {
		t.Fatal(e)
	}
	st, e := store.Open(f.root)
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	config := f.config
	config.Scheduler.Store = st
	config.Resolve = func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (Launch, error) {
		t.Error("recovery resolved a Runtime")
		return Launch{}, nil
	}
	other, e := New(context.Background(), config)
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close(context.Background())
	retry, e := other.Start(context.Background(), "fixture-recovery-start", in)
	if e != nil || retry.Created || retry.Run.ID != r.Run.ID || retry.Run.State != "unknown" {
		t.Fatal("restart replayed", e, got.Run.ID)
	}
	if _, e = other.Cancel(in.TaskID, retry.Run.ID, retry.Run.Generation); !errors.Is(e, ErrReconcile) {
		t.Fatal("adopted recovered process", e)
	}
}

func TestControllerCancellationBeforeHandlePublicationStillWaitsAndReleases(t *testing.T) {
	f := newControlFixture(t)
	base := f.launch.Backend.Start
	entered, release := make(chan store.StageRun, 1), make(chan struct{})
	f.launch.Backend.Start = func(ctx context.Context, r store.StageRun, spec managed.Spec) (Execution, error) {
		h, e := base(ctx, r, spec)
		entered <- r
		<-release
		return h, e
	}
	c := f.controller(t)
	result := make(chan error, 1)
	go func() { _, e := c.Start(context.Background(), "fixture-start", f.in); result <- e }()
	r := <-entered
	if _, e := c.Cancel(r.TaskID, r.ID, r.Generation); e != nil {
		t.Fatal(e)
	}
	close(release)
	if e := <-result; e != nil {
		t.Fatal(e)
	}
	done := waitControl(t, c, r.ID)
	if !done.Released || done.State != "cancelled" {
		t.Fatal("cancelled unpublished handle lost", done)
	}
}

func TestControllerCloseTimeoutDoesNotClaimStopped(t *testing.T) {
	f := newControlFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	base := f.launch.Backend.Start
	f.launch.Backend.Start = func(ctx context.Context, r store.StageRun, spec managed.Spec) (Execution, error) {
		close(entered)
		<-release
		return base(ctx, r, spec)
	}
	c := f.controller(t)
	result := make(chan store.StartReceipt, 1)
	go func() { got, _ := c.Start(context.Background(), "fixture-start", f.in); result <- got }()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if e := c.Close(ctx); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("unobserved process declared stopped", e)
	}
	close(release)
	got := <-result
	if e := c.Close(context.Background()); e != nil {
		t.Fatal(e)
	}
	done := waitControl(t, c, got.Run.ID)
	if !done.Released {
		t.Fatal("late owned handle abandoned", done)
	}
}

func TestControllerFrozenAutoSelectionCannotEscapeApprovedCandidates(t *testing.T) {
	f := newControlFixture(t)
	c := f.controller(t)
	plan, e := f.st.Plan(f.in.TaskID, 1)
	if e != nil {
		t.Fatal(e)
	}
	target := *plan.Bindings[stageplan.Design].Target
	binding := stageplan.FrozenBinding{Mode: stageplan.Auto, Candidates: []stageplan.ExecutionTarget{target}}
	c.config.SelectAuto = func(_ context.Context, _ store.Task, _ stageplan.Role, b stageplan.FrozenBinding) (stageplan.ExecutionTarget, error) {
		b.Candidates[0].Account = "fixture-other-account"
		return b.Candidates[0], nil
	}
	if _, e = c.target(context.Background(), store.Task{}, stageplan.Design, binding); !errors.Is(e, ErrIdentity) {
		t.Fatal("selector mutated frozen approval", e)
	}
	if binding.Candidates[0].Account != target.Account {
		t.Fatal("selector changed original frozen target")
	}
	c.config.SelectAuto = func(_ context.Context, _ store.Task, _ stageplan.Role, b stageplan.FrozenBinding) (stageplan.ExecutionTarget, error) {
		return b.Candidates[0], nil
	}
	got, e := c.target(context.Background(), store.Task{}, stageplan.Design, binding)
	if e != nil || !equal(got, target) {
		t.Fatal("approved selection rejected", e)
	}
}

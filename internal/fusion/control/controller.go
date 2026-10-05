// Package control owns trusted execution lifetimes. Its configuration is
// server wiring, never a task body or a credential/argv issuing endpoint.
package control

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/fusion/policy"
	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
	"github.com/yetone/magpie/internal/fusion/workspace"
)

var (
	ErrClosed      = errors.New("execution controller closed")
	ErrBusy        = errors.New("execution controller capacity unavailable")
	ErrIdentity    = errors.New("execution control identity mismatch")
	ErrUnsupported = errors.New("execution configuration unsupported")
	ErrLaunch      = errors.New("execution requires reconciliation after launch failure")
	ErrReconcile   = errors.New("execution requires explicit reconciliation")
	ErrForbidden   = errors.New("execution management authority unavailable")
)

type Execution interface {
	Cancel() error
	Wait(context.Context) (managed.Result, error)
}
type Backend struct {
	Probe   func(context.Context) (managed.Capabilities, error)
	Start   func(context.Context, store.StageRun, managed.Spec) (Execution, error)
	Release func(policy.StopProof) error
	// ValidateLaunch rejects adapter-specific constraints before intent. It is
	// trusted wiring, not admission or a replacement for post-intent checks.
	ValidateLaunch func(context.Context, stageplan.Role, stageplan.ExecutionTarget, managed.Spec) error
	// CheckRestore must verify the sealed checkpoint/current source before
	// intent. Restore must repeat those checks for the fresh prepared run.
	CheckRestore func(context.Context, store.StageRun, stageplan.ExecutionTarget, managed.Spec, store.RestoreIdentity) error
	Restore      func(context.Context, store.StageRun, managed.Spec, store.RestoreIdentity) (Execution, error)
	Checkpoint   func(context.Context, store.StageRun) (CheckpointRef, error)
}
type ReleasingAdapter interface {
	managed.Adapter
	Release(policy.StopProof) error
}

// BindAdapter preserves a known non-nil Handle even when Start returns an
// error. Release stays on the same adapter/Supervisor that owns its proof.
func BindAdapter(a ReleasingAdapter) Backend {
	if a == nil {
		return Backend{}
	}
	b := Backend{Probe: a.Probe, Release: a.Release, Start: func(ctx context.Context, r store.StageRun, spec managed.Spec) (Execution, error) {
		h, e := a.Start(ctx, r, spec)
		if h == nil {
			return nil, e
		}
		return h, e
	}}
	if v, ok := a.(interface {
		ValidateLaunch(context.Context, stageplan.Role, stageplan.ExecutionTarget, managed.Spec) error
	}); ok {
		b.ValidateLaunch = v.ValidateLaunch
	}
	return b
}

type Launch struct {
	Backend Backend
	Spec    managed.Spec
}
type Config struct {
	RequireSource bool
	Scheduler     policy.Scheduler
	Resolve       func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (Launch, error)
	SelectAuto    func(context.Context, store.Task, stageplan.Role, stageplan.FrozenBinding) (stageplan.ExecutionTarget, error)
}
type Completion struct {
	State           string `json:"state"`
	StoppedVerified bool   `json:"stopped_verified"`
	Released        bool   `json:"released"`
}
type executionJob struct {
	run        store.StageRun
	mu         sync.Mutex
	handle     Execution
	cancel     context.CancelFunc
	done       chan struct{}
	completion Completion
	stoppedRun store.StageRun
	checkpoint func(context.Context, store.StageRun) (CheckpointRef, error)
	source     workspace.SourceGuard
	workspace  string
}
type Controller struct {
	config     Config
	owner      string
	lifetime   context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex
	closed     bool
	closedDone chan struct{}
	jobs       map[string]*executionJob
	pending    int
	slots      chan struct{}
	wg         sync.WaitGroup
}

func New(parent context.Context, config Config) (*Controller, error) {
	if parent == nil || parent.Err() != nil || config.Scheduler.Store == nil || config.Scheduler.Inspect == nil || config.Resolve == nil {
		return nil, ErrUnsupported
	}
	var nonce [32]byte
	if _, e := rand.Read(nonce[:]); e != nil {
		return nil, ErrUnsupported
	}
	lifetime, cancel := context.WithCancel(parent)
	return &Controller{config: config, owner: "control-" + hex.EncodeToString(nonce[:]), lifetime: lifetime, cancel: cancel, jobs: map[string]*executionJob{}, slots: make(chan struct{}, 16), closedDone: make(chan struct{})}, nil
}

// Start accepts only a durable request identity. Target, workspace, prompt,
// executable, credentials and Native settings never come from this request.
// Caller authentication is required before invoking this internal service.
func (c *Controller) Start(request context.Context, key string, in store.StartIdentity) (store.StartReceipt, error) {
	if in.Restore != nil {
		return store.StartReceipt{}, ErrUnsupported
	}
	return c.start(request, key, in, nil)
}

// StartAuthorized repeats the exact issuer's management check through slow
// preflight work, before committing an intent. After commit, HTTP disconnect
// cannot cancel the owned execution; model admission remains independently
// enforced by CheckPrepared/Permit and the Adapter's current grant checks.
func (c *Controller) StartAuthorized(request context.Context, key string, in store.StartIdentity, current func(context.Context) bool) (store.StartReceipt, error) {
	if current == nil {
		return store.StartReceipt{}, ErrForbidden
	}
	if in.Restore != nil {
		return store.StartReceipt{}, ErrUnsupported
	}
	return c.start(request, key, in, current)
}
func (c *Controller) UsesStore(st *store.Store) bool {
	return c != nil && c.config.Scheduler.Store == st
}
func (c *Controller) start(request context.Context, key string, in store.StartIdentity, current func(context.Context) bool) (store.StartReceipt, error) {
	if c == nil || request == nil {
		return store.StartReceipt{}, ErrUnsupported
	}
	in = clone(in)
	c.mu.Lock()
	closed := c.closed || c.lifetime.Err() != nil
	c.mu.Unlock()
	if closed {
		return store.StartReceipt{}, ErrClosed
	}
	if current != nil && !current(request) {
		return store.StartReceipt{}, ErrForbidden
	}
	old, e := c.config.Scheduler.Store.LookupStart(key, in)
	if e == nil {
		return store.StartReceipt{Run: old, Created: false}, nil
	}
	if !errors.Is(e, store.ErrNotFound) {
		return store.StartReceipt{}, e
	}
	if e = c.acquire(); e != nil {
		return store.StartReceipt{}, e
	}
	pending, transferred := true, false
	finishWork := func() {
		if pending {
			c.mu.Lock()
			c.pending--
			c.mu.Unlock()
		}
		<-c.slots
		c.wg.Done()
	}
	defer func() {
		if !transferred {
			finishWork()
		}
	}()
	task, e := c.config.Scheduler.Store.Task(in.TaskID)
	if e != nil {
		return store.StartReceipt{}, e
	}
	if task.State != "ready" || task.PlanRevision != in.PlanRevision || task.Generation != in.Generation {
		return c.retryOrError(key, in, store.ErrConflict)
	}
	if e = c.config.Scheduler.Store.ValidateStageStartAuthorized(in, func() bool {
		return request.Err() == nil && c.lifetime.Err() == nil && (current == nil || current(request))
	}); e != nil {
		return c.retryOrError(key, in, e)
	}
	plan, e := c.config.Scheduler.Store.Plan(in.TaskID, in.PlanRevision)
	if e != nil {
		return store.StartReceipt{}, e
	}
	binding, ok := plan.Bindings[in.Role]
	if !ok {
		return store.StartReceipt{}, ErrIdentity
	}
	target, e := c.target(request, task, in.Role, binding)
	if e != nil {
		return c.retryOrError(key, in, e)
	}
	var origin store.StageRun
	if in.Restore != nil {
		origin, e = c.config.Scheduler.Store.Run(in.Restore.OriginRunID)
		if e != nil || origin.TaskID != task.ID || origin.Role != in.Role ||
			origin.PlanRevision != in.PlanRevision || origin.Generation > in.Generation ||
			origin.State != "succeeded" || origin.Owner != "" || !origin.LaunchConfirmed ||
			origin.NativeSessionID == "" || !equal(origin.Target, target) {
			return c.retryOrError(key, in, ErrIdentity)
		}
		if _, e = c.config.Scheduler.Store.Reservation(origin.ID); !errors.Is(e, store.ErrNotFound) {
			return c.retryOrError(key, in, ErrIdentity)
		}
	}
	launch, e := c.config.Resolve(request, task, in.Role, clone(target))
	valid := validLaunch(launch, in.Role)
	if in.Restore != nil {
		valid = validRestoreLaunch(launch, in.Role)
	}
	valid = valid && (!c.config.RequireSource || launch.Spec.Source.Present())
	if e != nil || !valid {
		return c.retryOrError(key, in, ErrUnsupported)
	}
	launch.Spec.Input = append([]byte(nil), launch.Spec.Input...)
	launch.Spec.WritePaths = append([]string(nil), launch.Spec.WritePaths...)
	if validate := launch.Backend.ValidateLaunch; validate != nil {
		spec := launch.Spec
		spec.Input = append([]byte(nil), spec.Input...)
		spec.WritePaths = append([]string(nil), spec.WritePaths...)
		if e := validate(request, in.Role, clone(target), spec); e != nil {
			return c.retryOrError(key, in, ErrUnsupported)
		}
	}
	if in.Restore != nil {
		spec := launch.Spec
		spec.Input = append([]byte(nil), spec.Input...)
		spec.WritePaths = append([]string(nil), spec.WritePaths...)
		if e = launch.Backend.CheckRestore(request, clone(origin), clone(target), spec, *in.Restore); e != nil {
			return c.retryOrError(key, in, ErrIdentity)
		}
	}
	probeCtx, probeCancel := context.WithTimeout(request, 5*time.Second)
	caps, e := launch.Backend.Probe(probeCtx)
	probeCancel()
	if e != nil || !caps.Probe || !caps.Start || !caps.Cancel || !caps.Events || caps.ChildProcesses || caps.Network {
		return c.retryOrError(key, in, ErrUnsupported)
	}
	if current != nil && !current(request) {
		return store.StartReceipt{}, ErrForbidden
	}
	if !launch.Spec.SourceCurrent() {
		return c.retryOrError(key, in, ErrUnsupported)
	}
	gen := in.Generation
	scheduler := c.config.Scheduler
	if current != nil || launch.Spec.Source.Present() {
		inspect := scheduler.Inspect
		scheduler.Inspect = func(ctx context.Context, task store.Task, role stageplan.Role, target stageplan.ExecutionTarget) (policy.Inspection, error) {
			if current != nil && !current(request) {
				return policy.Inspection{}, ErrForbidden
			}
			if !launch.Spec.SourceCurrent() {
				return policy.Inspection{}, ErrUnsupported
			}
			out, err := inspect(ctx, task, role, target)
			if current != nil && !current(request) {
				return policy.Inspection{}, ErrForbidden
			}
			if !launch.Spec.SourceCurrent() {
				return policy.Inspection{}, ErrUnsupported
			}
			return out, err
		}
	}
	receipt, e := scheduler.PrepareOnce(request, store.StartRequest{TaskID: in.TaskID, Role: in.Role, PlanRevision: in.PlanRevision, ExpectedGeneration: &gen, IdempotencyKey: key, Owner: c.owner, TTL: time.Minute, Target: target, Restore: in.Restore})
	if current != nil && !current(request) && !receipt.Created {
		return store.StartReceipt{}, ErrForbidden
	}
	if e != nil || !receipt.Created {
		return receipt, e
	}
	// From this commit onward, HTTP cancellation cannot terminate an owned
	// lifetime or drop a known process. Close/cancel/Runtime deadline can.
	lifetime, cancel := context.WithCancel(c.lifetime)
	job := &executionJob{run: clone(receipt.Run), cancel: cancel, done: make(chan struct{}), checkpoint: launch.Backend.Checkpoint, source: launch.Spec.Source, workspace: launch.Spec.Workspace}
	c.mu.Lock()
	c.pending--
	pending = false
	c.jobs[job.run.ID] = job
	c.mu.Unlock()
	if e = c.config.Scheduler.CheckPrepared(lifetime, job.run); e != nil {
		c.unlaunched(job)
		return receipt, ErrLaunch
	}
	if !launch.Spec.SourceCurrent() {
		c.unlaunched(job)
		return receipt, ErrLaunch
	}
	var h Execution
	if in.Restore != nil {
		h, e = launch.Backend.Restore(lifetime, clone(job.run), launch.Spec, *in.Restore)
	} else {
		h, e = launch.Backend.Start(lifetime, clone(job.run), launch.Spec)
	}
	if h == nil {
		c.unlaunched(job)
		return receipt, ErrLaunch
	}
	job.mu.Lock()
	job.handle = h
	job.mu.Unlock()
	// Pause/Close can cancel the owned lifetime before Start returns its
	// Handle. Deliver that cancellation once the exact handle is known even
	// when the backend did not observe the context while launching.
	if e != nil || lifetime.Err() != nil {
		cancel()
		_ = h.Cancel()
	}
	transferred = true
	go func() { defer finishWork(); c.observe(job, h, launch.Backend) }()
	if e != nil {
		return receipt, ErrLaunch
	}
	return receipt, nil
}
func (c *Controller) acquire() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.lifetime.Err() != nil {
		return ErrClosed
	}
	if len(c.jobs)+c.pending >= 4096 {
		return ErrBusy
	}
	select {
	case c.slots <- struct{}{}:
	default:
		return ErrBusy
	}
	c.pending++
	c.wg.Add(1)
	return nil
}
func (c *Controller) retryOrError(key string, in store.StartIdentity, original error) (store.StartReceipt, error) {
	r, e := c.config.Scheduler.Store.LookupStart(key, in)
	if e == nil {
		return store.StartReceipt{Run: r, Created: false}, nil
	}
	if !errors.Is(e, store.ErrNotFound) {
		return store.StartReceipt{}, e
	}
	return store.StartReceipt{}, original
}
func clone[T any](v T) T {
	raw, _ := json.Marshal(v)
	var out T
	_ = json.Unmarshal(raw, &out)
	return out
}
func equal(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
func (c *Controller) target(ctx context.Context, task store.Task, role stageplan.Role, b stageplan.FrozenBinding) (stageplan.ExecutionTarget, error) {
	if b.Mode == stageplan.Locked && b.Target != nil {
		return clone(*b.Target), nil
	}
	if b.Mode != stageplan.Auto || c.config.SelectAuto == nil {
		return stageplan.ExecutionTarget{}, ErrUnsupported
	}
	selected, e := c.config.SelectAuto(ctx, task, role, clone(b))
	if e != nil {
		return stageplan.ExecutionTarget{}, ErrUnsupported
	}
	for _, candidate := range b.Candidates {
		if equal(candidate, selected) {
			return clone(candidate), nil
		}
	}
	return stageplan.ExecutionTarget{}, ErrIdentity
}
func validLaunch(l Launch, role stageplan.Role) bool {
	s := l.Spec
	return l.Backend.Probe != nil && l.Backend.Start != nil && l.Backend.Release != nil &&
		s.SourceCurrent() && s.ValidWriteScope() &&
		filepath.IsAbs(s.Root) && filepath.Clean(s.Root) == s.Root && filepath.IsAbs(s.Workspace) && filepath.Clean(s.Workspace) == s.Workspace &&
		!strings.ContainsRune(s.Root, 0) && !strings.ContainsRune(s.Workspace, 0) && s.Root != s.Workspace &&
		s.Timeout > 0 && s.Timeout <= 10*time.Minute && len(s.Input) > 0 && len(s.Input) <= 64<<10 && utf8.Valid(s.Input) &&
		(!s.Writable || role == stageplan.Implementation || role == stageplan.Testing) && s.Executable == "" && s.ExecutableHash == "" && len(s.Args) == 0 && len(s.FixtureEnvironment) == 0 && s.NativeSessionID == "" && s.ValidateOutcome == nil && s.ClaudeChannel == nil && s.GrokChannel == nil && s.CodexChannel == nil
}
func (c *Controller) unlaunched(j *executionJob) {
	j.cancel()
	// No Handle does not establish a kernel stop proof. Keep reservation and
	// budget; Finish fences this intent from later completion or replay.
	_ = c.config.Scheduler.Store.Finish(j.run.ID, j.run.Generation, j.run.Owner, "interrupted")
	j.completion = Completion{State: "execution_uncertain"}
	close(j.done)
}
func terminal(state string) bool {
	switch state {
	case "succeeded", "failed", "cancelled", "interrupted", "advisory_only":
		return true
	}
	return false
}
func (c *Controller) observe(j *executionJob, h Execution, b Backend) {
	result, e := h.Wait(context.Background())
	completion := Completion{State: "execution_uncertain"}
	r, re := c.config.Scheduler.Store.Run(j.run.ID)
	if re == nil && r.Generation == j.run.Generation && terminal(r.State) {
		completion.State = r.State
		p := result.Proof
		if e == nil && result.StoppedVerified && p.RunID == r.ID && p.Generation == r.Generation && p.NativeSessionID == r.NativeSessionID && p.DescendantsStopped && b.Release(p) == nil {
			completion.StoppedVerified = true
			completion.Released = true
			j.stoppedRun = clone(r)
		}
	}
	j.cancel()
	j.completion = completion
	close(j.done)
}
func (c *Controller) Cancel(taskID, runID string, generation int64) (store.StageRun, error) {
	return c.cancelRun(taskID, runID, generation, 0)
}

// CancelAtRevision applies the public task revision precondition atomically
// with a cancellation intent. Cancel remains available for owned shutdowns.
func (c *Controller) CancelAtRevision(taskID, runID string, generation, revision int64) (store.StageRun, error) {
	if revision < 1 {
		return store.StageRun{}, store.ErrInvalid
	}
	return c.cancelRun(taskID, runID, generation, revision)
}
func (c *Controller) cancelRun(taskID, runID string, generation, revision int64) (store.StageRun, error) {
	if c == nil {
		return store.StageRun{}, ErrUnsupported
	}
	r, e := c.config.Scheduler.Store.Run(runID)
	if e != nil || r.TaskID != taskID || r.Generation != generation {
		return store.StageRun{}, ErrIdentity
	}
	if terminal(r.State) {
		if revision > 0 {
			task, e := c.config.Scheduler.Store.Task(taskID)
			if e != nil || task.PlanRevision != revision || task.Generation != generation {
				return r, store.ErrConflict
			}
		}
		return r, nil
	}
	if r.State == "unknown" {
		return r, ErrReconcile
	}
	c.mu.Lock()
	j := c.jobs[runID]
	c.mu.Unlock()
	if j == nil || j.run.Generation != generation || j.run.TaskID != taskID {
		return r, ErrReconcile
	}
	if revision > 0 {
		e = c.config.Scheduler.Store.CancelIntentAtRevision(runID, generation, j.run.Owner, revision)
	} else {
		e = c.config.Scheduler.Store.CancelIntent(runID, generation, j.run.Owner)
	}
	if e != nil {
		if errors.Is(e, store.ErrConflict) {
			return r, e
		}
		current, re := c.config.Scheduler.Store.Run(runID)
		if re == nil && current.Generation == generation && terminal(current.State) {
			if revision > 0 {
				task, te := c.config.Scheduler.Store.Task(taskID)
				if te != nil || task.PlanRevision != revision || task.Generation != generation {
					return current, store.ErrConflict
				}
			}
			return current, nil
		}
		return r, ErrIdentity
	}
	j.cancel()
	j.mu.Lock()
	h := j.handle
	j.mu.Unlock()
	if h != nil {
		_ = h.Cancel()
	}
	return c.config.Scheduler.Store.Run(runID)
}
func (c *Controller) Wait(ctx context.Context, runID string) (Completion, error) {
	if c == nil || ctx == nil {
		return Completion{}, ErrUnsupported
	}
	c.mu.Lock()
	j := c.jobs[runID]
	c.mu.Unlock()
	if j == nil {
		return Completion{}, ErrReconcile
	}
	select {
	case <-ctx.Done():
		return Completion{}, ctx.Err()
	case <-j.done:
		return j.completion, nil
	}
}

// Close stops accepting work, cancels only owned lifetimes, and waits for
// actual Handle completion/release processing. Timeout is not a stop proof.
func (c *Controller) Close(ctx context.Context) error {
	if c == nil || ctx == nil {
		return ErrUnsupported
	}
	c.mu.Lock()
	if !c.closed {
		c.closed = true
		c.cancel()
		go func() { c.wg.Wait(); close(c.closedDone) }()
	}
	c.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closedDone:
		return nil
	}
}

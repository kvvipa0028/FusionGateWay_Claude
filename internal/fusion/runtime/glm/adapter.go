package glm

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/fusion/policy"
	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

const NativeExecutableSHA256 = "6eab8333fe2121553100d8f40bfada384a3e989b94f947e18ba6677a6fcb41ea"

// Credential is a controller-only result from the private credential service.
// Identity must identify the exact frozen credential, not a mutable default.
type Credential struct {
	Identity string
	Key      string `json:"-"`
}

func (Credential) String() string   { return "GLM credential (redacted)" }
func (Credential) GoString() string { return "Credential(<redacted>)" }

// AdapterConfig is trusted server wiring, never an HTTP DTO. Scheduler's
// Inspector must independently admit route/quota/data/verification; Current
// must be a bounded current-identity check. Transport is single-send/admitted.
type AdapterConfig struct {
	Scheduler      policy.Scheduler
	Manager        *policy.Manager
	Executable     string
	LoadCredential func(context.Context, stageplan.ExecutionTarget) (Credential, error)
	Current        func(Binding) bool
	Transport      http.RoundTripper
}
type nativeObservation struct {
	generation int64
	outcome    Outcome
	text       string
}
type Adapter struct {
	config       AdapterConfig
	supervisor   *managed.Supervisor
	mu           sync.Mutex
	observations map[string]nativeObservation
}

func NewAdapter(c AdapterConfig) (*Adapter, error) {
	if c.Scheduler.Store == nil || c.Scheduler.Inspect == nil || c.Manager == nil || c.LoadCredential == nil || c.Current == nil || c.Transport == nil || !filepath.IsAbs(c.Executable) || filepath.Clean(c.Executable) != c.Executable || strings.ContainsRune(c.Executable, 0) {
		return nil, ErrUnverified
	}
	sup := managed.NewSupervisor(c.Scheduler.Store)
	c.Scheduler.VerifyStop = sup.VerifyStop
	return &Adapter{config: c, supervisor: sup, observations: map[string]nativeObservation{}}, nil
}
func (a *Adapter) Probe(ctx context.Context) (managed.Capabilities, error) {
	if a == nil || ctx.Err() != nil {
		return managed.Capabilities{}, ErrUnverified
	}
	h, e := managed.FileHash(a.config.Executable)
	if e != nil || h != NativeExecutableSHA256 {
		return managed.Capabilities{}, ErrIdentity
	}
	return a.supervisor.Probe(ctx)
}
func (*Adapter) Resume(context.Context, store.StageRun) (*managed.Handle, error) {
	return nil, ErrUnsupported
}
func (a *Adapter) VerifyStop(p policy.StopProof) bool { return a != nil && a.supervisor.VerifyStop(p) }
func (a *Adapter) Release(p policy.StopProof) error {
	if a == nil {
		return ErrUnverified
	}
	return a.config.Scheduler.Release(p)
}

// Start accepts only trusted private paths, prompt, timeout and requested write
// scope. The adapter generates all Native argv, identity, env, channel and
// outcome validation. Caller-provided launch controls are always refused.
func (a *Adapter) Start(ctx context.Context, r store.StageRun, in managed.Spec) (*managed.Handle, error) {
	if a == nil || ctx.Err() != nil || in.Executable != "" || in.ExecutableHash != "" || len(in.Args) != 0 || len(in.FixtureEnvironment) != 0 || in.NativeSessionID != "" || in.ClaudeChannel != nil || in.ValidateOutcome != nil || in.Timeout <= 0 || in.Timeout > 4*time.Minute || len(in.Input) == 0 || len(in.Input) > 64<<10 || !utf8.Valid(in.Input) || in.Writable && r.Role != stageplan.Implementation && r.Role != stageplan.Testing {
		return nil, ErrUnverified
	}
	if e := a.config.Scheduler.CheckPrepared(ctx, r); e != nil {
		return nil, e
	}
	reservation, e := a.config.Scheduler.Store.Reservation(r.ID)
	if e != nil || in.Writable != (reservation.WriteKey != "") {
		return nil, ErrUnverified
	}
	if _, e = a.Probe(ctx); e != nil {
		return nil, e
	}
	task, e := a.config.Scheduler.Store.Task(r.TaskID)
	if e != nil {
		return nil, ErrIdentity
	}
	budget, e := a.config.Scheduler.Store.Budget(r.TaskID)
	if e != nil || budget.MaxCalls <= budget.UsedCalls {
		return nil, ErrUnverified
	}
	var uuid [16]byte
	if _, e = rand.Read(uuid[:]); e != nil {
		return nil, ErrUnverified
	}
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	uuid[8] = (uuid[8] & 0x3f) | 0x80
	sid := fmt.Sprintf("%x-%x-%x-%x-%x", uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16])
	tools := []string{"Read"}
	if in.Writable {
		tools = append(tools, "Edit")
	}
	b := clone(Binding{RunID: r.ID, Generation: r.Generation, Role: r.Role, NativeSessionID: sid, Cwd: in.Workspace, Endpoint: Endpoint, Region: "CN", Target: r.Target, Tools: tools, MaxTurns: int64(budget.MaxCalls - budget.UsedCalls)})
	lifetime, cancel := context.WithTimeout(ctx, in.Timeout)
	current := func(got Binding) bool {
		if lifetime.Err() != nil {
			return false
		}
		active, e := a.config.Scheduler.Store.CheckActive(r.ID, r.Generation)
		return e == nil && active.State == "running" && active.Owner == r.Owner && active.TaskID == r.TaskID && active.Role == r.Role && active.Attempt == r.Attempt && active.PlanRevision == r.PlanRevision && equalTarget(active.Target, b.Target) && a.config.Current(clone(got))
	}
	observer, e := New(b, current)
	if e != nil {
		cancel()
		return nil, e
	}
	if !a.config.Current(clone(b)) {
		cancel()
		return nil, ErrIdentity
	}
	if b.Target.Effort.Value == nil || !knownEffort(*b.Target.Effort.Value) || b.Target.Effort.RequestedMode != stageplan.EffortExplicit && b.Target.Effort.RequestedMode != stageplan.EffortDefault {
		cancel()
		return nil, ErrUnverified
	}
	key, e := a.config.LoadCredential(lifetime, clone(b).Target)
	if e != nil || key.Identity != b.Target.CredentialIdentity {
		cancel()
		return nil, ErrIdentity
	}
	// Credential loading may take time. Repeat current admission before any
	// launch intent, secret delivery or externally executing process.
	if e = a.config.Scheduler.CheckPrepared(lifetime, r); e != nil {
		cancel()
		return nil, e
	}
	if !a.config.Current(clone(b)) {
		cancel()
		return nil, ErrIdentity
	}
	c := policy.Claims{TaskID: r.TaskID, RunID: r.ID, Role: r.Role, Attempt: r.Attempt, PlanRevision: r.PlanRevision, Generation: r.Generation, ProjectID: task.ProjectID, Audience: policy.ModelAudience}
	pending, e := a.config.Manager.PrepareModel(c, in.Timeout+20*time.Second)
	if e != nil {
		cancel()
		return nil, ErrUnverified
	}
	secret, e := pending.Secret()
	if e != nil {
		pending.Cancel()
		cancel()
		return nil, ErrUnverified
	}
	gate, e := NewCallGate(CallGateConfig{Binding: b, Manager: a.config.Manager, Claims: c, APIKey: key.Key, Current: current, Permit: a.config.Scheduler.Permit, Transport: a.config.Transport})
	if e != nil {
		pending.Cancel()
		cancel()
		return nil, e
	}
	channel, e := managed.NewClaudeChannel(gate, pending, b.Target)
	if e != nil {
		pending.Cancel()
		cancel()
		return nil, e
	}
	a.mu.Lock()
	_, duplicate := a.observations[r.ID]
	if duplicate || len(a.observations) >= 4096 {
		a.mu.Unlock()
		channel.Close()
		cancel()
		return nil, ErrUnverified
	}
	a.observations[r.ID] = nativeObservation{generation: r.Generation, outcome: Outcome{State: "execution_uncertain"}}
	a.mu.Unlock()
	args := []string{"--bare", "--restricted", "--strict-mcp-config", "--setting-sources", "", "--tools", strings.Join(tools, ","), "--allowedTools", strings.Join(tools, ","), "--disable-slash-commands", "--no-chrome", "--no-session-persistence", "--permission-mode", "dontAsk", "--model", b.Target.ResolvedModel, "--effort", *b.Target.Effort.Value, "--session-id", sid, "--system-prompt", "Execute only the assigned engineering stage in the approved working directory. Do not spawn agents or change models.", "-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages"}
	spec := managed.Spec{Executable: a.config.Executable, ExecutableHash: NativeExecutableSHA256, Args: args, Root: in.Root, Workspace: in.Workspace, Writable: in.Writable, Timeout: in.Timeout, Input: append([]byte(nil), in.Input...), NativeSessionID: sid, ClaudeChannel: channel, ValidateOutcome: func(raw []byte) bool {
		if bytes.Contains(raw, []byte(key.Key)) || bytes.Contains(raw, []byte(secret)) {
			return false
		}
		for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
			if _, e := observer.Event(b.RunID, b.Generation, line); e != nil {
				return false
			}
		}
		outcome, e := observer.Finish(0) // actual EOF and exit 0 established by Supervisor
		if e != nil || outcome.State != "succeeded" {
			return false
		}
		var result struct{ Result string }
		lines := bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
		if json.Unmarshal(lines[len(lines)-1], &result) != nil {
			return false
		}
		a.mu.Lock()
		a.observations[r.ID] = nativeObservation{generation: r.Generation, outcome: outcome, text: result.Result}
		a.mu.Unlock()
		return true
	}}
	h, e := a.supervisor.Start(lifetime, r, spec)
	if h == nil {
		channel.Close()
		pending.Cancel()
		cancel()
		return nil, e
	}
	// A known spawned activation failure returns its Handle with an error.
	// Keep ports and lifecycle until the supervisor's real wait/reap completes.
	go func() { h.Wait(context.Background()); cancel() }()
	return h, e
}
func equalTarget(a, b stageplan.ExecutionTarget) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

// Observation is untrusted Native output, not route/engineering acceptance.
// Read it only after Wait; false flags remain false and text may contain source.
func (a *Adapter) Observation(runID string, generation int64) (Outcome, string, error) {
	if a == nil {
		return Outcome{}, "", ErrUnverified
	}
	r, e := a.config.Scheduler.Store.Run(runID)
	if e != nil || r.Generation != generation {
		return Outcome{}, "", ErrIdentity
	}
	switch r.State {
	case "succeeded", "failed", "cancelled", "interrupted", "execution_uncertain":
	default:
		return Outcome{}, "", ErrUnverified
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	v, ok := a.observations[runID]
	if !ok || v.generation != generation {
		return Outcome{}, "", ErrIdentity
	}
	if r.State != "succeeded" {
		v.outcome.State = r.State
		v.text = ""
	}
	return v.outcome, v.text, nil
}

var _ managed.Adapter = (*Adapter)(nil)

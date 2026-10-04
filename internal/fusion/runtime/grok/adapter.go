package grok

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
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

// AdapterConfig is trusted controller wiring, never a request DTO. Inspector
// must independently admit the Native subscription route, private workspace,
// quota and verification. Forwarder owns the registered upstream credential;
// there is no ordinary API-key/proxy fallback provided by this package.
type AdapterConfig struct {
	Scheduler  policy.Scheduler
	Manager    *policy.Manager
	Executable string
	Current    func(GateBinding) bool
	Forwarder  NativeForwarder
}
type nativeObservation struct {
	generation int64
	outcome    Outcome
	text       string
	checkpoint *checkpointRecord
	handle     *managed.Handle
	released   policy.StopProof
}
type Adapter struct {
	config       AdapterConfig
	supervisor   *managed.Supervisor
	mu           sync.Mutex
	observations map[string]nativeObservation
}

func NewAdapter(c AdapterConfig) (*Adapter, error) {
	if c.Scheduler.Store == nil || c.Scheduler.Inspect == nil || c.Manager == nil || c.Current == nil || c.Forwarder == nil || !filepath.IsAbs(c.Executable) || filepath.Clean(c.Executable) != c.Executable || !utf8.ValidString(c.Executable) || hasNUL(c.Executable) {
		return nil, ErrUnverified
	}
	supervisor := managed.NewSupervisor(c.Scheduler.Store)
	c.Scheduler.VerifyStop = supervisor.VerifyStop
	return &Adapter{config: c, supervisor: supervisor, observations: map[string]nativeObservation{}}, nil
}
func (a *Adapter) Probe(ctx context.Context) (managed.Capabilities, error) {
	if a == nil || ctx.Err() != nil {
		return managed.Capabilities{}, ErrUnverified
	}
	hash, e := managed.FileHash(a.config.Executable)
	if e != nil || hash != NativeExecutableSHA256 {
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
	if e := a.config.Scheduler.Release(p); e != nil {
		return e
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	v, ok := a.observations[p.RunID]
	if ok && v.generation == p.Generation && v.handle != nil {
		v.released = p
		a.observations[p.RunID] = v
	}
	return nil
}

// Start accepts only private paths, a UTF-8 prompt and a bounded timeout. All
// executable/argv/env/session/channel/validator authority belongs to Adapter.
// This version supports single approved text read_file and readonly execution.
func (a *Adapter) Start(ctx context.Context, inputRun store.StageRun, in managed.Spec) (*managed.Handle, error) {
	return a.start(ctx, inputRun, in, nil)
}

type resumeRequest struct {
	archives *Archives
	ref      CheckpointRef
}

// ResumeCheckpoint is trusted controller wiring for a distinct prepared run.
// It accepts only a sealed checkpoint; ordinary Resume without scope/prompt is
// unsupported. Public task idempotency and route registration belong outside.
func (a *Adapter) ResumeCheckpoint(ctx context.Context, inputRun store.StageRun, in managed.Spec, archives *Archives, ref CheckpointRef) (*managed.Handle, error) {
	if archives == nil {
		return nil, ErrUnverified
	}
	return a.start(ctx, inputRun, in, &resumeRequest{archives: archives, ref: ref})
}

func (a *Adapter) start(ctx context.Context, inputRun store.StageRun, in managed.Spec, resume *resumeRequest) (*managed.Handle, error) {
	if !in.SourceCurrent() {
		return nil, ErrIdentity
	}
	if a == nil || ctx.Err() != nil || in.Executable != "" || in.ExecutableHash != "" || len(in.Args) != 0 || len(in.FixtureEnvironment) != 0 || in.NativeSessionID != "" || in.ClaudeChannel != nil || in.GrokChannel != nil || in.CodexChannel != nil || in.ValidateOutcome != nil || in.Timeout <= 0 || in.Timeout > 4*time.Minute || len(in.Input) == 0 || len(in.Input) > 64<<10 || !utf8.Valid(in.Input) || bytes.IndexByte(in.Input, 0) >= 0 {
		return nil, ErrUnverified
	}
	if in.Writable {
		return nil, ErrUnsupported
	}
	if workspace.PrivateState(in.Root) != nil || workspace.PrivateState(in.Workspace) != nil || in.Root == in.Workspace || strings.HasPrefix(in.Root, in.Workspace+"/") || strings.HasPrefix(in.Workspace, in.Root+"/") {
		return nil, ErrUnverified
	}
	var resumeRoot archiveIdentity
	if resume != nil {
		info, e := os.Stat(in.Root)
		var ok bool
		if e == nil {
			resumeRoot, ok = archiveIdentityOf(info)
		}
		if e != nil || !ok {
			return nil, ErrIdentity
		}
	}
	resumeRootCurrent := func() bool {
		if resume == nil {
			return true
		}
		if workspace.PrivateState(in.Root) != nil {
			return false
		}
		info, e := os.Stat(in.Root)
		if e != nil {
			return false
		}
		id, ok := archiveIdentityOf(info)
		return ok && id == resumeRoot
	}
	// Freeze mutable input before invoking any trusted service callback.
	promptInput := append([]byte(nil), in.Input...)
	if e := a.config.Scheduler.CheckPrepared(ctx, inputRun); e != nil {
		return nil, e
	}
	// Read the authoritative run again, instead of retaining mutable caller targets.
	r, e := a.config.Scheduler.Store.Run(inputRun.ID)
	if e != nil {
		return nil, ErrIdentity
	}
	reservation, e := a.config.Scheduler.Store.Reservation(r.ID)
	if e != nil || reservation.WriteKey != "" {
		return nil, ErrUnverified
	}
	if _, e := a.Probe(ctx); e != nil {
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
	if r.Target.Effort.RequestedMode != stageplan.EffortNone || r.Target.Effort.Value != nil {
		return nil, ErrUnsupported
	}
	var restored *restoredCheckpoint
	var sid string
	if resume != nil {
		restored, e = resume.archives.prepareRestore(ctx, resume.ref, &a.config.Scheduler, r, in.Workspace)
		if e != nil {
			return nil, e
		}
		sid = restored.manifest.Run.NativeSessionID
	} else {
		var id [16]byte
		if _, e := rand.Read(id[:]); e != nil {
			return nil, ErrUnverified
		}
		id[6] = (id[6] & 0x0f) | 0x40
		id[8] = (id[8] & 0x3f) | 0x80
		sid = fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
	}
	turns := int64(budget.MaxCalls - budget.UsedCalls)
	if turns > 1000 {
		turns = 1000
	}
	binding := copyGate(GateBinding{Observer: Binding{RunID: r.ID, Generation: r.Generation, Role: r.Role, NativeSessionID: sid, Cwd: in.Workspace, RuntimeVersion: CLIVersion, ExecutableSHA256: NativeExecutableSHA256, Model: r.Target.RequestedModel, Tools: []string{"read_file"}, MaxTurns: turns}, Target: r.Target})
	lifetime, cancel := context.WithTimeout(ctx, in.Timeout)
	current := func(got GateBinding) bool {
		if lifetime.Err() != nil || !in.SourceCurrent() || !resumeRootCurrent() || resume != nil && !resume.archives.live() {
			return false
		}
		active, e := a.config.Scheduler.Store.CheckActive(r.ID, r.Generation)
		x, _ := json.Marshal(got)
		y, _ := json.Marshal(binding)
		return e == nil && (active.State == "starting" || active.State == "running") && active.Owner == r.Owner && active.TaskID == r.TaskID && active.Role == r.Role && active.Attempt == r.Attempt && active.PlanRevision == r.PlanRevision && sameTarget(active.Target, binding.Target) && bytes.Equal(x, y) && a.config.Current(copyGate(got)) && in.SourceCurrent() && resumeRootCurrent()
	}
	if !current(copyGate(binding)) {
		cancel()
		return nil, ErrIdentity
	}
	readCurrent := func(got Binding) bool {
		return current(GateBinding{Observer: clone(got), Target: copyGate(binding).Target})
	}
	var read *ReadTools
	if restored != nil {
		read, e = restored.readTools(binding.Observer, readCurrent, nil)
	} else {
		read, e = NewReadTools(binding.Observer, readCurrent)
	}
	if e != nil {
		cancel()
		return nil, e
	}
	cleanup := func() { read.Close(); cancel() }
	observer, e := NewReadSession(binding.Observer, func(got Binding) bool {
		return current(GateBinding{Observer: clone(got), Target: copyGate(binding).Target})
	}, read)
	if e != nil {
		cleanup()
		return nil, e
	}
	claims := policy.Claims{TaskID: r.TaskID, RunID: r.ID, Role: r.Role, Attempt: r.Attempt, PlanRevision: r.PlanRevision, Generation: r.Generation, ProjectID: task.ProjectID, Audience: policy.ModelAudience}
	pending, e := a.config.Manager.PrepareModel(claims, in.Timeout+20*time.Second)
	if e != nil {
		cleanup()
		return nil, ErrUnverified
	}
	secret, e := pending.Secret()
	if e != nil {
		pending.Cancel()
		cleanup()
		return nil, ErrUnverified
	}
	if read.containsPrivate([]byte(secret)) {
		pending.Cancel()
		cleanup()
		return nil, ErrUnverified
	}
	gate, e := NewCallGate(CallGateConfig{Binding: binding, ReadTools: read, Manager: a.config.Manager, Claims: claims, Current: current, Permit: a.config.Scheduler.Permit, Forwarder: a.config.Forwarder})
	if e != nil {
		pending.Cancel()
		cleanup()
		return nil, e
	}
	var channel *managed.GrokChannel
	if restored != nil {
		channel, e = managed.NewGrokResumeChannel(gate, pending, binding.Target, sid, in.Workspace, restored.files)
	} else {
		channel, e = managed.NewGrokChannel(gate, pending, binding.Target)
	}
	if e != nil {
		pending.Cancel()
		cleanup()
		return nil, e
	}
	fail := func(e error) (*managed.Handle, error) { channel.Close(); pending.Cancel(); cleanup(); return nil, e }
	// Recheck after all preparation, immediately before publishing private prompt
	// or launch intent. Later drift is rechecked at each Native HTTP/event boundary.
	if e := a.config.Scheduler.CheckPrepared(lifetime, r); e != nil {
		return fail(e)
	}
	if !current(copyGate(binding)) {
		return fail(ErrIdentity)
	}
	a.mu.Lock()
	_, duplicate := a.observations[r.ID]
	if duplicate || len(a.observations) >= 4096 {
		a.mu.Unlock()
		return fail(ErrUnverified)
	}
	cwdIdentity, ok := archiveIdentityOf(read.rootInfo)
	if !ok {
		a.mu.Unlock()
		return fail(ErrIdentity)
	}
	rootInfo, rootErr := os.Stat(in.Root)
	var rootIdentity archiveIdentity
	if rootErr == nil {
		rootIdentity, ok = archiveIdentityOf(rootInfo)
	}
	if rootErr != nil || !ok {
		a.mu.Unlock()
		return fail(ErrIdentity)
	}
	a.observations[r.ID] = nativeObservation{generation: r.Generation, outcome: Outcome{State: "execution_uncertain", SessionID: sid}, checkpoint: &checkpointRecord{run: r, projectID: task.ProjectID, root: in.Root, cwd: in.Workspace, owner: r.Owner, cwdIdentity: cwdIdentity, rootIdentity: rootIdentity, markers: [][]byte{[]byte(secret)}}}
	a.mu.Unlock()
	if restored != nil {
		if e = resume.archives.recordRestore(lifetime, restored, in.Root, promptInput); e != nil {
			return fail(e)
		}
	}
	prompt := filepath.Join(in.Root, "grok-prompt.txt")
	file, e := os.OpenFile(prompt, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return fail(ErrUnverified)
	}
	_, writeErr := file.Write(promptInput)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return fail(ErrUnverified)
	}
	args := []string{"--prompt-file", prompt, "--model", "fusion", "--output-format", "streaming-json", "--permission-mode", "dontAsk", "--no-subagents", "--max-turns", fmt.Sprint(turns), "--tools", "Read", "--disallowed-tools", "search_tool,use_tool", "--disable-web-search", "--no-auto-update", "--cwd", binding.Observer.Cwd}
	if restored != nil {
		args = append(args, "--resume", sid)
	} else {
		args = append(args, "--session-id", sid)
	}
	spec := managed.Spec{Source: in.Source, Executable: a.config.Executable, ExecutableHash: NativeExecutableSHA256, Args: args, Root: in.Root, Workspace: in.Workspace, Timeout: in.Timeout, NativeSessionID: sid, GrokChannel: channel, ValidateOutcome: func(raw []byte) bool {
		if bytes.Contains(raw, []byte(secret)) {
			return false
		}
		var text strings.Builder
		for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
			if privateJSON(line, []byte(secret)) || observer.Event(r.ID, r.Generation, line) != nil {
				return false
			}
			var event struct {
				Type string
				Data string
			}
			if decode(line, &event) != nil {
				return false
			}
			if event.Type == "text" {
				text.WriteString(event.Data)
				if text.Len() > 64<<10 {
					return false
				}
			}
		}
		outcome, e := observer.Finish(0)
		if e != nil || outcome.State != "succeeded" {
			return false
		}
		callContext, e := a.config.Manager.WithStage(lifetime, secret, claims)
		if e != nil || !gate.Healthy(callContext) {
			return false
		}
		reads, e := read.checkpointReads()
		if e != nil {
			return false
		}
		a.mu.Lock()
		v := a.observations[r.ID]
		v.outcome, v.text = outcome, text.String()
		v.checkpoint.reads = reads
		a.observations[r.ID] = v
		a.mu.Unlock()
		return true
	}}
	if e := a.config.Scheduler.CheckPrepared(lifetime, r); e != nil {
		return fail(e)
	}
	if !current(copyGate(binding)) {
		return fail(ErrIdentity)
	}
	h, e := a.supervisor.Start(lifetime, r, spec)
	if h == nil {
		return fail(e)
	}
	a.mu.Lock()
	v := a.observations[r.ID]
	v.handle = h
	a.observations[r.ID] = v
	a.mu.Unlock()
	// Preserve a known Handle with an activation error until actual wait/reap.
	go func() { h.Wait(context.Background()); pending.Cancel(); cleanup() }()
	return h, e
}
func sameTarget(a, b stageplan.ExecutionTarget) bool {
	x, e := json.Marshal(a)
	y, f := json.Marshal(b)
	return e == nil && f == nil && bytes.Equal(x, y)
}

// Observation contains untrusted Native model text; it is not route admission
// or engineering acceptance. Read only after terminal state; failures have no
// success text. Caller must enforce project/task access before exposing it.
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

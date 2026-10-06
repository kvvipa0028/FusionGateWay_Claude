// Package codexadapter connects the pinned protocol to the managed lifecycle.
// It is separate from codex because runtime's private channel imports codex.
package codexadapter

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/fusion/policy"
	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/runtime/codex"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
	"github.com/yetone/magpie/internal/fusion/workspace"
)

// AdapterConfig is trusted controller wiring, never a request DTO. Identity
// reads the independent registry epoch, not the Native null-account response.
// Inspect/Current/Forwarder must prove the actual subscription/data/quota route.
type AdapterConfig struct {
	Scheduler  policy.Scheduler
	Manager    *policy.Manager
	Executable string
	Identity   func(stageplan.ExecutionTarget) codex.Identity
	Current    func(codex.Binding) bool
	Forwarder  codex.NativeForwarder
}

type Outcome struct {
	State     string `json:"state"`
	SessionID string `json:"session_id"`
	ThreadID  string `json:"thread_id"`
	TurnID    string `json:"turn_id"`
}
type observation struct {
	generation int64
	outcome    Outcome
	text       string
}
type Adapter struct {
	config       AdapterConfig
	supervisor   *managed.Supervisor
	mu           sync.Mutex
	observations map[string]observation
}

func (*Adapter) String() string               { return "managed Codex adapter (redacted)" }
func (*Adapter) GoString() string             { return "codexadapter.Adapter(<redacted>)" }
func (*Adapter) MarshalJSON() ([]byte, error) { return nil, codex.ErrUnverified }

func NewAdapter(c AdapterConfig) (*Adapter, error) {
	if c.Scheduler.Store == nil || c.Scheduler.Inspect == nil || c.Manager == nil || c.Identity == nil || c.Current == nil || c.Forwarder == nil || !filepath.IsAbs(c.Executable) || filepath.Clean(c.Executable) != c.Executable || !utf8.ValidString(c.Executable) || strings.ContainsRune(c.Executable, 0) {
		return nil, codex.ErrUnverified
	}
	sup := managed.NewSupervisor(c.Scheduler.Store)
	c.Scheduler.VerifyStop = sup.VerifyStop
	return &Adapter{config: c, supervisor: sup, observations: map[string]observation{}}, nil
}
func (a *Adapter) Probe(ctx context.Context) (managed.Capabilities, error) {
	if a == nil || ctx.Err() != nil {
		return managed.Capabilities{}, codex.ErrUnverified
	}
	if hash, e := managed.FileHash(a.config.Executable); e != nil || hash != managed.CodexExecutableSHA256 {
		return managed.Capabilities{}, codex.ErrIdentity
	}
	return a.supervisor.Probe(ctx)
}
func (*Adapter) Resume(context.Context, store.StageRun) (*managed.Handle, error) {
	return nil, managed.ErrUnsupported
}
func (a *Adapter) VerifyStop(p policy.StopProof) bool {
	return a != nil && a.supervisor.VerifyStop(p)
}
func (a *Adapter) Release(p policy.StopProof) error {
	if a == nil {
		return codex.ErrUnverified
	}
	return a.config.Scheduler.Release(p)
}

func cloneTarget(t stageplan.ExecutionTarget) stageplan.ExecutionTarget {
	if t.Effort.Value != nil {
		v := *t.Effort.Value
		t.Effort.Value = &v
	}
	if t.PluginVersion != nil {
		v := *t.PluginVersion
		t.PluginVersion = &v
	}
	if t.UpstreamReportedModel != nil {
		v := *t.UpstreamReportedModel
		t.UpstreamReportedModel = &v
	}
	t.Capabilities = append([]string(nil), t.Capabilities...)
	return t
}
func cloneBinding(b codex.Binding) codex.Binding { b.Target = cloneTarget(b.Target); return b }
func same(a, b any) bool {
	x, e := json.Marshal(a)
	y, f := json.Marshal(b)
	return e == nil && f == nil && bytes.Equal(x, y)
}

func validateSpec(ctx context.Context, in managed.Spec) error {
	if ctx.Err() != nil || !in.Source.Present() || !in.SourceCurrent() || !in.ValidWriteScope() || in.Executable != "" || in.ExecutableHash != "" || len(in.Args) != 0 || len(in.FixtureEnvironment) != 0 || in.NativeSessionID != "" || in.ClaudeChannel != nil || in.GrokChannel != nil || in.CodexChannel != nil || in.ValidateOutcome != nil || in.Timeout <= 0 || in.Timeout > 4*time.Minute || len(in.Input) == 0 || len(in.Input) > 64<<10 || !utf8.Valid(in.Input) || bytes.IndexByte(in.Input, 0) >= 0 {
		return codex.ErrUnverified
	}
	if workspace.PrivateState(in.Root) != nil || workspace.PrivateState(in.Workspace) != nil || in.Root == in.Workspace || strings.HasPrefix(in.Root, in.Workspace+"/") || strings.HasPrefix(in.Workspace, in.Root+"/") {
		return codex.ErrUnverified
	}
	encoded, e := json.Marshal(string(in.Input))
	if e != nil || len(encoded) > 32<<10 {
		return codex.ErrUnverified
	}
	return nil
}

// ValidateLaunch performs no RPC, grant creation, budget spend or startup. It
// supplies adapter-specific preflight to Controller before intent. Current
// route/permission/quota inspection and post-intent checks remain mandatory.
func (a *Adapter) ValidateLaunch(ctx context.Context, role stageplan.Role, target stageplan.ExecutionTarget, in managed.Spec) error {
	if a == nil {
		return codex.ErrUnverified
	}
	target = cloneTarget(target)
	in.Input = append([]byte(nil), in.Input...)
	if e := validateSpec(ctx, in); e != nil {
		return e
	}
	if e := codex.ValidateGatewayTarget(target); e != nil {
		return e
	}
	known := false
	for _, r := range stageplan.AllRoles() {
		known = known || role == r
	}
	if !known {
		return codex.ErrUnverified
	}
	id := a.config.Identity(cloneTarget(target))
	if id.Generation < 1 || id.Account != target.Account || id.Workspace != target.Workspace || id.CredentialIdentity != target.CredentialIdentity {
		return codex.ErrIdentity
	}
	if e := validateSpec(ctx, in); e != nil {
		return e
	}
	if a.config.Identity(cloneTarget(target)) != id {
		return codex.ErrIdentity
	}
	return validateSpec(ctx, in)
}

// Start repeats preflight after durable intent, then checks the reservation.
// This version is readonly/tool-free; roles do not grant write authority.
func (a *Adapter) Start(ctx context.Context, inputRun store.StageRun, in managed.Spec) (*managed.Handle, error) {
	if a == nil {
		return nil, codex.ErrUnverified
	}
	// Freeze caller slices/pointers before any trusted callback can run.
	in.Input = append([]byte(nil), in.Input...)
	inputRun.Target = cloneTarget(inputRun.Target)
	if e := a.ValidateLaunch(ctx, inputRun.Role, inputRun.Target, in); e != nil {
		return nil, e
	}
	rootInfo, e := os.Lstat(in.Root)
	if e != nil {
		return nil, codex.ErrIdentity
	}
	prompt := string(in.Input)
	if e := a.config.Scheduler.CheckPrepared(ctx, inputRun); e != nil {
		return nil, e
	}
	r, e := a.config.Scheduler.Store.Run(inputRun.ID)
	if e != nil {
		return nil, codex.ErrIdentity
	}
	reservation, e := a.config.Scheduler.Store.Reservation(r.ID)
	// Write authority and write intent must agree exactly: a readonly run
	// never carries a write key, and a writer run must have been granted one.
	if e != nil || in.Writable && reservation.WriteKey == "" || !in.Writable && reservation.WriteKey != "" {
		return nil, codex.ErrUnverified
	}
	if _, e := a.Probe(ctx); e != nil {
		return nil, e
	}
	task, e := a.config.Scheduler.Store.Task(r.TaskID)
	if e != nil {
		return nil, codex.ErrIdentity
	}
	var session [16]byte
	if _, e := rand.Read(session[:]); e != nil {
		return nil, codex.ErrUnverified
	}
	sid := "codex-" + hex.EncodeToString(session[:])
	// The native inner sandbox matches resolved vnode paths (a workspace under
	// /var is /private/var to the kernel), so hand it the resolved cwd.
	bindingCwd, evalErr := filepath.EvalSymlinks(in.Workspace)
	if evalErr != nil {
		bindingCwd = in.Workspace
	}
	binding := codex.Binding{Scope: codex.Scope{RunID: r.ID, Generation: r.Generation, Role: r.Role}, Identity: a.config.Identity(cloneTarget(r.Target)), Target: cloneTarget(r.Target), Cwd: bindingCwd, CodexHome: filepath.Join(in.Root, "config", "codex")}
	lifetime, cancel := context.WithTimeout(ctx, in.Timeout)
	pathsCurrent := func() bool {
		info, e := os.Lstat(in.Root)
		return e == nil && os.SameFile(rootInfo, info) && workspace.PrivateState(in.Root) == nil && in.SourceCurrent()
	}
	current := func(got codex.Binding) bool {
		if lifetime.Err() != nil || !pathsCurrent() || !same(binding, got) || a.config.Identity(cloneTarget(binding.Target)) != binding.Identity {
			return false
		}
		active := func() bool {
			x, e := a.config.Scheduler.Store.CheckActive(r.ID, r.Generation)
			return e == nil && (x.State == "starting" || x.State == "running") && x.Owner == r.Owner && x.TaskID == r.TaskID && x.Role == r.Role && x.Attempt == r.Attempt && x.PlanRevision == r.PlanRevision && same(x.Target, binding.Target)
		}
		return active() && a.config.Current(cloneBinding(got)) && active() && pathsCurrent() && a.config.Identity(cloneTarget(binding.Target)) == binding.Identity
	}
	if !current(cloneBinding(binding)) {
		cancel()
		return nil, codex.ErrIdentity
	}
	claims := policy.Claims{TaskID: r.TaskID, RunID: r.ID, Role: r.Role, Attempt: r.Attempt, PlanRevision: r.PlanRevision, Generation: r.Generation, ProjectID: task.ProjectID, Audience: policy.ModelAudience}
	pending, e := a.config.Manager.PrepareModel(claims, in.Timeout+20*time.Second)
	if e != nil {
		cancel()
		return nil, codex.ErrUnverified
	}
	secret, e := pending.Secret()
	if e != nil || strings.Contains(prompt, secret) {
		pending.Cancel()
		cancel()
		return nil, codex.ErrUnverified
	}
	gate, e := codex.NewCallGate(codex.CallGateConfig{Binding: binding, Manager: a.config.Manager, Claims: claims, Current: current, Permit: a.config.Scheduler.Permit, Forwarder: a.config.Forwarder})
	if e != nil {
		pending.Cancel()
		cancel()
		return nil, e
	}
	driver := func(ctx context.Context, stream io.ReadWriteCloser) error {
		peer, e := codex.NewStdioPeer(stream, func() bool { return current(cloneBinding(binding)) })
		if e != nil {
			return e
		}
		defer peer.Close()
		client, e := codex.NewGateway(peer, binding, func(s codex.Scope) bool { return s == binding.Scope && current(cloneBinding(binding)) }, func() codex.Identity { return a.config.Identity(cloneTarget(binding.Target)) }, func() bool { return current(cloneBinding(binding)) })
		if e != nil {
			return e
		}
		if e = client.Initialize(ctx); e != nil {
			return e
		}
		if e = client.ReadAccount(ctx); e != nil {
			return e
		}
		out := Outcome{State: "execution_uncertain", SessionID: sid}
		if out.ThreadID, e = client.StartThread(ctx); e != nil {
			return e
		}
		if out.TurnID, e = client.StartTurn(ctx, prompt); e != nil {
			return e
		}
		var text strings.Builder
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case raw, ok := <-peer.Notifications():
				if !ok {
					return codex.ErrNative
				}
				if privateJSON(raw, secret) {
					return codex.ErrUnverified
				}
				state, e := client.Event(binding.Scope, raw)
				if e != nil {
					return e
				}
				// Only fully validated completed agent items supply text. Deltas,
				// reasoning, error bodies and observations never become output.
				var event struct {
					Method string
					Params struct {
						Item struct {
							Type, Text string
							Phase      *string
						}
					}
				}
				if json.Unmarshal(raw, &event) != nil {
					return codex.ErrProtocol
				}
				if event.Method == "item/completed" && event.Params.Item.Type == "agentMessage" && (event.Params.Item.Phase == nil || *event.Params.Item.Phase == "final_answer") {
					text.WriteString(event.Params.Item.Text)
					if text.Len() > 32<<10 {
						return codex.ErrUnverified
					}
				}
				if state == "succeeded" || state == "failed" || state == "interrupted" {
					out.State = state
					a.mu.Lock()
					v := a.observations[r.ID]
					v.outcome = out
					if state == "succeeded" {
						v.text = text.String()
					}
					a.observations[r.ID] = v
					a.mu.Unlock()
					return nil
				}
			}
		}
	}
	channel, e := managed.NewCodexHTTPChannel(r, in.Root, in.Workspace, sid, driver, func() bool { return current(cloneBinding(binding)) }, gate, pending)
	if e != nil {
		pending.Cancel()
		cancel()
		return nil, e
	}
	fail := func(e error) (*managed.Handle, error) {
		channel.Close()
		pending.Cancel()
		cancel()
		return nil, e
	}
	if e := a.config.Scheduler.CheckPrepared(lifetime, r); e != nil {
		return fail(e)
	}
	if !current(cloneBinding(binding)) {
		return fail(codex.ErrIdentity)
	}
	a.mu.Lock()
	_, duplicate := a.observations[r.ID]
	if duplicate || len(a.observations) >= 4096 {
		a.mu.Unlock()
		return fail(codex.ErrUnverified)
	}
	a.observations[r.ID] = observation{generation: r.Generation, outcome: Outcome{State: "execution_uncertain"}}
	a.mu.Unlock()
	spec := managed.Spec{Source: in.Source, Executable: a.config.Executable, ExecutableHash: managed.CodexExecutableSHA256, Args: []string{"app-server", "--listen", "stdio://", "--strict-config"}, Root: in.Root, Workspace: in.Workspace, Timeout: in.Timeout, NativeSessionID: sid, Writable: in.Writable, WritePaths: append([]string(nil), in.WritePaths...), CodexChannel: channel, ValidateOutcome: func(raw []byte) bool {
		if !safeOutput(raw, secret) || !current(cloneBinding(binding)) {
			return false
		}
		callCtx, e := a.config.Manager.WithStage(lifetime, secret, claims)
		if e != nil || !gate.Healthy(callCtx) || !current(cloneBinding(binding)) {
			return false
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.observations[r.ID].outcome.State == "succeeded"
	}}
	if e := a.config.Scheduler.CheckPrepared(lifetime, r); e != nil {
		return fail(e)
	}
	if !current(cloneBinding(binding)) {
		return fail(codex.ErrIdentity)
	}
	h, e := a.supervisor.Start(lifetime, r, spec)
	if h == nil {
		return fail(e)
	}
	// An activation error with a known Handle still needs actual Wait and proof.
	go func() { h.Wait(context.Background()); channel.Close(); pending.Cancel(); cancel() }()
	return h, e
}

// Supervisor captures native stdout even when the private driver owns it.
// This scans for private material; protocol/terminal authority remains with
// the typed driver and gate, never this JSON confidentiality projection.
func safeOutput(raw []byte, marker string) bool {
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
		if privateJSON(line, marker) {
			return false
		}
	}
	return true
}

func privateJSON(raw []byte, marker string) bool {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return true
	}
	var walk func(any) bool
	walk = func(v any) bool {
		switch x := v.(type) {
		case string:
			return marker != "" && strings.Contains(x, marker)
		case []any:
			for _, e := range x {
				if walk(e) {
					return true
				}
			}
		case map[string]any:
			for k, e := range x {
				if walk(k) || walk(e) {
					return true
				}
			}
		}
		return false
	}
	return walk(v)
}

// Observation is untrusted model output, not engineering acceptance. The
// caller must enforce project/task access. Failed stages expose no success text.
func (a *Adapter) Observation(id string, generation int64) (Outcome, string, error) {
	if a == nil {
		return Outcome{}, "", codex.ErrUnverified
	}
	r, e := a.config.Scheduler.Store.Run(id)
	if e != nil || r.Generation != generation {
		return Outcome{}, "", codex.ErrIdentity
	}
	switch r.State {
	case "succeeded", "failed", "cancelled", "interrupted", "execution_uncertain":
	default:
		return Outcome{}, "", codex.ErrUnverified
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	v, ok := a.observations[id]
	if !ok || v.generation != generation {
		return Outcome{}, "", codex.ErrIdentity
	}
	if r.State != "succeeded" {
		v.outcome.State = r.State
		v.text = ""
	}
	return v.outcome, v.text, nil
}

var _ managed.Adapter = (*Adapter)(nil)

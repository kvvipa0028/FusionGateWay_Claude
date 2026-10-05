package runtime

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/store"
)

type Supervisor struct {
	store    *store.Store
	mu       sync.Mutex
	launches map[string]*Handle
	proofs   map[string]policy.StopProof
}
type Handle struct {
	supervisor         *Supervisor
	run                store.StageRun
	spec               Spec
	channel            *modelChannel
	codexStream        *codexStream
	codexCancel        context.CancelFunc
	codexResult        chan error
	cmd                *exec.Cmd
	identity           Identity
	nonce, profileHash string
	mu                 sync.Mutex
	cancelRequested    bool
	result             Result
	done               chan struct{}
	cancel             chan struct{}
	events             chan Event
	seq                int64
	output, stderr     boundedOutput
}
type boundedOutput struct {
	mu       sync.Mutex
	data     []byte
	overflow bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.data)+len(p) > 64<<10 {
		b.overflow = true
		return 0, ErrLaunch
	}
	b.data = append(b.data, p...)
	return len(p), nil
}
func (b *boundedOutput) snapshot() ([]byte, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.data...), b.overflow
}
func NewSupervisor(s *store.Store) *Supervisor {
	return &Supervisor{store: s, launches: map[string]*Handle{}, proofs: map[string]policy.StopProof{}}
}
func (s *Supervisor) Probe(ctx context.Context) (Capabilities, error) {
	if ctx.Err() != nil {
		return Capabilities{}, ctx.Err()
	}
	if !platformAvailable() {
		return Capabilities{}, ErrUnsupported
	}
	return Capabilities{Probe: true, Start: true, Events: true, Cancel: true}, nil
}
func (s *Supervisor) Resume(context.Context, store.StageRun) (*Handle, error) {
	return nil, ErrUnsupported
}

// Start consumes an already committed reservation. Spec is assembled by a
// trusted adapter, never decoded from an HTTP request. Child processes remain
// denied; a typed model channel adds only its controller-owned loopback port.
func (s *Supervisor) Start(ctx context.Context, r store.StageRun, in Spec) (*Handle, error) {
	if s == nil || s.store == nil || ctx.Err() != nil || in.ValidateOutcome == nil || in.Timeout <= 0 || in.Timeout > 10*time.Minute || in.NativeSessionID == "" || len(in.Input) > 64<<10 || !filepath.IsAbs(in.Executable) || strings.ContainsRune(in.NativeSessionID, 0) {
		return nil, ErrLaunch
	}
	if !in.SourceCurrent() || !in.ValidWriteScope() {
		return nil, ErrLaunch
	}
	channel, e := in.modelChannel()
	if e != nil {
		return nil, ErrLaunch
	}
	if in.GrokChannel != nil && in.ExecutableHash != GrokExecutableSHA256 {
		return nil, ErrLaunch
	}
	current, e := s.store.CheckActive(r.ID, r.Generation)
	if e != nil || current.State != "starting" || current.Owner != r.Owner || current.TaskID != r.TaskID || current.PlanRevision != r.PlanRevision {
		return nil, ErrLaunch
	}
	reservation, e := s.store.Reservation(r.ID)
	if e != nil || in.Writable && reservation.WriteKey == "" {
		return nil, ErrLaunch
	}
	if c := channel; c != nil {
		task, e := s.store.Task(current.TaskID)
		inputTarget, _ := json.Marshal(r.Target)
		storedTarget, _ := json.Marshal(current.Target)
		if e != nil || current.Role != r.Role || current.Attempt != r.Attempt || string(inputTarget) != string(storedTarget) || !c.acquire(current, task.ProjectID, in.Timeout) {
			return nil, ErrLaunch
		}
	}
	owned := false
	if c := in.CodexChannel; c != nil {
		inputTarget, _ := json.Marshal(r.Target)
		storedTarget, _ := json.Marshal(current.Target)
		if current.Role != r.Role || current.Attempt != r.Attempt || string(inputTarget) != string(storedTarget) || !c.acquire(current, in) {
			channel.release()
			return nil, ErrLaunch
		}
	}
	defer func() {
		if !owned {
			channel.release()
			in.CodexChannel.release()
		}
	}()
	actual, e := FileHash(in.Executable)
	if e != nil || actual != in.ExecutableHash {
		return nil, ErrLaunch
	}
	exe, e := filepath.EvalSymlinks(in.Executable)
	if e != nil || exe != in.Executable {
		return nil, ErrLaunch
	}
	info, e := os.Lstat(exe)
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 {
		return nil, ErrLaunch
	}
	// Copy all mutable caller-owned arguments and input before starting.
	spec := in
	spec.Args = append([]string(nil), in.Args...)
	spec.Input = append([]byte(nil), in.Input...)
	spec.WritePaths = append([]string(nil), in.WritePaths...)
	spec.FixtureEnvironment = map[string]string{}
	for k, v := range in.FixtureEnvironment {
		spec.FixtureEnvironment[k] = v
	}
	if len(spec.Args) > 128 {
		return nil, ErrLaunch
	}
	for _, v := range spec.Args {
		if len(v) > 8192 || strings.ContainsRune(v, 0) {
			return nil, ErrLaunch
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.launches[r.ID]; ok || len(s.launches) >= 4096 {
		return nil, ErrLaunch
	}
	profile, env, e := sandbox(spec)
	if e != nil {
		return nil, e
	}
	var nonce [16]byte
	if _, e = rand.Read(nonce[:]); e != nil {
		return nil, ErrLaunch
	}
	h := &Handle{supervisor: s, run: current, spec: spec, channel: channel, nonce: hex.EncodeToString(nonce[:]), profileHash: hashBytes([]byte(profile)), done: make(chan struct{}), cancel: make(chan struct{}, 1), events: make(chan Event, 16)}
	if !spec.SourceCurrent() {
		return nil, ErrLaunch
	}
	// O_EXCL + fsync intent before spawning. A controller crash in the spawn
	// window remains unknown; absence of a PID never authorizes automatic replay.
	if e = h.journal("intent"); e != nil {
		return nil, ErrLaunch
	}
	cmd := platformCommand(profile, spec)
	cmd.Dir = spec.Workspace
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(spec.Input)
	cmd.Stdout = &h.output
	cmd.Stderr = &h.stderr
	if c := spec.CodexChannel; c != nil {
		stream, err := newCodexStream(func() bool {
			live, err := s.store.CheckActive(current.ID, current.Generation)
			return err == nil && live.Owner == current.Owner && spec.SourceCurrent() && c.current()
		}, &h.output)
		if err != nil {
			return nil, ErrLaunch
		}
		h.codexStream = stream
		cmd.Stdin, cmd.Stdout = stream.childRead, stream.childWrite
		defer func() {
			if !owned {
				stream.Close()
			}
		}()
	}
	h.cmd = cmd
	if !spec.SourceCurrent() {
		return nil, ErrLaunch
	}
	s.launches[r.ID] = h
	if e = cmd.Start(); e != nil {
		h.result = Result{State: "launch_unknown", ExitCode: -1}
		close(h.done)
		close(h.events)
		return nil, ErrLaunch
	}
	if h.codexStream != nil {
		h.codexStream.closeChildEnds()
	}
	h.identity, e = processIdentity(cmd.Process.Pid)
	if e != nil {
		cmd.Process.Kill()
		cmd.Wait()
		h.result = Result{State: "execution_uncertain", ExitCode: -1}
		close(h.done)
		close(h.events)
		return nil, ErrIdentity
	}
	if e = h.journal("spawned"); e != nil {
		signalProcess(h.identity, syscall.SIGKILL)
		cmd.Wait()
		h.result = Result{State: "execution_uncertain", ExitCode: -1}
		close(h.done)
		close(h.events)
		return nil, ErrLaunch
	}
	if e = s.store.ConfirmStarted(r.ID, r.Generation, r.Owner, spec.NativeSessionID); e != nil {
		signalProcess(h.identity, syscall.SIGKILL)
		cmd.Wait()
		h.result = Result{State: "execution_uncertain", ExitCode: -1}
		close(h.done)
		close(h.events)
		return nil, ErrLaunch
	}
	h.emit("started")
	owned = true
	if c := channel; c != nil {
		if e = c.grant.Activate(); e != nil {
			c.cancel()
			h.cancelRequested = true
			_ = s.store.CancelIntent(r.ID, r.Generation, r.Owner)
			h.emit("authorization_failed")
			h.cancel <- struct{}{}
			go h.supervise(ctx)
			return h, ErrLaunch
		}
	}
	if c := spec.CodexChannel; c != nil {
		driverCtx, cancel := context.WithCancel(ctx)
		h.codexCancel = cancel
		h.codexResult = make(chan error, 1)
		go func() {
			defer h.codexStream.Close()
			if !spec.SourceCurrent() || !c.current() {
				h.codexResult <- ErrLaunch
				return
			}
			err := c.driver(driverCtx, h.codexStream)
			if !spec.SourceCurrent() || !c.current() {
				err = ErrLaunch
			}
			h.codexResult <- err
		}()
	}
	go h.supervise(ctx)
	return h, nil
}
func (h *Handle) journal(state string) error {
	b, e := json.Marshal(struct {
		State, RunID                       string
		Generation                         int64
		Nonce, ExecutableHash, ProfileHash string
		Process                            Identity
	}{state, h.run.ID, h.run.Generation, h.nonce, h.spec.ExecutableHash, h.profileHash, h.identity})
	if e != nil {
		return e
	}
	path := filepath.Join(h.spec.Root, "launch.json")
	flags := os.O_CREATE | os.O_WRONLY | os.O_EXCL
	if state != "intent" {
		path += ".next"
	}
	f, e := os.OpenFile(path, flags, 0600)
	if e != nil {
		return e
	}
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if state != "intent" {
		if e = os.Rename(path, filepath.Join(h.spec.Root, "launch.json")); e != nil {
			return e
		}
	}
	dir, e := os.Open(h.spec.Root)
	if e != nil {
		return e
	}
	defer dir.Close()
	return dir.Sync()
}
func (h *Handle) emit(kind string) {
	h.seq++
	select {
	case h.events <- Event{h.run.ID, h.run.Generation, h.seq, kind}:
	default:
	}
}
func (h *Handle) Events() <-chan Event { return h.events }
func (h *Handle) Wait(ctx context.Context) (Result, error) {
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case <-h.done:
		return h.result, nil
	}
}
func (h *Handle) Cancel() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	select {
	case <-h.done:
		return nil
	default:
	}
	if h.cancelRequested {
		return nil
	}
	if e := h.supervisor.store.CancelIntent(h.run.ID, h.run.Generation, h.run.Owner); e != nil {
		return ErrIdentity
	}
	h.channel.cancel()
	h.cancelRequested = true
	select {
	case h.cancel <- struct{}{}:
	default:
	}
	return signalProcess(h.identity, syscall.SIGTERM)
}
func (h *Handle) supervise(ctx context.Context) {
	defer h.channel.cancel()
	wait := make(chan error, 1)
	go func() { wait <- h.cmd.Wait() }()
	deadline := time.NewTimer(h.spec.Timeout)
	defer deadline.Stop()
	heartbeat := time.NewTicker(2 * time.Second)
	defer heartbeat.Stop()
	var kill <-chan time.Time
	var killTimer *time.Timer
	var waitErr error
	var channelFailure <-chan struct{}
	var driverResult <-chan error = h.codexResult
	driverFinished, driverSucceeded := driverResult == nil, driverResult == nil
	if h.channel != nil {
		channelFailure = h.channel.failure
	}
	stop := func() {
		h.channel.cancel()
		if h.codexCancel != nil {
			h.codexCancel()
			h.codexStream.Close()
		}
		h.mu.Lock()
		if !h.cancelRequested {
			h.cancelRequested = true
			_ = h.supervisor.store.CancelIntent(h.run.ID, h.run.Generation, h.run.Owner)
		}
		_ = signalProcess(h.identity, syscall.SIGTERM)
		h.mu.Unlock()
		if kill == nil {
			killTimer = time.NewTimer(200 * time.Millisecond)
			kill = killTimer.C
		}
	}
	for {
		select {
		case waitErr = <-wait:
			goto exited
		case err := <-driverResult:
			driverFinished, driverSucceeded = true, err == nil
			driverResult = nil
			if err != nil {
				stop()
			}
		case <-h.cancel:
			stop()
		case <-channelFailure:
			stop()
			channelFailure = nil
		case <-ctx.Done():
			stop()
			ctx = context.Background()
		case <-deadline.C:
			stop()
		case <-heartbeat.C:
			_, over := h.output.snapshot()
			_, errOver := h.stderr.snapshot()
			h.mu.Lock()
			cancelled := h.cancelRequested
			h.mu.Unlock()
			if over || errOver || cancelled || !h.spec.SourceCurrent() || h.spec.CodexChannel != nil && !h.spec.CodexChannel.current() {
				stop()
			} else if h.supervisor.store.RenewLease(h.run.ID, h.run.Generation, h.run.Owner, time.Minute) != nil {
				stop()
			} else {
				h.emit("heartbeat")
			}
		case <-kill:
			_ = signalProcess(h.identity, syscall.SIGKILL)
			kill = nil
		}
	}
exited:
	if !driverFinished {
		// A validated reply may have arrived just before EOF/Wait. Let the
		// trusted driver consume it, with a bounded join; late completion can
		// never turn an already recorded failure into success.
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case err := <-driverResult:
			driverSucceeded = err == nil
		case <-timer.C:
		}
		timer.Stop()
	}
	if h.codexCancel != nil {
		h.codexCancel()
		h.codexStream.Close()
	}
	if killTimer != nil {
		killTimer.Stop()
	}
	out, over := h.output.snapshot()
	_, errOver := h.stderr.snapshot()
	h.mu.Lock()
	defer h.mu.Unlock()
	// Wait has reaped the only process allowed by the kernel profile. No PID-
	// group enumeration or prompt assertion is used to claim descendant exit.
	verified := reaped(h.cmd.ProcessState)
	state := "failed"
	if h.cancelRequested {
		state = "cancelled"
	} else if waitErr == nil && driverSucceeded && (h.codexStream == nil || !h.codexStream.failed.Load()) && !over && !errOver && h.spec.SourceCurrent() && (h.spec.CodexChannel == nil || h.spec.CodexChannel.current()) && h.spec.ValidateOutcome(out) && h.spec.SourceCurrent() && (h.spec.CodexChannel == nil || h.spec.CodexChannel.current()) {
		state = "succeeded"
	}
	if state != "succeeded" && h.spec.Writable {
		state = "interrupted"
	}
	current, e := h.supervisor.store.CheckActive(h.run.ID, h.run.Generation)
	if e != nil || current.Owner != h.run.Owner {
		state = "execution_uncertain"
		verified = false
	} else if e = h.supervisor.store.Finish(h.run.ID, h.run.Generation, h.run.Owner, state); e != nil {
		state = "execution_uncertain"
		verified = false
	}
	actualHash, hashErr := FileHash(h.spec.Executable)
	if hashErr != nil || actualHash != h.spec.ExecutableHash {
		state = "execution_uncertain"
		verified = false
	}
	if h.journal("stopped") != nil {
		verified = false
	}
	h.result = Result{State: state, ExitCode: h.cmd.ProcessState.ExitCode(), StoppedVerified: verified, OutputHash: hashBytes(out), OutputBytes: len(out)}
	if verified {
		identityRaw, _ := json.Marshal(struct {
			Process                            Identity
			Nonce, ExecutableHash, ProfileHash string
		}{h.identity, h.nonce, h.spec.ExecutableHash, h.profileHash})
		p := policy.StopProof{RunID: h.run.ID, Generation: h.run.Generation, NativeSessionID: h.spec.NativeSessionID, ProcessIdentityHash: hashBytes(identityRaw), DescendantsStopped: true}
		raw, _ := json.Marshal(h.result)
		p.ReportHash = hashBytes(append(identityRaw, raw...))
		h.result.Proof = p
		h.supervisor.mu.Lock()
		h.supervisor.proofs[p.ReportHash] = p
		h.supervisor.mu.Unlock()
	}
	h.emit(state)
	h.channel.release()
	h.spec.CodexChannel.release()
	close(h.events)
	close(h.done)
}
func (s *Supervisor) VerifyStop(proof policy.StopProof) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.proofs[proof.ReportHash]
	return ok && p == proof
}

var _ Adapter = (*Supervisor)(nil)

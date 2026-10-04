//go:build darwin

package runtime_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/runtime/codex"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
	"github.com/yetone/magpie/internal/fusion/workspace"
)

var codexNativeCLI = flag.String("fusion-native-codex", "", "explicit pinned private Codex bootstrap test; no login/model")

func codexPrivate(t *testing.T) string {
	t.Helper()
	p, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil || os.Chmod(p, 0700) != nil {
		t.Fatal("private directory failed")
	}
	return p
}
func codexPrepared(t *testing.T) (*store.Store, store.StageRun) {
	t.Helper()
	s, e := store.Open(codexPrivate(t))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	effort := "medium"
	route := stageplan.Route{ID: "fixture-codex-route", Revision: 1, Model: "fixture-model", Account: "fixture-account", Workspace: "fixture-provider-home", CredentialIdentity: "fixture-credential", RuntimeVersion: managed.CodexCLIVersion, BillingPath: "subscription", BillingKnown: true, Admitted: true, Efforts: []string{effort}, DefaultEffort: &effort, Capabilities: []string{"text"}, LockEnforcement: stageplan.ControlledCalls}
	binding := stageplan.Binding{Mode: stageplan.Locked, Route: &stageplan.RouteRef{ID: route.ID, Revision: 1}, Model: route.Model, Effort: &stageplan.EffortSelection{Mode: stageplan.EffortDefault}}
	plan, e := stageplan.Compile(1, []stageplan.Role{stageplan.Design}, stageplan.Layer{Roles: map[stageplan.Role]stageplan.Binding{stageplan.Design: binding}}, stageplan.Layer{}, stageplan.Layer{}, []stageplan.Route{route})
	if e != nil {
		t.Fatal(e)
	}
	task, e := s.Create("fixture-key", store.CreateRequest{ProjectID: "fixture-project", Goal: "native metadata fixture", Plan: plan})
	if e != nil {
		t.Fatal(e)
	}
	if e := s.ConfigureBudget(task.ID, store.Budget{MaxCalls: 2}); e != nil {
		t.Fatal(e)
	}
	r, e := s.StartReserved(store.StartRequest{TaskID: task.ID, Role: stageplan.Design, PlanRevision: 1, Owner: "fixture-owner", TTL: time.Minute, Target: *plan.Bindings[stageplan.Design].Target}, store.ReservationRequest{PoolKey: "fixture-pool", GlobalLimit: 2, AdmissionHash: strings.Repeat("a", 64)})
	if e != nil {
		t.Fatal(e)
	}
	return s, r
}

// Actual immutable binary, production Seatbelt/Supervisor/Store/Client and
// StdioPeer. Synthetic registry/plan/reservation do not admit a real account.
func TestManagedPinnedCodexBootstrap(t *testing.T) {
	if *codexNativeCLI == "" {
		t.Skip("explicit pinned Native bootstrap only")
	}
	exe, e := filepath.EvalSymlinks(*codexNativeCLI)
	if e != nil {
		t.Fatal(e)
	}
	if hash, e := managed.FileHash(exe); e != nil || hash != managed.CodexExecutableSHA256 {
		t.Fatal("Native pin changed")
	}
	for _, mode := range []string{"success", "driver_failure", "semantic_failure", "validator_source_drift", "validator_identity_drift", "cancel", "parent_cancel", "timeout", "source_drift", "identity_drift", "transport_overflow", "ignored_overflow", "prelaunch_stale", "prelaunch_args", "prelaunch_target"} {
		t.Run(mode, func(t *testing.T) {
			s, r := codexPrepared(t)
			root, origin, copies := codexPrivate(t), codexPrivate(t), codexPrivate(t)
			if e := os.WriteFile(filepath.Join(origin, "fixture.txt"), []byte("owned"), 0600); e != nil {
				t.Fatal(e)
			}
			copy, e := workspace.Copy(origin, copies, "copied-project")
			if e != nil {
				t.Fatal(e)
			}
			guard, e := copy.Guard()
			if e != nil {
				t.Fatal(e)
			}
			var current, verified atomic.Bool
			current.Store(true)
			ready := make(chan struct{})
			var observed codexObservedStream
			driverDone := make(chan struct{})
			identity := codex.Identity{Account: r.Target.Account, Workspace: r.Target.Workspace, CredentialIdentity: r.Target.CredentialIdentity, Generation: 1}
			binding := codex.Binding{Scope: codex.Scope{RunID: r.ID, Generation: r.Generation, Role: r.Role}, Identity: identity, Target: r.Target, Cwd: copy.Path, CodexHome: filepath.Join(root, "config", "codex")}
			driver := func(ctx context.Context, stream io.ReadWriteCloser) error {
				defer close(driverDone)
				observed.stream = stream
				stream = &observed
				peer, e := codex.NewStdioPeer(stream, current.Load)
				if e != nil {
					return e
				}
				defer peer.Close()
				client, e := codex.New(peer, binding, func(scope codex.Scope) bool { return scope == binding.Scope && current.Load() }, func() codex.Identity { return identity }, nil)
				if e != nil {
					return e
				}
				if e = client.Initialize(ctx); e != nil {
					return e
				}
				if !errors.Is(client.ReadAccount(ctx), codex.ErrUnverified) {
					return codex.ErrProtocol
				}
				if _, e := client.StartThread(ctx); !errors.Is(e, codex.ErrUnverified) {
					return codex.ErrProtocol
				}
				close(ready)
				if mode == "transport_overflow" || mode == "ignored_overflow" {
					_, e = stream.Write(make([]byte, 65537))
					if mode == "ignored_overflow" {
						verified.Store(true)
						return nil
					}
					return e
				}
				if mode == "driver_failure" {
					return codex.ErrUnverified
				}
				if mode == "success" || mode == "semantic_failure" || strings.HasPrefix(mode, "validator_") {
					verified.Store(true)
					return nil
				}
				<-ctx.Done()
				return ctx.Err()
			}
			ch, e := managed.NewCodexChannel(r, root, copy.Path, "fixture-native-launch", driver, current.Load)
			if e != nil {
				t.Fatal(e)
			}
			spec := managed.Spec{Source: guard, Root: root, Workspace: copy.Path, Executable: exe, ExecutableHash: managed.CodexExecutableSHA256, Args: []string{"app-server", "--listen", "stdio://", "--strict-config"}, NativeSessionID: "fixture-native-launch", CodexChannel: ch, Timeout: 10 * time.Second, ValidateOutcome: func([]byte) bool {
				if mode == "validator_source_drift" {
					if e := os.WriteFile(filepath.Join(origin, "fixture.txt"), []byte("validator change"), 0600); e != nil {
						return false
					}
				}
				if mode == "validator_identity_drift" {
					current.Store(false)
				}
				return verified.Load() && mode != "semantic_failure"
			}}
			if mode == "timeout" {
				spec.Timeout = time.Second
			}
			if mode == "prelaunch_stale" {
				current.Store(false)
			}
			if mode == "prelaunch_args" {
				spec.Args = append(spec.Args, "-c", "ignore_host_managed=true")
			}
			if mode == "prelaunch_target" {
				r.Target.Account = "fixture-other"
			}
			sup := managed.NewSupervisor(s)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			h, e := sup.Start(ctx, r, spec)
			if strings.HasPrefix(mode, "prelaunch_") {
				if e == nil || h != nil {
					t.Fatal("invalid native launch accepted")
				}
				if _, e := os.Stat(filepath.Join(root, "launch.json")); !os.IsNotExist(e) {
					t.Fatal("invalid launch wrote intent")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() {
				h.Cancel()
				wait, c := context.WithTimeout(context.Background(), 3*time.Second)
				defer c()
				h.Wait(wait)
			})
			select {
			case <-ready:
			case <-driverDone:
				t.Log(observed.projection())
				t.Fatal("Native initialize failed")
			case <-time.After(5 * time.Second):
				t.Fatal("Native initialize timed out")
			}
			// A starting/running/terminal run never accepts another Native spawn.
			if other, e := sup.Start(ctx, r, spec); e == nil || other != nil {
				t.Fatal("duplicate native launch")
			}
			switch mode {
			case "cancel":
				if e := h.Cancel(); e != nil {
					t.Fatal(e)
				}
			case "parent_cancel":
				cancel()
			case "source_drift":
				if e := os.WriteFile(filepath.Join(origin, "fixture.txt"), []byte("changed"), 0600); e != nil {
					t.Fatal(e)
				}
			case "identity_drift":
				current.Store(false)
			}
			wait, stop := context.WithTimeout(context.Background(), 6*time.Second)
			defer stop()
			result, e := h.Wait(wait)
			if e != nil || !result.StoppedVerified || !sup.VerifyStop(result.Proof) {
				t.Fatal("actual native exit not proven", e, result.State)
			}
			if mode == "success" {
				if result.State != "succeeded" || result.ExitCode != 0 || !verified.Load() || result.OutputBytes == 0 {
					t.Log(observed.projection())
					t.Fatal("native metadata not validated", result.State, result.ExitCode)
				}
			} else if result.State == "succeeded" {
				t.Fatal("negative accepted", mode)
			}
			select {
			case <-driverDone:
			case <-time.After(time.Second):
				t.Fatal("driver not cancelled/joined")
			}
			if !observed.emptyAccount() {
				t.Fatal("private native account must be empty")
			}
			if b, e := s.Budget(r.TaskID); e != nil || b.UsedCalls != 0 {
				t.Fatal("metadata spent model budget")
			}
			if e := (&policy.Scheduler{Store: s, VerifyStop: sup.VerifyStop}).Release(result.Proof); e != nil {
				t.Fatal(e)
			}
			if _, e := s.Reservation(r.ID); !errors.Is(e, store.ErrNotFound) {
				t.Fatal("capacity not released")
			}
			if _, e := os.Stat(filepath.Join(root, "config", "codex", "auth.json")); !os.IsNotExist(e) {
				t.Fatal("credentials imported")
			}
			if b, e := os.ReadFile(filepath.Join(copy.Path, "fixture.txt")); e != nil || string(b) != "owned" {
				t.Fatal("copied project changed")
			}
			t.Log("pinned Native metadata only; real wait/StopProof/release; login=0 model=0 quota=0")
		})
	}
}

// Project only frame structure from the empty private Native home. No account,
// auth content, stderr or Native home paths are printed on a test failure.
type codexObservedStream struct {
	stream io.ReadWriteCloser
	mu     sync.Mutex
	raw    []byte
}

func (s *codexObservedStream) Read(b []byte) (int, error) {
	n, e := s.stream.Read(b)
	s.mu.Lock()
	if len(s.raw)+n <= 65536 {
		s.raw = append(s.raw, b[:n]...)
	}
	s.mu.Unlock()
	return n, e
}
func (s *codexObservedStream) Write(b []byte) (int, error) { return s.stream.Write(b) }
func (s *codexObservedStream) Close() error                { return s.stream.Close() }
func (s *codexObservedStream) projection() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var frames []map[string]any
	for _, line := range bytes.Split(s.raw, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var f map[string]json.RawMessage
		if json.Unmarshal(line, &f) != nil {
			continue
		}
		var method string
		json.Unmarshal(f["method"], &method)
		var result map[string]json.RawMessage
		json.Unmarshal(f["result"], &result)
		var keys []string
		for k := range result {
			keys = append(keys, k)
		}
		var top []string
		for k := range f {
			top = append(top, k)
		}
		frames = append(frames, map[string]any{"id": string(f["id"]), "method": method, "resultKeys": keys, "topKeys": top, "paramsNull": string(f["params"]) == "null"})
	}
	return frames
}

func (s *codexObservedStream) emptyAccount() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, line := range bytes.Split(s.raw, []byte("\n")) {
		var f struct {
			ID     int64 `json:"id"`
			Result struct {
				Account  json.RawMessage `json:"account"`
				Requires bool            `json:"requiresOpenaiAuth"`
			} `json:"result"`
		}
		if json.Unmarshal(line, &f) == nil && f.ID == 2 {
			return bytes.Equal(f.Result.Account, []byte("null")) && f.Result.Requires
		}
	}
	return false
}

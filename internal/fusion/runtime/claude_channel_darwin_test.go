//go:build darwin

package runtime

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

func managedChannelFixture(t *testing.T) (*Supervisor, *store.Store, store.StageRun, Spec, *policy.Manager, *policy.PendingModelGrant, string, *atomic.Bool) {
	t.Helper()
	s, e := store.Open(pdir(t))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	high := "high"
	route := stageplan.Route{ID: "fixture-glm", Revision: 1, Model: "glm-5.3", Account: "fixture-account", Workspace: "fixture-workspace", CredentialIdentity: "fixture-credential", RuntimeVersion: ClaudeCLIVersion, BillingPath: "coding_plan", BillingKnown: true, Admitted: true, Efforts: []string{high}, DefaultEffort: &high, Capabilities: []string{"text"}, LockEnforcement: stageplan.ControlledCalls}
	plan, e := stageplan.Compile(1, []stageplan.Role{stageplan.Design}, stageplan.Layer{Roles: map[stageplan.Role]stageplan.Binding{stageplan.Design: {Mode: stageplan.Locked, Route: &stageplan.RouteRef{ID: route.ID, Revision: 1}, Model: route.Model, Effort: &stageplan.EffortSelection{Mode: stageplan.EffortDefault}}}}, stageplan.Layer{}, stageplan.Layer{}, []stageplan.Route{route})
	if e != nil {
		t.Fatal(e)
	}
	task, e := s.Create("fixture-channel", store.CreateRequest{ProjectID: "fixture-project", Goal: "fixture", Plan: plan})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConfigureBudget(task.ID, store.Budget{MaxCalls: 2}); e != nil {
		t.Fatal(e)
	}
	run, e := s.StartReserved(store.StartRequest{TaskID: task.ID, Role: stageplan.Design, PlanRevision: 1, Owner: "fixture-owner", TTL: time.Minute, Target: *plan.Bindings[stageplan.Design].Target}, store.ReservationRequest{PoolKey: "fixture-pool", GlobalLimit: 2, AdmissionHash: strings.Repeat("a", 64)})
	if e != nil {
		t.Fatal(e)
	}
	valid := &atomic.Bool{}
	valid.Store(true)
	validator := policy.StoreValidator(s)
	m := policy.NewManager("fixture-management", func(c policy.Claims) bool { return valid.Load() && validator(c) }, nil)
	c := policy.Claims{TaskID: task.ID, RunID: run.ID, Role: run.Role, Attempt: run.Attempt, PlanRevision: run.PlanRevision, Generation: run.Generation, ProjectID: task.ProjectID, Audience: policy.ModelAudience}
	p, e := m.PrepareModel(c, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	ports := []string{}
	channel, e := NewClaudeChannel(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(403) }), p, run.Target)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { channel.Close() })
	ports = append(ports, channel.port)
	for i := 0; i < 1; i++ {
		listener, e := net.Listen("tcp4", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { listener.Close() })
		ports = append(ports, strings.Split(listener.Addr().String(), ":")[1])
		go func() {
			for {
				connection, e := listener.Accept()
				if e != nil {
					return
				}
				connection.Close()
			}
		}()
	}
	exe := compileProbe(t)
	hash, e := FileHash(exe)
	if e != nil {
		t.Fatal(e)
	}
	control := exec.Command(exe, "--channel-control", ports[0], ports[1])
	control.Env = []string{}
	if b, e := control.CombinedOutput(); e != nil || string(b) != "fixture-completed\n" {
		t.Fatalf("channel positive control failed: %v (synthetic output %s)", e, b)
	}
	root, workspace := pdir(t), pdir(t)
	release := filepath.Join(root, "tmp", "release")
	spec := Spec{Executable: exe, ExecutableHash: hash, Args: []string{"--channel", ports[0], ports[1], release}, Root: root, Workspace: workspace, Timeout: 3 * time.Second, NativeSessionID: "fixture-channel-session", ClaudeChannel: channel, ValidateOutcome: func(b []byte) bool { return string(b) == "fixture-completed\n" }}
	return NewSupervisor(s), s, run, spec, m, p, release, valid
}

func TestManagedClaudeChannelActivatesAfterConfirmationAndReapsOneProcess(t *testing.T) {
	sup, s, run, spec, m, p, release, _ := managedChannelFixture(t)
	raw, e := p.Secret()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.AuthenticateStage(raw, p.Claims()); e == nil {
		t.Fatal("prelaunch entropy already authorized")
	}
	h, e := sup.Start(context.Background(), run, spec)
	if e != nil {
		t.Fatal("channel start", e)
	}
	defer h.Cancel()
	if _, e = m.AuthenticateStage(raw, p.Claims()); e != nil {
		t.Fatal("confirmed run grant not active", e)
	}
	if e = os.WriteFile(release, []byte("fixture-release"), 0600); e != nil {
		t.Fatal(e)
	}
	got := wait(t, h)
	if got.State != "succeeded" || !got.StoppedVerified || !sup.VerifyStop(got.Proof) {
		errOut, _ := h.stderr.snapshot()
		t.Logf("synthetic channel stderr: %s", strings.ReplaceAll(string(errOut), raw, "<stage-grant>"))
		t.Fatal("channel boundary or stop unverified", got.State, got.ExitCode)
	}
	if _, e = p.Secret(); e == nil {
		t.Fatal("terminal capability retained")
	}
	if _, e = m.AuthenticateStage(raw, p.Claims()); e == nil {
		t.Fatal("terminal model calls allowed")
	}
	if b, e := os.ReadFile(filepath.Join(spec.Root, "launch.json")); e != nil || strings.Contains(string(b), raw) {
		t.Fatal("launch journal exposed grant")
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), raw) {
		t.Fatal("stop report exposed grant")
	}
	if e = (&policy.Scheduler{Store: s, VerifyStop: sup.VerifyStop}).Release(got.Proof); e != nil {
		t.Fatal(e)
	}
}

func TestManagedClaudeChannelScopeFailureNeverSpawnsAndActivationFailureStops(t *testing.T) {
	for _, mode := range []string{"scope", "activate", "listener_failed"} {
		t.Run(mode, func(t *testing.T) {
			sup, _, run, spec, _, p, _, valid := managedChannelFixture(t)
			if mode == "scope" {
				run.Target.Account = "fixture-other"
			} else if mode == "activate" {
				valid.Store(false)
			}
			h, e := sup.Start(context.Background(), run, spec)
			if mode == "listener_failed" {
				if e != nil || h == nil {
					t.Fatal("fixture launch", e)
				}
				spec.ClaudeChannel.listeners[0].Close()
				got := wait(t, h)
				if got.State == "succeeded" || !got.StoppedVerified || !sup.VerifyStop(got.Proof) {
					t.Fatal("lost listener did not stop process", got.State)
				}
				return
			}
			if e == nil {
				if h != nil {
					h.Cancel()
				}
				t.Fatal("unsafe channel start accepted")
			}
			if mode == "scope" {
				if _, e = os.Stat(filepath.Join(spec.Root, "launch.json")); !os.IsNotExist(e) {
					t.Fatal("scope mismatch spawned or journaled")
				}
				return
			}
			if h == nil {
				t.Fatal("known spawned activation failure lost managed handle")
			}
			got := wait(t, h)
			if got.State == "succeeded" || !got.StoppedVerified || !sup.VerifyStop(got.Proof) {
				t.Fatal("activation failure not stopped/reconciled", got.State)
			}
			if _, e = p.Secret(); e == nil {
				t.Fatal("failed activation retained entropy")
			}
		})
	}
}

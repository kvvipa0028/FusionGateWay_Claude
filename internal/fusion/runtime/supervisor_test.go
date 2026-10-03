//go:build darwin

package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
	"github.com/yetone/magpie/internal/fusion/workspace"
)

func TestWorkerFixture(t *testing.T) {
	mode := os.Getenv("FUSION_WORKER_FIXTURE")
	if mode == "" {
		t.Skip("worker helper only")
	}
	switch mode {
	case "ok":
		os.Stdout.WriteString("fixture-completed\n")
	case "delay":
		time.Sleep(time.Hour)
	case "stubborn":
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGTERM)
		os.WriteFile("ready", []byte("ready"), 0600)
		for {
			time.Sleep(time.Second)
		}
	case "barrier":
		os.WriteFile("ready", []byte("ready"), 0600)
		for {
			if _, e := os.Stat("release"); e == nil {
				break
			}
			time.Sleep(time.Millisecond)
		}
		os.Stdout.WriteString("fixture-completed\n")
	case "write_loss":
		os.WriteFile("effect.txt", []byte("fixture effect"), 0600)
		os.Exit(70)
	case "boundaries":
		if os.Getenv("FUSION_MANAGEMENT_SECRET") != "" {
			os.Exit(80)
		}
		if _, e := os.ReadFile(os.Getenv("FUSION_FIXTURE_FORBIDDEN")); e == nil {
			os.Exit(81)
		}
		if e := os.WriteFile(os.Getenv("FUSION_FIXTURE_OUTSIDE"), []byte("escape"), 0600); e == nil {
			os.Exit(82)
		}
		if e := os.WriteFile("allowed.txt", []byte("fixture allowed"), 0600); e != nil {
			os.Exit(83)
		}
		if e := exec.Command("/usr/bin/true").Run(); e == nil {
			os.Exit(84)
		}
		os.Stdout.WriteString("fixture-completed\n")
	case "network":
		c, e := net.DialTimeout("tcp", os.Getenv("FUSION_FIXTURE_NETWORK"), time.Second)
		if e == nil {
			c.Close()
			os.Exit(87)
		}
		os.Stdout.WriteString("fixture-completed\n")
	case "fork":
		e := exec.Command(os.Args[0], "-test.run=^TestWorkerFixture$").Start()
		if e == nil {
			os.Exit(85)
		}
		os.Stdout.WriteString("fixture-completed\n")
	case "readonly":
		if e := os.WriteFile("effect.txt", []byte("bad"), 0600); e == nil {
			os.Exit(86)
		}
		os.Stdout.WriteString("fixture-completed\n")
	case "overflow":
		os.Stdout.WriteString(strings.Repeat("x", 128*1024))
		time.Sleep(time.Hour)
	}
	os.Exit(0)
}
func pdir(t *testing.T) string {
	t.Helper()
	p := t.TempDir()
	os.Chmod(p, 0700)
	p, _ = filepath.EvalSymlinks(p)
	return p
}
func setup(t *testing.T, mode string, write bool) (*Supervisor, *Handle, *store.Store, store.StageRun, Spec, string) {
	t.Helper()
	stateRoot := pdir(t)
	s, e := store.Open(stateRoot)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	def := "medium"
	r := stageplan.Route{ID: "fixture-route", Revision: 1, Model: "fixture-model", Account: "fixture-account", Workspace: "fixture-workspace", CredentialIdentity: "fixture-credential", RuntimeVersion: "fixture-v1", BillingPath: "fixture-plan", BillingKnown: true, Admitted: true, Efforts: []string{"medium"}, DefaultEffort: &def, Capabilities: []string{"text"}, LockEnforcement: stageplan.ControlledCalls}
	b := stageplan.Binding{Mode: stageplan.Locked, Route: &stageplan.RouteRef{ID: r.ID, Revision: 1}, Model: r.Model, Effort: &stageplan.EffortSelection{Mode: stageplan.EffortDefault}}
	p, e := stageplan.Compile(1, []stageplan.Role{stageplan.Implementation}, stageplan.Layer{Roles: map[stageplan.Role]stageplan.Binding{stageplan.Implementation: b}}, stageplan.Layer{}, stageplan.Layer{}, []stageplan.Route{r})
	if e != nil {
		t.Fatal(e)
	}
	task, e := s.Create("fixture-key", store.CreateRequest{ProjectID: "fixture-project", Goal: "fixture", Plan: p})
	if e != nil {
		t.Fatal(e)
	}
	s.ConfigureBudget(task.ID, store.Budget{MaxCalls: 2})
	run, e := s.StartReserved(store.StartRequest{TaskID: task.ID, Role: stageplan.Implementation, PlanRevision: 1, Owner: "fixture-worker", TTL: time.Minute, Target: *p.Bindings[stageplan.Implementation].Target}, store.ReservationRequest{PoolKey: "fixture-pool", WriteKey: "fixture-writer", GlobalLimit: 2, AdmissionHash: strings.Repeat("a", 64)})
	if e != nil {
		t.Fatal(e)
	}
	exe, _ := os.Executable()
	exe, _ = filepath.EvalSymlinks(exe)
	hash, e := FileHash(exe)
	if e != nil {
		t.Fatal(e)
	}
	root, work := pdir(t), pdir(t)
	if mode == "mach_network_fork" {
		exe = compileProbe(t)
		hash, e = FileHash(exe)
		if e != nil {
			t.Fatal(e)
		}
	}
	spec := Spec{Executable: exe, ExecutableHash: hash, Args: []string{"-test.run=^TestWorkerFixture$"}, Root: root, Workspace: work, Writable: write, Timeout: 3 * time.Second, NativeSessionID: "fixture-session", FixtureEnvironment: map[string]string{"FUSION_WORKER_FIXTURE": mode}, ValidateOutcome: func(out []byte) bool { return string(out) == "fixture-completed\n" }}
	if mode == "mach_network_fork" {
		listener, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { listener.Close() })
		port := strings.Split(listener.Addr().String(), ":")[1]
		spec.Args = nil
		spec.FixtureEnvironment["FUSION_FIXTURE_NETWORK"] = port
		go func() {
			for {
				conn, e := listener.Accept()
				if e != nil {
					return
				}
				conn.Close()
			}
		}()
		control := exec.Command(exe, "--control")
		control.Env = []string{"FUSION_FIXTURE_NETWORK=" + port}
		b, e := control.CombinedOutput()
		if e != nil || string(b) != "fixture-control-ready\n" {
			t.Fatalf("native boundary control unavailable: %v (synthetic output %s)", e, b)
		}
	}
	if mode == "boundaries" {
		outside := pdir(t)
		secret := filepath.Join(outside, "fixture-management-secret")
		os.WriteFile(secret, []byte("fixture-management-secret"), 0600)
		spec.FixtureEnvironment["FUSION_FIXTURE_FORBIDDEN"] = secret
		spec.FixtureEnvironment["FUSION_FIXTURE_OUTSIDE"] = filepath.Join(outside, "escape")
	}
	sup := NewSupervisor(s)
	h, e := sup.Start(context.Background(), run, spec)
	if e != nil {
		dirs, _ := os.ReadDir(root)
		names := []string{}
		for _, d := range dirs {
			names = append(names, d.Name())
		}
		real, _ := filepath.EvalSymlinks(exe)
		info, _ := os.Stat(exe)
		t.Fatalf("start: %v root=%v work=%v canonical=%v mode=%v files=%v", e, workspace.PrivateState(root), workspace.PrivateState(work), real == exe, info.Mode(), names)
	}
	t.Cleanup(func() {
		h.Cancel()
		ctx, c := context.WithTimeout(context.Background(), time.Second)
		defer c()
		h.Wait(ctx)
	})
	return sup, h, s, run, spec, stateRoot
}
func wait(t *testing.T, h *Handle) Result {
	t.Helper()
	ctx, c := context.WithTimeout(context.Background(), 5*time.Second)
	defer c()
	r, e := h.Wait(ctx)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestNativeWorkerCompletionAndUnforgeableStopProof(t *testing.T) {
	sup, h, s, r, _, _ := setup(t, "ok", false)
	got := wait(t, h)
	if got.State != "succeeded" || !got.StoppedVerified || !sup.VerifyStop(got.Proof) {
		t.Log(h.cmd.ProcessState.String())
		errOut, _ := h.stderr.snapshot()
		t.Fatalf("completion not verified: %+v; synthetic stderr: %s", got, errOut)
		t.Log(h.cmd.ProcessState.String())
	}
	altered := got.Proof
	altered.Generation++
	if sup.VerifyStop(altered) {
		t.Fatal("forged stop proof accepted")
	}
	if e := (&policy.Scheduler{Store: s, VerifyStop: sup.VerifyStop}).Release(got.Proof); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Reservation(r.ID); !errors.Is(e, store.ErrNotFound) {
		t.Fatal("reservation held")
	}
}
func TestNativeWorkerCancellationTimeoutAndWriteUncertainty(t *testing.T) {
	for _, mode := range []string{"delay", "write_loss", "overflow"} {
		t.Run(mode, func(t *testing.T) {
			sup, h, s, r, spec, _ := setup(t, mode, true)
			if mode == "delay" {
				if e := h.Cancel(); e != nil {
					t.Fatal(e)
				}
			}
			got := wait(t, h)
			if !got.StoppedVerified || !sup.VerifyStop(got.Proof) {
				t.Fatal("exit not confirmed")
			}
			if mode == "write_loss" {
				if got.State != "interrupted" {
					t.Fatal("effect failure replayable", got.State)
				}
				if b, e := os.ReadFile(filepath.Join(spec.Workspace, "effect.txt")); e != nil || string(b) != "fixture effect" {
					t.Fatal("effect missing")
				}
				task, _ := s.Task(r.TaskID)
				if task.State != "needs_review" {
					t.Fatal("uncertainty lost")
				}
			} else if got.State == "succeeded" {
				t.Fatal("cancel/overflow accepted")
			}
		})
	}
}
func TestNativeWorkerSandboxDeniesChildCreationAndReadonlyWrites(t *testing.T) {
	for _, mode := range []string{"fork", "readonly"} {
		t.Run(mode, func(t *testing.T) {
			_, h, _, _, _, _ := setup(t, mode, false)
			got := wait(t, h)
			if got.State != "succeeded" || !got.StoppedVerified {
				t.Fatalf("sandbox boundary failed: %+v", got)
			}
		})
	}
}
func TestBirthIdentityRejectsPIDReuseWithoutSignalling(t *testing.T) {
	actual, e := processIdentity(os.Getpid())
	if e != nil {
		t.Fatal(e)
	}
	other := actual
	other.StartMicros++
	if sameProcess(other) {
		t.Fatal("PID only identity accepted")
	}
	if e = signalProcess(other, 0); !errors.Is(e, ErrIdentity) {
		t.Fatal("different birth accepted", e)
	}
}
func TestWorkerRestartCannotAttachOrDuplicateUnknown(t *testing.T) {
	_, h, s, r, _, stateRoot := setup(t, "delay", true)
	s.Close()
	fresh, e := store.Open(stateRoot)
	if e != nil {
		t.Fatal(e)
	}
	defer fresh.Close()
	got, e := fresh.Run(r.ID)
	if e != nil || got.State != "unknown" || got.Generation <= r.Generation {
		t.Fatal("recovery failed")
	}
	if _, e = NewSupervisor(fresh).Start(context.Background(), r, Spec{}); e == nil {
		t.Fatal("unknown replayed")
	}
	if e = h.Cancel(); e == nil {
		t.Fatal("old generation cancel accepted")
	}
	signalProcess(h.identity, 9)
	wait(t, h)
}
func TestReportContainsNoEnvironmentOrOutput(t *testing.T) {
	_, h, _, _, _, _ := setup(t, "ok", false)
	got := wait(t, h)
	b, e := json.Marshal(got)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(b), "fixture-completed") || strings.Contains(string(b), "FUSION_WORKER") {
		t.Fatal("raw output in report")
	}
}

func ready(t *testing.T, p string) {
	t.Helper()
	until := time.Now().Add(2 * time.Second)
	for time.Now().Before(until) {
		if _, e := os.Stat(filepath.Join(p, "ready")); e == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("fixture did not reach barrier")
}
func TestCancelEscalatesStubbornWorkerAndTimeoutTerminates(t *testing.T) {
	_, h, _, _, spec, _ := setup(t, "stubborn", true)
	ready(t, spec.Workspace)
	start := time.Now()
	if e := h.Cancel(); e != nil {
		t.Fatal(e)
	}
	r := wait(t, h)
	if !r.StoppedVerified || time.Since(start) > time.Second || r.State != "interrupted" {
		t.Fatal("cancel failed to stop/reconcile")
	}
	_, slow, _, _, _, _ := setup(t, "delay", false)
	r = wait(t, slow)
	if r.State != "cancelled" || !r.StoppedVerified {
		t.Fatal("timeout failed")
	}
}
func TestCancellationCompletionRaceProducesOneTerminalEvent(t *testing.T) {
	for _, mode := range []string{"cancel_first", "complete_first", "race"} {
		t.Run(mode, func(t *testing.T) {
			_, h, s, r, spec, _ := setup(t, "barrier", true)
			ready(t, spec.Workspace)
			if mode == "cancel_first" {
				h.Cancel()
			} else {
				os.WriteFile(filepath.Join(spec.Workspace, "release"), []byte("release"), 0600)
				if mode == "race" {
					h.Cancel()
				}
			}
			got := wait(t, h)
			if mode == "cancel_first" && got.State != "interrupted" {
				t.Fatal("cancel lost")
			}
			if mode == "complete_first" && got.State != "succeeded" {
				t.Fatal("completion lost")
			}
			if got.State != "interrupted" && got.State != "succeeded" {
				t.Fatal("invalid terminal", got.State)
			}
			if e := h.Cancel(); e != nil {
				t.Fatal("terminal cancel not idempotent")
			}
			events, e := s.Events(r.TaskID, 0)
			if e != nil {
				t.Fatal(e)
			}
			terminal := 0
			for _, v := range events {
				if strings.HasPrefix(v.Kind, "finished_") {
					terminal++
				}
			}
			if terminal != 1 {
				t.Fatal("duplicate terminal")
			}
		})
	}
}
func TestSandboxRejectsManagementReadAndOutsideWrite(t *testing.T) {
	t.Setenv("FUSION_MANAGEMENT_SECRET", "fixture-management-secret")
	_, h, _, _, spec, _ := setup(t, "boundaries", true)
	r := wait(t, h)
	if r.State != "succeeded" {
		t.Fatal("boundary escaped", r.State)
	}
	if _, e := os.Stat(spec.FixtureEnvironment["FUSION_FIXTURE_OUTSIDE"]); !os.IsNotExist(e) {
		t.Fatal("outside effect")
	}
	if _, e := os.Stat(filepath.Join(spec.Workspace, "allowed.txt")); e != nil {
		t.Fatal("allowed write blocked")
	}
	if b, e := os.ReadFile(filepath.Join(spec.Root, "launch.json")); e != nil || strings.Contains(string(b), "fixture-management-secret") {
		t.Fatal("private environment in journal")
	}
}

func compileProbe(t *testing.T) string {
	t.Helper()
	source, e := filepath.Abs("../../../tests/fusion/fixtures/sandbox-probe.c")
	if e != nil {
		t.Fatal(e)
	}
	out := filepath.Join(pdir(t), "fixture-sandbox-probe")
	args := []string{"-Wall", "-Wextra", "-Werror", source, "-o", out}
	if sdk := os.Getenv("SDKROOT"); sdk != "" {
		args = append([]string{"-isysroot", sdk}, args...)
	}
	cmd := exec.Command("/usr/bin/clang", args...)
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("native fixture compile: %v: %s", e, b)
	}
	return out
}
func TestNativeMachNetworkAndForkBoundariesHavePositiveControls(t *testing.T) {
	_, h, _, _, _, _ := setup(t, "mach_network_fork", false)
	got := wait(t, h)
	if got.State != "succeeded" || !got.StoppedVerified {
		t.Fatal("native kernel boundaries unverified", got.State, got.ExitCode)
	}
}

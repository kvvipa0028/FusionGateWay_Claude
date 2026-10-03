package glm

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/quota"
	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

func adapterFixture(t *testing.T, exe string, role stageplan.Role) (AdapterConfig, store.StageRun, managed.Spec, *policy.Inspection, *atomic.Int64) {
	t.Helper()
	private := func() string {
		p, e := filepath.EvalSymlinks(t.TempDir())
		if e != nil {
			t.Fatal(e)
		}
		if e = os.Chmod(p, 0700); e != nil {
			t.Fatal(e)
		}
		return p
	}
	s, e := store.Open(private())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	route := stageplan.Route{ID: "fixture-adapter-glm", Revision: 1, Model: "glm-5.3", Account: "fixture-account", Workspace: "fixture-workspace", CredentialIdentity: "fixture-credential", RuntimeVersion: CLIVersion, BillingPath: "coding_plan", BillingKnown: true, Admitted: true, Efforts: []string{"high"}, DefaultEffort: ptr("high"), Capabilities: []string{"text"}, LockEnforcement: stageplan.ControlledCalls}
	plan, e := stageplan.Compile(1, []stageplan.Role{role}, stageplan.Layer{Roles: map[stageplan.Role]stageplan.Binding{role: {Mode: stageplan.Locked, Route: &stageplan.RouteRef{ID: route.ID, Revision: 1}, Model: route.Model, Effort: &stageplan.EffortSelection{Mode: stageplan.EffortExplicit, Value: "high"}}}}, stageplan.Layer{}, stageplan.Layer{}, []stageplan.Route{route})
	if e != nil {
		t.Fatal(e)
	}
	task, e := s.Create("fixture-adapter", store.CreateRequest{ProjectID: "fixture-project", Goal: "fixture", Plan: plan})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConfigureBudget(task.ID, store.Budget{MaxCalls: 3}); e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	used := float64(20)
	env := &policy.Inspection{Route: route, Provider: "fixture-provider", QueryAdmitted: true, DataAllowed: true, SandboxVerified: true, VerificationAvailable: true, AllCallsCounted: true, ManagedExecutions: 1, ProofHash: strings.Repeat("a", 64), Quota: quota.Snapshot{Identity: quota.Identity{Provider: "fixture-provider", Account: route.Account, Workspace: route.Workspace, Region: "CN", Generation: 1}, Source: "fixture-source", ObservedAt: &now, ReceivedAt: now, Complete: true, Status: quota.Available, Pool: quota.Pool{ID: "fixture-pool", Provider: "fixture-provider", Region: "CN", Scope: "account", Owner: route.Account, Verified: true}, Windows: []quota.Window{{Kind: "subscription", Unit: "percent", UsedPercent: &used}}}}
	writable := role == stageplan.Implementation || role == stageplan.Testing
	if writable {
		env.WriteKey = "fixture-workspace-write"
	}
	scheduler := policy.Scheduler{Store: s, Inspect: func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (policy.Inspection, error) {
		return *env, nil
	}}
	run, e := scheduler.Prepare(context.Background(), store.StartRequest{TaskID: task.ID, Role: role, PlanRevision: 1, Owner: "fixture-owner", TTL: time.Minute, Target: *plan.Bindings[role].Target})
	if e != nil {
		t.Fatal(e)
	}
	loads := &atomic.Int64{}
	config := AdapterConfig{Scheduler: scheduler, Manager: policy.NewManager("fixture-manager", policy.StoreValidator(s), nil), Executable: exe, Current: func(Binding) bool { return true }, LoadCredential: func(context.Context, stageplan.ExecutionTarget) (Credential, error) {
		loads.Add(1)
		return Credential{Identity: route.CredentialIdentity, Key: "fixture-controller-key"}, nil
	}, Transport: fixtureRoundTrip(func(*http.Request) (*http.Response, error) {
		t.Error("unexpected unit-test upstream call")
		return nil, ErrUnverified
	})}
	spec := managed.Spec{Root: private(), Workspace: private(), Writable: writable, Timeout: 15 * time.Second, Input: []byte("fixture prompt")}
	return config, run, spec, env, loads
}

func TestGLMAdapterRequiresTrustedServicesAndRedactsCredential(t *testing.T) {
	config, _, _, _, _ := adapterFixture(t, "/fixture/native-claude", stageplan.Design)
	for _, mode := range []string{"store", "inspect", "manager", "current", "credential", "transport", "path"} {
		c := config
		switch mode {
		case "store":
			c.Scheduler.Store = nil
		case "inspect":
			c.Scheduler.Inspect = nil
		case "manager":
			c.Manager = nil
		case "current":
			c.Current = nil
		case "credential":
			c.LoadCredential = nil
		case "transport":
			c.Transport = nil
		case "path":
			c.Executable = "relative"
		}
		if _, e := NewAdapter(c); e == nil {
			t.Fatal("untrusted adapter accepted", mode)
		}
	}
	raw, _ := json.Marshal(Credential{Identity: "fixture-credential", Key: "fixture-controller-key"})
	if strings.Contains(string(raw), "fixture-controller-key") {
		t.Fatal("credential JSON exposed key")
	}
}

func TestGLMAdapterRejectsClientLaunchControlsBeforeSecretsOrSpawn(t *testing.T) {
	for _, mode := range []string{"args", "executable", "hash", "environment", "validator", "session", "empty_prompt", "bad_utf8", "timeout", "write_role", "quota", "identity", "owner"} {
		t.Run(mode, func(t *testing.T) {
			c, r, in, env, loads := adapterFixture(t, "/fixture/native-claude", stageplan.Design)
			switch mode {
			case "args":
				in.Args = []string{"--tools", "Bash"}
			case "executable":
				in.Executable = "/bin/sh"
			case "hash":
				in.ExecutableHash = strings.Repeat("a", 64)
			case "environment":
				in.FixtureEnvironment = map[string]string{"ANTHROPIC_API_KEY": "fixture-client-key"}
			case "validator":
				in.ValidateOutcome = func([]byte) bool { return true }
			case "session":
				in.NativeSessionID = "fixture-client-session"
			case "empty_prompt":
				in.Input = nil
			case "bad_utf8":
				in.Input = []byte{0xff}
			case "timeout":
				in.Timeout = 5 * time.Minute
			case "write_role":
				in.Writable = true
			case "quota":
				env.Quota.Status = quota.Unknown
			case "identity":
				r.Target.CredentialIdentity = "fixture-other"
			case "owner":
				r.Owner = "fixture-other"
			}
			a, e := NewAdapter(c)
			if e != nil {
				t.Fatal(e)
			}
			if h, e := a.Start(context.Background(), r, in); e == nil || h != nil {
				t.Fatal("unsafe native launch accepted")
			}
			if loads.Load() != 0 {
				t.Fatal("secret loaded before admission")
			}
			if _, e := os.Stat(filepath.Join(in.Root, "launch.json")); !os.IsNotExist(e) {
				t.Fatal("blocked native launch journaled")
			}
			b, e := c.Scheduler.Store.Budget(r.TaskID)
			if e != nil || b.UsedCalls != 0 {
				t.Fatal("blocked startup spent model budget")
			}
		})
	}
}

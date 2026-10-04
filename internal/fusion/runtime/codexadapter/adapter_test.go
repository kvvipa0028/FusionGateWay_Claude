package codexadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/quota"
	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/runtime/codex"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
	"github.com/yetone/magpie/internal/fusion/workspace"
)

type fakeForwarder func(context.Context, stageplan.ExecutionTarget, []byte) (codex.ForwardResponse, error)

func (f fakeForwarder) Send(c context.Context, t stageplan.ExecutionTarget, b []byte) (codex.ForwardResponse, error) {
	return f(c, t, b)
}
func privateDir(t *testing.T) string {
	t.Helper()
	p, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil || os.Chmod(p, 0700) != nil {
		t.Fatal("private directory failed")
	}
	return p
}

// Registry/quota/upstream are synthetic; Store/Scheduler/Manager are real.
func adapterFixture(t *testing.T) (AdapterConfig, store.StageRun, managed.Spec, *atomic.Int64, *policy.Inspection, string) {
	t.Helper()
	s, e := store.Open(privateDir(t))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	effort := "medium"
	route := stageplan.Route{ID: "fixture-codex", Revision: 1, Model: "fixture-model", Account: "fixture-account", Workspace: "fixture-workspace", CredentialIdentity: "fixture-identity", RuntimeVersion: codex.CLIVersion, BillingPath: "subscription", BillingKnown: true, Admitted: true, Efforts: []string{effort}, DefaultEffort: &effort, Capabilities: []string{"text"}, LockEnforcement: stageplan.ControlledCalls}
	role := stageplan.Design
	plan, e := stageplan.Compile(1, []stageplan.Role{role}, stageplan.Layer{Roles: map[stageplan.Role]stageplan.Binding{role: {Mode: stageplan.Locked, Route: &stageplan.RouteRef{ID: route.ID, Revision: 1}, Model: route.Model, Effort: &stageplan.EffortSelection{Mode: stageplan.EffortDefault}}}}, stageplan.Layer{}, stageplan.Layer{}, []stageplan.Route{route})
	if e != nil {
		t.Fatal(e)
	}
	task, e := s.Create("fixture-codex-adapter", store.CreateRequest{ProjectID: "fixture-project", Goal: "fixture", Plan: plan})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConfigureBudget(task.ID, store.Budget{MaxCalls: 3}); e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	used := float64(10)
	in := &policy.Inspection{Route: route, Provider: "fixture-provider", QueryAdmitted: true, DataAllowed: true, SandboxVerified: true, VerificationAvailable: true, AllCallsCounted: true, ManagedExecutions: 1, ProofHash: strings.Repeat("a", 64), Quota: quota.Snapshot{Identity: quota.Identity{Provider: "fixture-provider", Account: route.Account, Workspace: route.Workspace, Region: "fixture-region", Generation: 1}, Source: "fixture-source", ObservedAt: &now, ReceivedAt: now, Complete: true, Status: quota.Available, Pool: quota.Pool{ID: "fixture-pool", Provider: "fixture-provider", Region: "fixture-region", Scope: "account", Owner: route.Account, Verified: true}, Windows: []quota.Window{{Kind: "subscription", Unit: "percent", UsedPercent: &used}}}}
	sch := policy.Scheduler{Store: s, Inspect: func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (policy.Inspection, error) {
		return *in, nil
	}}
	r, e := sch.Prepare(context.Background(), store.StartRequest{TaskID: task.ID, Role: role, PlanRevision: 1, Owner: "fixture-owner", TTL: time.Minute, Target: *plan.Bindings[role].Target})
	if e != nil {
		t.Fatal(e)
	}
	origin := privateDir(t)
	if os.WriteFile(filepath.Join(origin, "fixture.txt"), []byte("source"), 0600) != nil {
		t.Fatal("source")
	}
	copy, e := workspace.Copy(origin, privateDir(t), "copied-project")
	if e != nil {
		t.Fatal(e)
	}
	guard, e := copy.Guard()
	if e != nil {
		t.Fatal(e)
	}
	calls := &atomic.Int64{}
	c := AdapterConfig{Scheduler: sch, Manager: policy.NewManager("fixture-management", policy.StoreValidator(s), nil), Executable: "/fixture/pinned-codex", Identity: func(target stageplan.ExecutionTarget) codex.Identity {
		return codex.Identity{Account: target.Account, Workspace: target.Workspace, CredentialIdentity: target.CredentialIdentity, Generation: 1}
	}, Current: func(codex.Binding) bool { return true }, Forwarder: fakeForwarder(func(context.Context, stageplan.ExecutionTarget, []byte) (codex.ForwardResponse, error) {
		calls.Add(1)
		return codex.ForwardResponse{}, codex.ErrUnverified
	})}
	return c, r, managed.Spec{Source: guard, Root: privateDir(t), Workspace: copy.Path, Timeout: 15 * time.Second, Input: []byte("fixture private prompt")}, calls, in, origin
}

func TestCodexAdapterRequiresTrustedServices(t *testing.T) {
	c, _, _, _, _, _ := adapterFixture(t)
	for _, mode := range []string{"store", "inspect", "manager", "identity", "current", "forwarder", "path", "nul"} {
		t.Run(mode, func(t *testing.T) {
			x := c
			switch mode {
			case "store":
				x.Scheduler.Store = nil
			case "inspect":
				x.Scheduler.Inspect = nil
			case "manager":
				x.Manager = nil
			case "identity":
				x.Identity = nil
			case "current":
				x.Current = nil
			case "forwarder":
				x.Forwarder = nil
			case "path":
				x.Executable = "relative"
			case "nul":
				x.Executable = "/private\x00/codex"
			}
			if _, e := NewAdapter(x); e == nil {
				t.Fatal("untrusted service accepted")
			}
		})
	}
}
func TestCodexAdapterRejectsCallerControlsAndUnverifiedSource(t *testing.T) {
	for _, mode := range []string{"args", "exe", "hash", "env", "validator", "session", "claude", "grok", "codex", "empty", "utf8", "nul", "oversize", "encoded_size", "timeout", "write", "source_missing", "source_drift", "quota", "owner", "identity", "root", "workspace"} {
		t.Run(mode, func(t *testing.T) {
			c, r, in, calls, inspection, origin := adapterFixture(t)
			switch mode {
			case "args":
				in.Args = []string{"--allow-all"}
			case "exe":
				in.Executable = "/bin/sh"
			case "hash":
				in.ExecutableHash = strings.Repeat("a", 64)
			case "env":
				in.FixtureEnvironment = map[string]string{"CODEX_HOME": "/caller"}
			case "validator":
				in.ValidateOutcome = func([]byte) bool { return true }
			case "session":
				in.NativeSessionID = "caller"
			case "claude":
				in.ClaudeChannel = &managed.ClaudeChannel{}
			case "grok":
				in.GrokChannel = &managed.GrokChannel{}
			case "codex":
				in.CodexChannel = &managed.CodexChannel{}
			case "empty":
				in.Input = nil
			case "utf8":
				in.Input = []byte{255}
			case "nul":
				in.Input = []byte("private\x00prompt")
			case "oversize":
				in.Input = make([]byte, 65537)
			case "encoded_size":
				in.Input = []byte(strings.Repeat("\n", 20000))
			case "timeout":
				in.Timeout = 5 * time.Minute
			case "write":
				in.Writable = true
			case "source_missing":
				in.Source = workspace.SourceGuard{}
			case "source_drift":
				os.WriteFile(filepath.Join(origin, "fixture.txt"), []byte("changed"), 0600)
			case "quota":
				inspection.Quota.Status = quota.Unknown
			case "owner":
				r.Owner = "other"
			case "identity":
				r.Target.CredentialIdentity = "other"
			case "root":
				in.Root = "relative"
			case "workspace":
				in.Workspace = in.Root
			}
			a, e := NewAdapter(c)
			if e != nil {
				t.Fatal(e)
			}
			if h, e := a.Start(context.Background(), r, in); h != nil || e == nil {
				t.Fatal("unsafe launch accepted")
			}
			b, e := c.Scheduler.Store.Budget(r.TaskID)
			if e != nil || b.UsedCalls != 0 || calls.Load() != 0 {
				t.Fatal("rejection spent a call")
			}
			if _, e := os.Stat(filepath.Join(in.Root, "launch.json")); !os.IsNotExist(e) {
				t.Fatal("rejection launched")
			}
		})
	}
}
func TestCodexAdapterNoUnverifiedResumeOutputOrStop(t *testing.T) {
	c, r, _, _, _, _ := adapterFixture(t)
	a, e := NewAdapter(c)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.Probe(context.Background()); e == nil {
		t.Fatal("missing pin accepted")
	}
	if h, e := a.Resume(context.Background(), r); h != nil || e != managed.ErrUnsupported {
		t.Fatal("resume admitted")
	}
	if _, text, e := a.Observation(r.ID, r.Generation); e == nil || text != "" {
		t.Fatal("early output exposed")
	}
	if a.VerifyStop(policy.StopProof{}) {
		t.Fatal("foreign proof accepted")
	}
}

func TestCodexAdapterPrivateOutputAndFormatting(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{`{"result":{"value":"fixture"}}`, true},
		{`{"params":{"item":{"text":"private-stage-marker"}}}`, false},
		{`{"result":{"value":"private-stage-\u006darker"}}`, false},
		{`{"private-stage-marker":"value"}`, false},
		{"not JSON", false},
		{"{}\nnot JSON", false},
	} {
		if safeOutput([]byte(tc.raw), "private-stage-marker") != tc.want {
			t.Fatal("private output projection disagrees")
		}
	}
	c, _, _, _, _, _ := adapterFixture(t)
	a, e := NewAdapter(c)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(fmt.Sprintf("%v %#v", a, a), c.Executable) {
		t.Fatal("private adapter formatted")
	}
	if _, e := json.Marshal(a); e == nil {
		t.Fatal("private adapter serialized")
	}
}

func TestCodexAdapterPreflightRequiresExactTargetAndCurrentSource(t *testing.T) {
	for _, mode := range []string{"valid", "role", "version", "model", "billing", "plugin", "effort", "identity", "context", "callback_source"} {
		t.Run(mode, func(t *testing.T) {
			c, r, in, _, _, origin := adapterFixture(t)
			role := r.Role
			target := cloneTarget(r.Target)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "role":
				role = "unknown"
			case "version":
				target.RuntimeVersion = "other"
			case "model":
				target.ResolvedModel = "other"
			case "billing":
				target.BillingPath = "api"
			case "plugin":
				x := "other"
				target.PluginVersion = &x
			case "effort":
				target.Effort.Value = nil
			case "identity":
				c.Identity = func(stageplan.ExecutionTarget) codex.Identity { return codex.Identity{} }
			case "context":
				cancel()
			case "callback_source":
				old := c.Identity
				c.Identity = func(x stageplan.ExecutionTarget) codex.Identity {
					os.WriteFile(filepath.Join(origin, "fixture.txt"), []byte("changed"), 0600)
					return old(x)
				}
			}
			a, e := NewAdapter(c)
			if e != nil {
				t.Fatal(e)
			}
			e = a.ValidateLaunch(ctx, role, target, in)
			if (e == nil) != (mode == "valid") {
				t.Fatal("invalid preflight result", mode, e)
			}
		})
	}
}

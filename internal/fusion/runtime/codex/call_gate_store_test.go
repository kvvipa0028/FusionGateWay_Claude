package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/quota"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

// Actual persistence and Scheduler transactions; route/quota/transport inspection
// is synthetic and cannot admit a production subscription or Native all-calls.
func storedGateFixture(t *testing.T, max int) (*gateFixture, *store.Store, *policy.Inspection) {
	t.Helper()
	f := newGateFixture(t)
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(root, 0700); e != nil {
		t.Fatal(e)
	}
	s, e := store.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	target := f.config.Binding.Target
	route := stageplan.Route{ID: target.Route.ID, Revision: 1, Model: target.ResolvedModel, Account: target.Account, Workspace: target.Workspace, CredentialIdentity: target.CredentialIdentity, RuntimeVersion: CLIVersion, BillingPath: "subscription", BillingKnown: true, Admitted: true, Efforts: []string{"high"}, Capabilities: []string{"text"}, LockEnforcement: stageplan.ControlledCalls}
	role := stageplan.Design
	plan, e := stageplan.Compile(1, []stageplan.Role{role}, stageplan.Layer{Roles: map[stageplan.Role]stageplan.Binding{role: {Mode: stageplan.Locked, Route: &target.Route, Model: target.ResolvedModel, Effort: &stageplan.EffortSelection{Mode: stageplan.EffortExplicit, Value: "high"}}}}, stageplan.Layer{}, stageplan.Layer{}, []stageplan.Route{route})
	if e != nil {
		t.Fatal(e)
	}
	task, e := s.Create("fixture-codex-gate", store.CreateRequest{ProjectID: "fixture-project", Goal: "fixture", Plan: plan})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConfigureBudget(task.ID, store.Budget{MaxCalls: max}); e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	used := float64(10)
	in := &policy.Inspection{Route: route, Provider: "fixture-provider", QueryAdmitted: true, DataAllowed: true, SandboxVerified: true, VerificationAvailable: true, AllCallsCounted: true, ManagedExecutions: 1, ProofHash: strings.Repeat("a", 64), Quota: quota.Snapshot{Identity: quota.Identity{Provider: "fixture-provider", Account: route.Account, Workspace: route.Workspace, Region: "fixture-region", Generation: 1}, Source: "fixture-source", ObservedAt: &now, ReceivedAt: now, Complete: true, Status: quota.Available, Pool: quota.Pool{ID: "fixture-pool", Provider: "fixture-provider", Region: "fixture-region", Scope: "account", Owner: route.Account, Verified: true}, Windows: []quota.Window{{Kind: "subscription", Unit: "percent", UsedPercent: &used}}}}
	scheduler := &policy.Scheduler{Store: s, Inspect: func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (policy.Inspection, error) {
		return *in, nil
	}}
	run, e := scheduler.Prepare(context.Background(), store.StartRequest{TaskID: task.ID, Role: role, PlanRevision: 1, Owner: "fixture-owner", TTL: time.Minute, Target: *plan.Bindings[role].Target})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConfirmStarted(run.ID, run.Generation, run.Owner, "fixture-native-session"); e != nil {
		t.Fatal(e)
	}
	f.config.Binding.Scope.RunID = run.ID
	f.config.Binding.Scope.Generation = run.Generation
	f.config.Binding.Target = run.Target
	f.config.Claims = policy.Claims{TaskID: task.ID, RunID: run.ID, Role: run.Role, Attempt: run.Attempt, PlanRevision: run.PlanRevision, Generation: run.Generation, ProjectID: task.ProjectID, Audience: policy.ModelAudience}
	f.manager = policy.NewManager("fixture-management", policy.StoreValidator(s), nil)
	f.config.Manager = f.manager
	f.token, e = f.manager.Issue(f.config.Claims, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	f.config.Permit = func(ctx context.Context, c policy.Claims, target stageplan.ExecutionTarget, n int) error {
		f.permits.Add(1)
		return scheduler.Permit(ctx, c, target, n)
	}
	return f, s, in
}
func TestCodexCallGatePersistentBudgetCountsFailedAttemptAndRetry(t *testing.T) {
	f, s, _ := storedGateFixture(t, 2)
	f.config.Forwarder = gateForward(func(context.Context, stageplan.ExecutionTarget, []byte) (ForwardResponse, error) {
		if f.calls.Add(1) == 1 {
			r := gateResponse("private rate limit")
			r.StatusCode = 429
			return r, nil
		}
		return gateResponse(gateSSE()), nil
	})
	g := newGate(t, f)
	for i, want := range []int{429, 200, 403} {
		if w := send(g, f, gateBody, "/responses"); w.Code != want {
			t.Fatal(i, w.Code, g.Audit())
		}
	}
	b, e := s.Budget(f.config.Claims.TaskID)
	if e != nil || b.UsedCalls != 2 || b.MaxCalls != 2 || f.calls.Load() != 2 || f.permits.Load() != 3 || gateHealthy(g, f) {
		t.Fatal(b, e, g.Audit())
	}
}
func TestCodexCallGateRechecksPersistentInspection(t *testing.T) {
	for _, mode := range []string{"quota", "permission", "proof"} {
		t.Run(mode, func(t *testing.T) {
			f, s, in := storedGateFixture(t, 3)
			g := newGate(t, f)
			if w := send(g, f, gateBody, "/responses"); w.Code != 200 {
				t.Fatal(w.Code)
			}
			switch mode {
			case "quota":
				in.Quota.Status = quota.Unknown
			case "permission":
				in.DataAllowed = false
			case "proof":
				in.ProofHash = strings.Repeat("b", 64)
			}
			w := send(g, f, gateBody, "/responses")
			b, e := s.Budget(f.config.Claims.TaskID)
			if w.Code < 400 || e != nil || b.UsedCalls != 1 || f.calls.Load() != 1 || gateHealthy(g, f) {
				t.Fatal(w.Code, b, e, g.Audit())
			}
		})
	}
}

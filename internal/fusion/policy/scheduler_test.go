package policy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/yetone/magpie/internal/fusion/quota"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
	"testing"
	"time"
)

func proofHash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func inspection(f *dispatchFixture) Inspection {
	now := time.Now()
	used := float64(20)
	return Inspection{Route: f.route, Provider: "fixture-provider", QueryAdmitted: true, DataAllowed: true, SandboxVerified: true, VerificationAvailable: true, AllCallsCounted: true, ManagedExecutions: 1, ProofHash: proofHash("fixture-admission"), Quota: quota.Snapshot{Identity: quota.Identity{Provider: "fixture-provider", Account: f.route.Account, Workspace: f.route.Workspace, Region: "fixture-region", Generation: 1}, Source: "fixture-source", ObservedAt: &now, ReceivedAt: now, Complete: true, Status: quota.Available, Pool: quota.Pool{ID: "fixture-pool", Provider: "fixture-provider", Region: "fixture-region", Scope: "account", Owner: f.route.Account, Verified: true}, Windows: []quota.Window{{Kind: "subscription", Unit: "percent", UsedPercent: &used}}}}
}
func TestSchedulerCallGateRejectsUnknownQuotaBillingPermissionsAndVerification(t *testing.T) {
	for _, change := range []string{"quota", "stale", "billing", "permission", "sandbox", "verification", "subagents", "query", "identity"} {
		t.Run(change, func(t *testing.T) {
			f := newDispatchFixture(t)
			env := inspection(f)
			switch change {
			case "quota":
				env.Quota.Status = quota.Unknown
			case "stale":
				old := time.Now().Add(-time.Hour)
				env.Quota.ObservedAt = &old
			case "billing":
				env.Route.BillingKnown = false
			case "permission":
				env.DataAllowed = false
			case "sandbox":
				env.SandboxVerified = false
			case "verification":
				env.VerificationAvailable = false
			case "subagents":
				env.ManagedExecutions = 2
			case "query":
				env.QueryAdmitted = false
			case "identity":
				env.Quota.Identity.Account = "fixture-other"
			}
			scheduler := Scheduler{Store: f.s, Inspect: func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (Inspection, error) {
				return env, nil
			}}
			if e := scheduler.Permit(context.Background(), f.c, f.target, 1); e == nil {
				t.Fatal("unsafe gate allowed")
			} else {
				var b *Blocker
				expected := map[string]string{"quota": "quota_unknown", "stale": "quota_stale", "billing": "route_changed", "permission": "data_permission_denied", "sandbox": "sandbox_unverified", "verification": "verification_unavailable", "subagents": "internal_execution_uncontrolled", "query": "quota_query_not_admitted", "identity": "quota_identity_mismatch"}
				if !errors.As(e, &b) || b.Code != expected[change] {
					t.Fatalf("wrong blocker: %v", e)
				}
			}
			run, _ := f.s.Run(f.c.RunID)
			if run.Target.ResolvedModel != f.target.ResolvedModel {
				t.Fatal("fallback target")
			}
		})
	}
}
func TestSchedulerPrepareRequiresBudgetAndNoImplicitFallback(t *testing.T) {
	f := newDispatchFixture(t)
	env := inspection(f)
	scheduler := Scheduler{Store: f.s, Inspect: func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (Inspection, error) {
		return env, nil
	}}
	_, e := scheduler.Prepare(context.Background(), store.StartRequest{TaskID: f.c.TaskID, Role: f.c.Role, PlanRevision: 1, Owner: "fixture-owner", TTL: time.Minute, Target: f.target})
	if e == nil {
		t.Fatal("running task admitted")
	}
	var blocker *Blocker
	if !errors.As(e, &blocker) {
		t.Fatal("structured blocker missing")
	}
}
func TestConcurrentDispatchesCannotRunSameStageInParallel(t *testing.T) {
	f := newDispatchFixture(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	calls := 0
	d := Dispatcher{Manager: f.m, Store: f.s, Lookup: func(stageplan.RouteRef) (DispatchRoute, error) {
		return DispatchRoute{Route: f.route, TransportID: "fixture-transport", Executor: fixtureExecutor{func(context.Context, Invocation) (Reply, error) {
			calls++
			close(entered)
			<-release
			return Reply{Text: "fixture"}, nil
		}}}, nil
	}, Permit: func(context.Context, Claims, stageplan.ExecutionTarget, int) error { return nil }}
	ctx := f.context(t)
	go func() {
		_, e := d.Dispatch(ctx, DispatchRequest{Messages: []Message{{Role: "user", Content: "fixture"}}}, 1)
		done <- e
	}()
	<-entered
	if _, e := d.Dispatch(ctx, DispatchRequest{Messages: []Message{{Role: "user", Content: "fixture"}}}, 1); !errors.Is(e, ErrDispatchBusy) {
		t.Errorf("second call admitted: %v", e)
	}
	close(release)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if calls != 1 {
		t.Fatal(calls)
	}
}

func TestSchedulerPrepareSpendAndVerifiedReleaseUseOnePersistentContract(t *testing.T) {
	f := newDispatchFixture(t)
	p, e := f.s.Plan(f.c.TaskID, 1)
	if e != nil {
		t.Fatal(e)
	}
	task, e := f.s.Create("fixture-scheduled", store.CreateRequest{ProjectID: "fixture-project-two", Goal: "fixture", Plan: p})
	if e != nil {
		t.Fatal(e)
	}
	env := inspection(f)
	scheduler := Scheduler{Store: f.s, Inspect: func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (Inspection, error) {
		return env, nil
	}, VerifyStop: func(p StopProof) bool { return p.ReportHash == proofHash("fixture-stop") }}
	request := store.StartRequest{TaskID: task.ID, Role: stageplan.Design, PlanRevision: 1, Owner: "fixture-worker", TTL: time.Minute, Target: f.target}
	if _, e = scheduler.Prepare(context.Background(), request); e == nil {
		t.Fatal("missing budget accepted")
	}
	if e = f.s.ConfigureBudget(task.ID, store.Budget{MaxCalls: 2, MaxReworks: 1}); e != nil {
		t.Fatal(e)
	}
	run, e := scheduler.Prepare(context.Background(), request)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.ConfirmStarted(run.ID, run.Generation, run.Owner, "fixture-native"); e != nil {
		t.Fatal(e)
	}
	claims := Claims{TaskID: task.ID, RunID: run.ID, Role: run.Role, Attempt: run.Attempt, PlanRevision: run.PlanRevision, Generation: run.Generation, ProjectID: task.ProjectID, Audience: ModelAudience}
	for i := 1; i <= 2; i++ {
		if e = scheduler.Permit(context.Background(), claims, f.target, 1); e != nil {
			t.Fatal(e)
		}
	}
	if e = scheduler.Permit(context.Background(), claims, f.target, 1); e == nil {
		t.Fatal("shared budget bypassed")
	}
	stop := StopProof{RunID: run.ID, Generation: run.Generation, NativeSessionID: "fixture-native", ProcessIdentityHash: proofHash("fixture-process"), ReportHash: proofHash("fixture-stop"), DescendantsStopped: true}
	if e = scheduler.Release(stop); e == nil {
		t.Fatal("active process slot released")
	}
	f.s.Finish(run.ID, run.Generation, run.Owner, "succeeded")
	bad := stop
	bad.NativeSessionID = "fixture-other"
	if e = scheduler.Release(bad); e == nil {
		t.Fatal("wrong native identity released")
	}
	if e = scheduler.Release(stop); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Reservation(run.ID); e == nil {
		t.Fatal("reservation retained")
	}
	budget, _ := f.s.Budget(task.ID)
	if budget.UsedCalls != 2 {
		t.Fatal("release refunded calls")
	}
}

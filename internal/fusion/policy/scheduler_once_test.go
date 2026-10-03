package policy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/quota"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

func onceScheduleFixture(t *testing.T) (*Scheduler, store.StartRequest, *Inspection, string) {
	t.Helper()
	f := newDispatchFixture(t)
	plan, e := f.s.Plan(f.c.TaskID, 1)
	if e != nil {
		t.Fatal(e)
	}
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
	task, e := s.Create("fixture-once-schedule", store.CreateRequest{ProjectID: "fixture-project", Goal: "fixture", Plan: plan, Budget: &store.Budget{MaxCalls: 1}})
	if e != nil {
		t.Fatal(e)
	}
	env := inspection(f)
	gen := task.Generation
	scheduler := &Scheduler{Store: s, Inspect: func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (Inspection, error) {
		return env, nil
	}}
	in := store.StartRequest{TaskID: task.ID, Role: stageplan.Design, PlanRevision: 1, Owner: "fixture-owner", TTL: time.Minute, Target: f.target, IdempotencyKey: "fixture-once-schedule-key", ExpectedGeneration: &gen}
	return scheduler, in, &env, root
}

func requireBlock(t *testing.T, e error, code string) {
	t.Helper()
	var b *Blocker
	if !errors.As(e, &b) || b.Code != code {
		t.Fatalf("want %s, got %v", code, e)
	}
}

func TestPrepareOnceRetryDoesNotInspectOrSpendAfterTerminal(t *testing.T) {
	s, in, _, _ := onceScheduleFixture(t)
	first, e := s.PrepareOnce(context.Background(), in)
	if e != nil || !first.Created {
		t.Fatal(e)
	}
	r := first.Run
	if e = s.Store.ConfirmStarted(r.ID, r.Generation, r.Owner, "fixture-session"); e != nil {
		t.Fatal(e)
	}
	if e = s.Store.ReserveCall(r.ID, r.Generation); e != nil {
		t.Fatal(e)
	}
	if e = s.Store.Finish(r.ID, r.Generation, r.Owner, "succeeded"); e != nil {
		t.Fatal(e)
	}
	before, e := s.Store.Events(in.TaskID, 0)
	if e != nil {
		t.Fatal(e)
	}
	s.Inspect = func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (Inspection, error) {
		t.Error("retry re-entered admission")
		return Inspection{}, errors.New("fixture unavailable")
	}
	in.Owner = "fixture-new-owner"
	got, e := s.PrepareOnce(context.Background(), in)
	if e != nil || got.Created || got.Run.ID != r.ID || got.Run.State != "succeeded" {
		t.Fatal("retry replayed instead of reading", e)
	}
	b, e := s.Store.Budget(in.TaskID)
	if e != nil || b.UsedCalls != 1 {
		t.Fatal("retry changed consumed budget", e)
	}
	after, e := s.Store.Events(in.TaskID, 0)
	if e != nil || len(before) != len(after) {
		t.Fatal("retry wrote an event", e)
	}
	if _, e = s.Store.Reservation(r.ID); e != nil {
		t.Fatal("terminal retry released held capacity without stop proof", e)
	}
}

func TestPrepareOnceRecoveryReadsUnknownWithoutAdmission(t *testing.T) {
	s, in, _, root := onceScheduleFixture(t)
	first, e := s.PrepareOnce(context.Background(), in)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.Close(); e != nil {
		t.Fatal(e)
	}
	reopened, e := store.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	s.Store, s.Inspect = reopened, nil
	got, e := s.PrepareOnce(context.Background(), in)
	if e != nil || got.Created || got.Run.ID != first.Run.ID || got.Run.State != "unknown" {
		t.Fatal("restart replayed or required new admission", e)
	}
}

func TestPrepareOncePayloadConflictsAndInvalidRequestsDoNotInspect(t *testing.T) {
	s, in, _, _ := onceScheduleFixture(t)
	if _, e := s.PrepareOnce(context.Background(), in); e != nil {
		t.Fatal(e)
	}
	s.Inspect = func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (Inspection, error) {
		t.Error("invalid/conflicting request reached admission")
		return Inspection{}, nil
	}
	for _, change := range []string{"target", "task", "revision", "role", "generation"} {
		bad := in
		switch change {
		case "target":
			bad.Target.ResolvedModel = "fixture-other-model"
		case "task":
			bad.TaskID = "fixture-other-task"
		case "revision":
			bad.PlanRevision++
		case "role":
			bad.Role = stageplan.Review
		case "generation":
			gen := int64(1)
			bad.ExpectedGeneration = &gen
		}
		_, e := s.PrepareOnce(context.Background(), bad)
		requireBlock(t, e, "start_request_conflict")
	}
	for _, change := range []string{"key", "generation", "negative", "role"} {
		bad := in
		switch change {
		case "key":
			bad.IdempotencyKey = ""
		case "generation":
			bad.ExpectedGeneration = nil
		case "negative":
			gen := int64(-1)
			bad.ExpectedGeneration = &gen
		case "role":
			bad.Role = "unsupported"
		}
		_, e := s.PrepareOnce(context.Background(), bad)
		requireBlock(t, e, "start_request_invalid")
	}
	_, e := s.Prepare(context.Background(), in)
	requireBlock(t, e, "start_request_invalid")
}

func TestPrepareOnceNewRequestStillRequiresCurrentQuotaAndGeneration(t *testing.T) {
	s, in, env, _ := onceScheduleFixture(t)
	env.Quota.Status = quota.Unknown
	_, e := s.PrepareOnce(context.Background(), in)
	requireBlock(t, e, "quota_unknown")
	identity := store.StartIdentity{TaskID: in.TaskID, Role: in.Role, PlanRevision: in.PlanRevision, Generation: *in.ExpectedGeneration}
	if _, e = s.Store.LookupStart(in.IdempotencyKey, identity); !errors.Is(e, store.ErrNotFound) {
		t.Fatal("blocked admission consumed the start key", e)
	}
	env.Quota.Status = quota.Available
	wrong := int64(1)
	bad := in
	bad.ExpectedGeneration = &wrong
	_, e = s.PrepareOnce(context.Background(), bad)
	requireBlock(t, e, "task_not_ready")
	first, e := s.PrepareOnce(context.Background(), in)
	if e != nil || !first.Created || first.Run.Generation != 1 {
		t.Fatal("refused requests changed generation", e)
	}
}

func TestPrepareOnceConcurrentRequestsHaveOneLaunchWinner(t *testing.T) {
	s, in, env, _ := onceScheduleFixture(t)
	var inspected atomic.Int64
	s.Inspect = func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (Inspection, error) {
		inspected.Add(1)
		return *env, nil
	}
	var wg sync.WaitGroup
	results := make(chan store.StartReceipt, 16)
	for n := 0; n < 16; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, e := s.PrepareOnce(context.Background(), in)
			if e != nil {
				t.Error(e)
				return
			}
			results <- got
		}()
	}
	wg.Wait()
	close(results)
	winners, received := 0, 0
	var runID string
	for got := range results {
		received++
		if runID == "" {
			runID = got.Run.ID
		}
		if got.Run.ID != runID {
			t.Fatal("multiple intents")
		}
		if got.Created {
			winners++
		}
	}
	if received != 16 || winners != 1 || inspected.Load() < 1 {
		t.Fatal("retry returned an error or multiple launch permissions", received, winners)
	}
}

func TestPrepareOnceLosingInspectionReadsCommittedWinner(t *testing.T) {
	s, in, env, _ := onceScheduleFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	s.Inspect = func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (Inspection, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
			return Inspection{}, errors.New("fixture unavailable after another request committed")
		}
		return *env, nil
	}
	result := make(chan store.StartReceipt, 1)
	failure := make(chan error, 1)
	go func() {
		got, e := s.PrepareOnce(context.Background(), in)
		result <- got
		failure <- e
	}()
	<-entered
	winner, e := s.PrepareOnce(context.Background(), in)
	close(release)
	loser, le := <-result, <-failure
	if e != nil || !winner.Created || le != nil || loser.Created || loser.Run.ID != winner.Run.ID {
		t.Fatal("concurrent committed request lost its read-only receipt", e, le)
	}
}

package store

import (
	"errors"
	"testing"
	"time"
)

func TestSubmissionBudgetIsAtomicWithTaskSnapshotAndIdempotency(t *testing.T) {
	s, _ := openFixture(t)
	if _, e := s.db.Exec(`CREATE TRIGGER fail_submission_budget BEFORE INSERT ON task_budgets BEGIN SELECT RAISE(ABORT,'fixture-budget-write-failure'); END;`); e != nil {
		t.Fatal(e)
	}
	in := CreateRequest{ProjectID: "fixture-project", Goal: "fixture-goal", Plan: plan(t, 1), Budget: &Budget{MaxCalls: 7, MaxReworks: 1}}
	if _, e := s.Create("fixture-key", in); e == nil {
		t.Fatal("failed budget write committed task")
	}
	for _, table := range []string{"tasks", "plan_revisions", "idempotency", "events", "task_budgets"} {
		var count int
		if e := s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); e != nil || count != 0 {
			t.Fatal("partial submission survived", table, count, e)
		}
	}
	if _, e := s.db.Exec("DROP TRIGGER fail_submission_budget"); e != nil {
		t.Fatal(e)
	}
	task, e := s.Create("fixture-key", in)
	if e != nil {
		t.Fatal(e)
	}
	got, e := s.Budget(task.ID)
	if e != nil || got != *in.Budget {
		t.Fatal("budget not frozen", got, e)
	}
	retry, e := s.Create("fixture-key", in)
	if e != nil || retry.ID != task.ID {
		t.Fatal("retry duplicated task", retry, e)
	}
	changed := in
	changed.Budget = &Budget{MaxCalls: 8, MaxReworks: 1}
	if _, e = s.Create("fixture-key", changed); !errors.Is(e, ErrConflict) {
		t.Fatal("changed budget reused idempotency key", e)
	}
}
func TestSubmissionBudgetRejectsUsedCountersAndInvalidLimits(t *testing.T) {
	s, _ := openFixture(t)
	for _, b := range []Budget{{MaxCalls: 0}, {MaxCalls: 1001}, {MaxCalls: 7, MaxReworks: 2}, {MaxCalls: 7, MaxReworks: -1}, {MaxCalls: 7, UsedCalls: 1}, {MaxCalls: 7, MaxReworks: 1, UsedReworks: 1}} {
		if _, e := s.Create("fixture-key", CreateRequest{ProjectID: "fixture-project", Goal: "fixture-goal", Plan: plan(t, 1), Budget: &b}); !errors.Is(e, ErrInvalid) {
			t.Fatal("invalid initial budget accepted", b, e)
		}
	}
}
func TestSubmissionRetryNeverResetsAlreadyConsumedBudget(t *testing.T) {
	s, _ := openFixture(t)
	in := CreateRequest{ProjectID: "fixture-project", Goal: "fixture-goal", Plan: plan(t, 1), Budget: &Budget{MaxCalls: 7, MaxReworks: 1}}
	task, e := s.Create("fixture-key", in)
	if e != nil {
		t.Fatal(e)
	}
	run, e := s.StartReserved(StartRequest{TaskID: task.ID, Role: in.Plan.RequiredRoles[0], PlanRevision: 1, Owner: "fixture-worker", TTL: time.Minute, Target: *in.Plan.Bindings[in.Plan.RequiredRoles[0]].Target}, ReservationRequest{PoolKey: "fixture-pool", AdmissionHash: hash([]byte("fixture-proof")), GlobalLimit: 2})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConfirmStarted(run.ID, run.Generation, run.Owner, "fixture-session"); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 3; i++ {
		if e = s.ReserveCall(run.ID, run.Generation); e != nil {
			t.Fatal(e)
		}
	}
	if e = s.ReserveRework(task.ID, run.Generation); e != nil {
		t.Fatal(e)
	}
	before, e := s.Events(task.ID, 0)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Create("fixture-key", in); e != nil {
		t.Fatal(e)
	}
	b, e := s.Budget(task.ID)
	if e != nil || b.UsedCalls != 3 || b.UsedReworks != 1 {
		t.Fatal("retry refunded consumed budget", b, e)
	}
	after, e := s.Events(task.ID, 0)
	if e != nil || len(before) != len(after) {
		t.Fatal("submission retry produced execution events", e)
	}
}

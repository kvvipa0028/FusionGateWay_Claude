package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func scheduledTask(t *testing.T, s *Store, key string) (Task, StartRequest) {
	t.Helper()
	p := plan(t, 1)
	task, e := s.Create(key, CreateRequest{ProjectID: key, Goal: "fixture", Plan: p})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConfigureBudget(task.ID, Budget{MaxCalls: 2, MaxReworks: 1}); e != nil {
		t.Fatal(e)
	}
	return task, StartRequest{TaskID: task.ID, Role: p.RequiredRoles[0], PlanRevision: 1, Owner: "fixture-worker", TTL: time.Minute, Target: *p.Bindings[p.RequiredRoles[0]].Target}
}
func TestScheduledPoolGlobalAndWriterReservationsAreAtomic(t *testing.T) {
	s, _ := openFixture(t)
	_, one := scheduledTask(t, s, "fixture-one")
	_, two := scheduledTask(t, s, "fixture-two")
	_, three := scheduledTask(t, s, "fixture-three")
	req := ReservationRequest{PoolKey: "fixture-pool-a", WriteKey: "fixture-writer-a", GlobalLimit: 2, AdmissionHash: hash([]byte("fixture-proof"))}
	r, e := s.StartReserved(one, req)
	if e != nil {
		t.Fatal(e)
	}
	for _, bad := range []ReservationRequest{req, {PoolKey: "fixture-pool-b", WriteKey: req.WriteKey, GlobalLimit: 2, AdmissionHash: req.AdmissionHash}} {
		if _, e = s.StartReserved(two, bad); !errors.Is(e, ErrConflict) {
			t.Fatal(e)
		}
		task, _ := s.Task(two.TaskID)
		if task.State != "ready" {
			t.Fatal("failed reservation committed intent")
		}
	}
	req.PoolKey = "fixture-pool-b"
	req.WriteKey = "fixture-writer-b"
	if _, e = s.StartReserved(two, req); e != nil {
		t.Fatal(e)
	}
	req.PoolKey = "fixture-pool-c"
	req.WriteKey = ""
	if _, e = s.StartReserved(three, req); !errors.Is(e, ErrConflict) {
		t.Fatal("global limit", e)
	}
	if e = s.ReleaseReserved(r.ID, r.Generation, "", false); e == nil {
		t.Fatal("unproved process stop released writer")
	}
	if e = s.Finish(r.ID, r.Generation, r.Owner, "failed"); e != nil {
		t.Fatal(e)
	}
	if e = s.ReleaseReserved(r.ID, r.Generation, hash([]byte("fixture-stop")), true); e != nil {
		t.Fatal(e)
	}
	if _, e = s.StartReserved(three, req); e != nil {
		t.Fatal(e)
	}
}
func TestSharedCallAndReworkBudgetDoesNotResetAtAttempt(t *testing.T) {
	s, _ := openFixture(t)
	task, start := scheduledTask(t, s, "fixture-task")
	r, e := s.StartReserved(start, ReservationRequest{PoolKey: "fixture-pool", GlobalLimit: 2, AdmissionHash: hash([]byte("proof"))})
	if e != nil {
		t.Fatal(e)
	}
	s.ConfirmStarted(r.ID, r.Generation, r.Owner, "fixture-session")
	for n := 0; n < 2; n++ {
		if e = s.ReserveCall(r.ID, r.Generation); e != nil {
			t.Fatal(e)
		}
	}
	if e = s.ReserveCall(r.ID, r.Generation); !errors.Is(e, ErrBudget) {
		t.Fatal(e)
	}
	if e = s.ConfigureBudget(task.ID, Budget{MaxCalls: 99, MaxReworks: 1}); !errors.Is(e, ErrConflict) {
		t.Fatal("budget increased silently", e)
	}
	for n := 0; n < 1; n++ {
		if e = s.ReserveRework(task.ID, r.Generation); e != nil {
			t.Fatal(e)
		}
	}
	if e = s.ReserveRework(task.ID, r.Generation); !errors.Is(e, ErrBudget) {
		t.Fatal(e)
	}
	got, e := s.Budget(task.ID)
	if e != nil || got.UsedCalls != 2 || got.UsedReworks != 1 {
		t.Fatalf("%+v %v", got, e)
	}
}
func TestConcurrentPoolStartsAndBudgetCallsHaveOneWinner(t *testing.T) {
	s, _ := openFixture(t)
	_, a := scheduledTask(t, s, "fixture-a")
	_, b := scheduledTask(t, s, "fixture-b")
	req := ReservationRequest{PoolKey: "fixture-pool", GlobalLimit: 2, AdmissionHash: hash([]byte("proof"))}
	ch := make(chan StageRun, 2)
	var wg sync.WaitGroup
	for _, start := range []StartRequest{a, b} {
		wg.Add(1)
		go func(in StartRequest) {
			defer wg.Done()
			r, e := s.StartReserved(in, req)
			if e == nil {
				ch <- r
			} else if !errors.Is(e, ErrConflict) {
				t.Error(e)
			}
		}(start)
	}
	wg.Wait()
	close(ch)
	var run StageRun
	n := 0
	for r := range ch {
		run = r
		n++
	}
	if n != 1 {
		t.Fatal(n)
	}
	s.ConfirmStarted(run.ID, run.Generation, run.Owner, "fixture-session")
	errs := make(chan error, 8)
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- s.ReserveCall(run.ID, run.Generation) }()
	}
	wg.Wait()
	close(errs)
	pass := 0
	for e := range errs {
		if e == nil {
			pass++
		} else if !errors.Is(e, ErrBudget) {
			t.Fatal(e)
		}
	}
	if pass != 2 {
		t.Fatal(pass)
	}
}
func TestReservationAndBudgetSurviveUnknownRecovery(t *testing.T) {
	s, root := openFixture(t)
	_, in := scheduledTask(t, s, "fixture-old")
	r, e := s.StartReserved(in, ReservationRequest{PoolKey: "fixture-pool", GlobalLimit: 2, AdmissionHash: hash([]byte("proof"))})
	if e != nil {
		t.Fatal(e)
	}
	s.ConfirmStarted(r.ID, r.Generation, r.Owner, "fixture-session")
	s.ReserveCall(r.ID, r.Generation)
	s.Close()
	s, e = Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	old, _ := s.Run(r.ID)
	if old.State != "unknown" {
		t.Fatal(old.State)
	}
	_, next := scheduledTask(t, s, "fixture-new")
	if _, e = s.StartReserved(next, ReservationRequest{PoolKey: "fixture-pool", GlobalLimit: 2, AdmissionHash: hash([]byte("proof"))}); !errors.Is(e, ErrConflict) {
		t.Fatal("unknown freed pool", e)
	}
	budget, _ := s.Budget(r.TaskID)
	if budget.UsedCalls != 1 {
		t.Fatal("budget reset")
	}
	if e = s.ReleaseReserved(r.ID, old.Generation, hash([]byte("stop")), true); e == nil {
		t.Fatal("unknown released without reconciliation")
	}
}
func TestMigrationTwoPreservesVerifiedVersionOneDatabase(t *testing.T) {
	root := t.TempDir()
	os.Chmod(root, 0700)
	root, _ = filepath.EvalSymlinks(root)
	path := filepath.Join(root, "fusion.db")
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	f.Close()
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(migration); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("INSERT INTO metadata VALUES('migration_001_sha256',?)", hash([]byte(migration))); e != nil {
		t.Fatal(e)
	}
	db.Close()
	s, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var version int
	s.db.QueryRow("PRAGMA user_version").Scan(&version)
	if version != 4 {
		t.Fatal(version)
	}
}

func TestFinishedProtocolCannotFreeSameTaskBeforeProcessStop(t *testing.T) {
	s, _ := openFixture(t)
	_, in := scheduledTask(t, s, "fixture-task")
	r, e := s.StartReserved(in, ReservationRequest{PoolKey: "fixture-pool-one", GlobalLimit: 2, AdmissionHash: hash([]byte("proof"))})
	if e != nil {
		t.Fatal(e)
	}
	s.ConfirmStarted(r.ID, r.Generation, r.Owner, "fixture-session")
	s.Finish(r.ID, r.Generation, r.Owner, "succeeded")
	if _, e = s.StartReserved(in, ReservationRequest{PoolKey: "fixture-pool-two", GlobalLimit: 2, AdmissionHash: hash([]byte("proof"))}); !errors.Is(e, ErrConflict) {
		t.Error("same task resumed before verified stop", e)
	}
}
func TestAdvisoryAndInterruptedTerminalsCanReleaseWithStopProof(t *testing.T) {
	for _, outcome := range []string{"advisory_only", "interrupted"} {
		t.Run(outcome, func(t *testing.T) {
			s, _ := openFixture(t)
			_, in := scheduledTask(t, s, "fixture-task")
			r, e := s.StartReserved(in, ReservationRequest{PoolKey: "fixture-pool", GlobalLimit: 2, AdmissionHash: hash([]byte("proof"))})
			if e != nil {
				t.Fatal(e)
			}
			s.Finish(r.ID, r.Generation, r.Owner, outcome)
			if e = s.ReleaseReserved(r.ID, r.Generation, hash([]byte("stop")), true); e != nil {
				t.Error(e)
			}
		})
	}
}

func TestReservationAndCallEventFailuresRollbackAllEffects(t *testing.T) {
	s, _ := openFixture(t)
	task, in := scheduledTask(t, s, "fixture-task")
	s.db.Exec("CREATE TRIGGER fixture_fail_reservation BEFORE INSERT ON events WHEN NEW.kind='reservation_held' BEGIN SELECT RAISE(ABORT,'fixture failure'); END")
	req := ReservationRequest{PoolKey: "fixture-pool", GlobalLimit: 2, AdmissionHash: hash([]byte("proof"))}
	if _, e := s.StartReserved(in, req); e == nil {
		t.Fatal("event failure hidden")
	}
	fresh, _ := s.Task(task.ID)
	if fresh.State != "ready" || fresh.Generation != 0 {
		t.Fatal("intent partly committed")
	}
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM reservations").Scan(&count)
	if count != 0 {
		t.Fatal("reservation partly committed")
	}
	s.db.Exec("DROP TRIGGER fixture_fail_reservation")
	r, e := s.StartReserved(in, req)
	if e != nil {
		t.Fatal(e)
	}
	s.ConfirmStarted(r.ID, r.Generation, r.Owner, "fixture-session")
	s.db.Exec("CREATE TRIGGER fixture_fail_call BEFORE INSERT ON events WHEN NEW.kind='model_call_reserved' BEGIN SELECT RAISE(ABORT,'fixture failure'); END")
	if e = s.ReserveCall(r.ID, r.Generation); e == nil {
		t.Fatal("call event failure hidden")
	}
	b, _ := s.Budget(task.ID)
	if b.UsedCalls != 0 {
		t.Fatal("failed call permission spent budget")
	}
}
func TestCapacityCannotChangeWithHeldOrUnknownExecutions(t *testing.T) {
	s, _ := openFixture(t)
	if e := s.ConfigureCapacity(1); e != nil {
		t.Fatal(e)
	}
	_, in := scheduledTask(t, s, "fixture-task")
	if _, e := s.StartReserved(in, ReservationRequest{PoolKey: "fixture-pool", GlobalLimit: 1, AdmissionHash: hash([]byte("proof"))}); e != nil {
		t.Fatal(e)
	}
	if e := s.ConfigureCapacity(2); !errors.Is(e, ErrConflict) {
		t.Fatal("capacity raised while held", e)
	}
}

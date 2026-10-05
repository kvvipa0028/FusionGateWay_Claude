package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func onceFixture(t *testing.T, s *Store) (StartRequest, ReservationRequest) {
	t.Helper()
	task, in := scheduledTask(t, s, "fixture-once-task")
	in.IdempotencyKey = "fixture-start-key"
	gen := task.Generation
	in.ExpectedGeneration = &gen
	return in, ReservationRequest{PoolKey: "fixture-once-pool", GlobalLimit: 2, AdmissionHash: hash([]byte("fixture-proof"))}
}

func identityOf(in StartRequest) StartIdentity {
	return StartIdentity{TaskID: in.TaskID, Role: in.Role, PlanRevision: in.PlanRevision, Generation: *in.ExpectedGeneration}
}

func TestStartOnceConcurrentRetriesCreateOneIntentAndReservation(t *testing.T) {
	s, _ := openFixture(t)
	in, req := onceFixture(t, s)
	var wg sync.WaitGroup
	results := make(chan StartReceipt, 12)
	for n := 0; n < 12; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, e := s.StartReservedOnce(in, req)
			if e != nil {
				t.Error(e)
				return
			}
			results <- got
		}()
	}
	wg.Wait()
	close(results)
	created := 0
	var first StageRun
	for got := range results {
		if got.Created {
			created++
			first = got.Run
		}
		if got.Run.Attempt != 1 || got.Run.Generation != 1 {
			t.Fatal("retry advanced execution")
		}
	}
	if created != 1 {
		t.Fatal("multiple launch winners", created)
	}
	got, e := s.LookupStart(in.IdempotencyKey, identityOf(in))
	if e != nil || got.ID != first.ID {
		t.Fatal("persisted start not found")
	}
	var intents, reservations int
	s.db.QueryRow("SELECT COUNT(*) FROM stage_runs WHERE task_id=?", in.TaskID).Scan(&intents)
	s.db.QueryRow("SELECT COUNT(*) FROM reservations WHERE run_id=?", first.ID).Scan(&reservations)
	if intents != 1 || reservations != 1 {
		t.Fatal("duplicate durable side effects")
	}
	events, e := s.Events(in.TaskID, 0)
	if e != nil {
		t.Fatal(e)
	}
	starts := 0
	for _, e := range events {
		if e.Kind == "start_intent" {
			starts++
		}
	}
	if starts != 1 {
		t.Fatal("duplicate start events")
	}
}

func TestStartOnceReplayNeverReservesAgainOrRefundsConsumedBudget(t *testing.T) {
	s, _ := openFixture(t)
	in, req := onceFixture(t, s)
	first, e := s.StartReservedOnce(in, req)
	if e != nil || !first.Created {
		t.Fatal(e)
	}
	r := first.Run
	if e = s.ConfirmStarted(r.ID, r.Generation, r.Owner, "fixture-session"); e != nil {
		t.Fatal(e)
	}
	for n := 0; n < 2; n++ {
		if e = s.ReserveCall(r.ID, r.Generation); e != nil {
			t.Fatal(e)
		}
	}
	if e = s.Finish(r.ID, r.Generation, r.Owner, "succeeded"); e != nil {
		t.Fatal(e)
	}
	if e = s.ReleaseReserved(r.ID, r.Generation, hash([]byte("fixture-stop")), true); e != nil {
		t.Fatal(e)
	}
	events, _ := s.Events(in.TaskID, 0)
	req.PoolKey = "different-pool"
	in.Owner = "new-controller-owner"
	got, e := s.StartReservedOnce(in, req)
	if e != nil || got.Created || got.Run.ID != r.ID || got.Run.State != "succeeded" {
		t.Fatal("completed request replayed", e)
	}
	_, e = s.Reservation(r.ID)
	var state, pool string
	if err := s.db.QueryRow("SELECT state,pool_key FROM reservations WHERE run_id=?", r.ID).Scan(&state, &pool); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(e, ErrNotFound) || state != "released" || pool != "fixture-once-pool" {
		t.Fatal("replay rewrote reservation")
	}
	b, _ := s.Budget(in.TaskID)
	after, _ := s.Events(in.TaskID, 0)
	if b.UsedCalls != 2 || len(after) != len(events) {
		t.Fatal("replay refunded or wrote events")
	}
	if _, e = s.StartReserved(in, req); !errors.Is(e, ErrInvalid) {
		t.Fatal("legacy entry cannot distinguish replay and allowed a keyed request")
	}
}

func TestStartOnceGenerationAndPayloadConflictAreAtomic(t *testing.T) {
	s, _ := openFixture(t)
	in, req := onceFixture(t, s)
	wrong := int64(1)
	bad := in
	bad.ExpectedGeneration = &wrong
	if _, e := s.StartReservedOnce(bad, req); !errors.Is(e, ErrConflict) {
		t.Fatal("stale generation started", e)
	}
	task, _ := s.Task(in.TaskID)
	if task.Generation != 0 || task.State != "ready" {
		t.Fatal("conflict consumed generation")
	}
	got, e := s.StartReservedOnce(in, req)
	if e != nil || !got.Created {
		t.Fatal(e)
	}
	for _, mode := range []string{"task", "role", "revision", "generation", "target"} {
		bad := in
		switch mode {
		case "task":
			bad.TaskID = "different-task"
		case "role":
			bad.Role = "review"
		case "revision":
			bad.PlanRevision++
		case "generation":
			bad.ExpectedGeneration = &wrong
		case "target":
			bad.Target.CredentialIdentity = "different-credential"
		}
		if _, e := s.StartReservedOnce(bad, req); !errors.Is(e, ErrConflict) {
			t.Fatal("key adopted another request", mode, e)
		}
	}
	bad = in
	bad.IdempotencyKey = "new-key"
	if _, e := s.StartReservedOnce(bad, req); !errors.Is(e, ErrConflict) {
		t.Fatal("new key bypassed current state/generation", e)
	}
	if _, e := s.LookupStart(in.IdempotencyKey, StartIdentity{TaskID: in.TaskID, Role: in.Role, PlanRevision: 2, Generation: 0}); !errors.Is(e, ErrConflict) {
		t.Fatal("lookup adopted changed payload")
	}
}

func TestStartOnceRecoveryReturnsUnknownWithoutExecution(t *testing.T) {
	s, root := openFixture(t)
	in, req := onceFixture(t, s)
	first, e := s.StartReservedOnce(in, req)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConfirmStarted(first.Run.ID, first.Run.Generation, first.Run.Owner, "fixture-session"); e != nil {
		t.Fatal(e)
	}
	if e = s.ReserveCall(first.Run.ID, first.Run.Generation); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	before, _ := s.Events(in.TaskID, 0)
	got, e := s.StartReservedOnce(in, req)
	if e != nil || got.Created || got.Run.ID != first.Run.ID || got.Run.State != "unknown" {
		t.Fatal("recovery replayed launch", e)
	}
	var state string
	if e = s.db.QueryRow("SELECT state FROM reservations WHERE run_id=?", got.Run.ID).Scan(&state); e != nil {
		t.Fatal(e)
	}
	b, _ := s.Budget(in.TaskID)
	after, _ := s.Events(in.TaskID, 0)
	if state != "held" || b.UsedCalls != 1 || len(after) != len(before) {
		t.Fatal("unknown replay changed capacity or budget")
	}
}

func TestStartOnceStaleNewKeyCannotRestartCompletedTask(t *testing.T) {
	s, _ := openFixture(t)
	in, req := onceFixture(t, s)
	first, e := s.StartReservedOnce(in, req)
	if e != nil {
		t.Fatal(e)
	}
	r := first.Run
	if e = s.ConfirmStarted(r.ID, r.Generation, r.Owner, "fixture-session"); e != nil {
		t.Fatal(e)
	}
	if e = s.Finish(r.ID, r.Generation, r.Owner, "succeeded"); e != nil {
		t.Fatal(e)
	}
	if e = s.ReleaseReserved(r.ID, r.Generation, hash([]byte("fixture-stop")), true); e != nil {
		t.Fatal(e)
	}
	in.IdempotencyKey = "new-key-after-finish"
	if _, e = s.StartReservedOnce(in, req); !errors.Is(e, ErrConflict) {
		t.Fatal("old generation started a second attempt after completion", e)
	}
	if _, e = s.LookupStart(in.IdempotencyKey, identityOf(in)); !errors.Is(e, ErrNotFound) {
		t.Fatal("failed CAS consumed the new key", e)
	}
	gen := r.Generation
	in.ExpectedGeneration = &gen
	got, e := s.StartReservedOnce(in, req)
	if e != nil || !got.Created || got.Run.Attempt != 2 || got.Run.Generation != gen+1 {
		t.Fatal("explicit current generation cannot start a new intent", e)
	}
}

func TestStartOnceMappingCannotBeRetargetedAndClosedLookupRefuses(t *testing.T) {
	s, _ := openFixture(t)
	in, req := onceFixture(t, s)
	got, e := s.StartReservedOnce(in, req)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec("UPDATE start_requests SET payload_hash=? WHERE key=?", hash([]byte("changed")), in.IdempotencyKey); e == nil {
		t.Fatal("immutable request mapping changed")
	}
	r, e := s.LookupStart(in.IdempotencyKey, identityOf(in))
	if e != nil || r.ID != got.Run.ID {
		t.Fatal("failed mutation changed lookup", e)
	}
	bad := in
	bad.ExpectedGeneration = nil
	if _, e = s.StartReservedOnce(bad, req); !errors.Is(e, ErrInvalid) {
		t.Fatal("missing generation accepted", e)
	}
	if _, e = s.StartIntent(in); !errors.Is(e, ErrInvalid) {
		t.Fatal("unreserved legacy entry accepted an idempotent key", e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	if _, e = s.LookupStart(in.IdempotencyKey, identityOf(in)); !errors.Is(e, ErrClosed) {
		t.Fatal("closed lookup accepted", e)
	}
}

func TestStartOnceMappingFailureRollsBackEverything(t *testing.T) {
	s, _ := openFixture(t)
	in, req := onceFixture(t, s)
	before, e := s.Events(in.TaskID, 0)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := s.db.Exec("CREATE TRIGGER fixture_reject_start BEFORE INSERT ON start_requests BEGIN SELECT RAISE(ABORT,'fixture'); END;"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.StartReservedOnce(in, req); e == nil {
		t.Fatal("fault accepted")
	}
	task, _ := s.Task(in.TaskID)
	var runs, reservations int
	s.db.QueryRow("SELECT COUNT(*) FROM stage_runs").Scan(&runs)
	s.db.QueryRow("SELECT COUNT(*) FROM reservations").Scan(&reservations)
	if task.State != "ready" || task.Generation != 0 || runs != 0 || reservations != 0 {
		t.Fatal("failed mapping left launch side effects")
	}
	after, e := s.Events(in.TaskID, 0)
	if e != nil || len(before) != len(after) {
		t.Fatal("failed mapping left events")
	}
	if _, e := s.db.Exec("DROP TRIGGER fixture_reject_start"); e != nil {
		t.Fatal(e)
	}
	got, e := s.StartReservedOnce(in, req)
	if e != nil || !got.Created || got.Run.Attempt != 1 {
		t.Fatal("rollback consumed key/attempt", e)
	}
}

func TestMigrationThreePreservesVersionTwoAndRejectsChecksumDrift(t *testing.T) {
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	os.Chmod(root, 0700)
	path := filepath.Join(root, "fusion.db")
	if e = os.WriteFile(path, nil, 0600); e != nil {
		t.Fatal(e)
	}
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	for _, m := range []struct{ name, sql string }{{"migration_001_sha256", migration}, {"migration_002_sha256", migrationTwo}} {
		if _, e = db.Exec(m.sql); e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec("INSERT INTO metadata VALUES(?,?)", m.name, hash([]byte(m.sql))); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = db.Exec("UPDATE controller_policy SET max_active=4"); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("INSERT INTO tasks(id,project_id,goal,state,plan_revision,generation,next_event_seq) VALUES('fixture-v2-task','fixture-project','preserve goal','ready',1,0,1)"); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("INSERT INTO task_budgets(task_id,max_calls,max_reworks,used_calls,used_reworks) VALUES('fixture-v2-task',17,1,3,0)"); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("INSERT INTO events(task_id,seq,kind,generation) VALUES('fixture-v2-task',1,'fixture-preserved',0)"); e != nil {
		t.Fatal(e)
	}
	db.Close()
	s, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	var version int
	s.db.QueryRow("PRAGMA user_version").Scan(&version)
	limit, _ := s.Capacity()
	if version != 9 || limit != 4 {
		t.Fatal("migration lost existing policy")
	}
	task, e := s.Task("fixture-v2-task")
	if e != nil || task.Goal != "preserve goal" || task.Generation != 0 {
		t.Fatal("migration changed existing task", e)
	}
	budget, e := s.Budget(task.ID)
	if e != nil || budget.MaxCalls != 17 || budget.UsedCalls != 3 {
		t.Fatal("migration changed consumed budget", e)
	}
	events, e := s.Events(task.ID, 0)
	if e != nil || len(events) != 1 || events[0].Kind != "fixture-preserved" {
		t.Fatal("migration changed historical events", e)
	}
	if _, e = s.db.Exec("UPDATE metadata SET value='fixture-tampered' WHERE key='migration_003_sha256'"); e != nil {
		t.Fatal(e)
	}
	s.Close()
	if _, e = Open(root); !errors.Is(e, ErrUnsupported) {
		t.Fatal("migration checksum not checked", e)
	}
}

package store

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

func attemptLimitFixture(t *testing.T, s *Store) (StartRequest, ReservationRequest, []StageRun) {
	t.Helper()
	p := plan(t, 1)
	task, e := s.Create("attempt-limit-task", CreateRequest{ProjectID: "attempt-limit-project", Goal: "fixture", Plan: p, Budget: &Budget{MaxCalls: 20, MaxReworks: 1}})
	if e != nil {
		t.Fatal(e)
	}
	in := StartRequest{TaskID: task.ID, Role: stageplan.Design, PlanRevision: 1, Owner: "attempt-worker", TTL: time.Minute, Target: *p.Bindings[stageplan.Design].Target}
	req := ReservationRequest{PoolKey: "attempt-pool", GlobalLimit: 2, AdmissionHash: hash([]byte("attempt-proof"))}
	var runs []StageRun
	for n, key := range []string{"attempt-first", "attempt-remedial"} {
		in.IdempotencyKey = key
		gen := int64(n)
		in.ExpectedGeneration = &gen
		got, e := s.StartReservedOnce(in, req)
		if e != nil || !got.Created || got.Run.Attempt != int64(n+1) {
			t.Fatal("first or remedial attempt rejected", e)
		}
		r := got.Run
		if e = s.ConfirmStarted(r.ID, r.Generation, r.Owner, key+"-session"); e != nil {
			t.Fatal(e)
		}
		if e = s.ReserveCall(r.ID, r.Generation); e != nil {
			t.Fatal(e)
		}
		if e = s.Finish(r.ID, r.Generation, r.Owner, "succeeded"); e != nil {
			t.Fatal(e)
		}
		if e = s.ReleaseReserved(r.ID, r.Generation, hash([]byte(key)), true); e != nil {
			t.Fatal(e)
		}
		r, e = s.Run(r.ID)
		if e != nil {
			t.Fatal(e)
		}
		runs = append(runs, r)
	}
	gen := int64(2)
	in.ExpectedGeneration = &gen
	in.IdempotencyKey = "attempt-third"
	return in, req, runs
}

func TestStageAttemptLimitRejectsThirdIntentAndPreservesReleasedHistory(t *testing.T) {
	for _, entry := range []string{"reserved-once", "reserved", "intent"} {
		t.Run(entry, func(t *testing.T) {
			s, _ := openFixture(t)
			in, req, runs := attemptLimitFixture(t, s)
			beforeBudget, e := s.Budget(in.TaskID)
			if e != nil {
				t.Fatal(e)
			}
			var err error
			switch entry {
			case "reserved-once":
				_, err = s.StartReservedOnce(in, req)
			case "reserved":
				in.IdempotencyKey = ""
				_, err = s.StartReserved(in, req)
			case "intent":
				in.IdempotencyKey = ""
				_, err = s.StartIntent(in)
			}
			if !errors.Is(err, ErrStageLimit) {
				t.Fatal("third stage attempt was not rejected by the cap", err)
			}
			task, e := s.Task(in.TaskID)
			if e != nil || task.State != "needs_review" || task.Generation != 3 {
				t.Fatal("exhausted task was not durably fenced", task, e)
			}
			for _, before := range runs {
				after, e := s.Run(before.ID)
				if e != nil || !reflect.DeepEqual(before, after) {
					t.Fatal("cap changed prior run", e)
				}
			}
			budget, e := s.Budget(in.TaskID)
			if e != nil || budget != beforeBudget {
				t.Fatal("cap reset or consumed budget", budget, e)
			}
			var count int
			if e = s.db.QueryRow("SELECT COUNT(*) FROM stage_runs WHERE task_id=?", in.TaskID).Scan(&count); e != nil || count != 2 {
				t.Fatal("cap inserted an intent", count, e)
			}
			if e = s.db.QueryRow("SELECT COUNT(*) FROM reservations JOIN stage_runs ON stage_runs.id=reservations.run_id WHERE task_id=?", in.TaskID).Scan(&count); e != nil || count != 2 {
				t.Fatal("cap inserted a reservation", count, e)
			}
			events, e := s.Events(in.TaskID, 0)
			if e != nil {
				t.Fatal(e)
			}
			last := events[len(events)-1]
			if last.Kind != "stage_attempt_limit" || last.RunID != runs[1].ID || last.Generation != 3 {
				t.Fatal("cap did not retain affected stage identity", last)
			}
		})
	}
}

func TestStageAttemptLimitRevisionRestartAndReplayCannotResetCount(t *testing.T) {
	s, root := openFixture(t)
	in, req, runs := attemptLimitFixture(t, s)
	if e := s.RevisePlan(in.TaskID, 1, plan(t, 2)); e != nil {
		t.Fatal(e)
	}
	if e := s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	in.PlanRevision = 2
	if _, e = s.StartReservedOnce(in, req); !errors.Is(e, ErrStageLimit) {
		t.Fatal("revision/restart reset cap", e)
	}
	before, e := s.Events(in.TaskID, 0)
	if e != nil {
		t.Fatal(e)
	}
	old := in
	old.PlanRevision = 1
	old.IdempotencyKey = "attempt-remedial"
	gen := int64(1)
	old.ExpectedGeneration = &gen
	got, e := s.StartReservedOnce(old, req)
	if e != nil || got.Created || !reflect.DeepEqual(got.Run, runs[1]) {
		t.Fatal("replay became a fresh attempt", e)
	}
	after, e := s.Events(in.TaskID, 0)
	if e != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("replay added another limit event", e)
	}
	if _, e = s.LookupStart(in.IdempotencyKey, identityOf(in)); !errors.Is(e, ErrNotFound) {
		t.Fatal("denied attempt wrote a key mapping", e)
	}
}

func TestStageAttemptLimitAuthorityCASAndInvalidRoleCannotFenceTask(t *testing.T) {
	s, _ := openFixture(t)
	in, _, _ := attemptLimitFixture(t, s)
	identity := identityOf(in)
	before, e := s.Task(in.TaskID)
	if e != nil {
		t.Fatal(e)
	}
	events, e := s.Events(in.TaskID, 0)
	if e != nil {
		t.Fatal(e)
	}
	for _, current := range []func() bool{nil, func() bool { return false }} {
		if e = s.ValidateStageStartAuthorized(identity, current); !errors.Is(e, ErrWorkflowAuthority) {
			t.Fatal("absent authority mutated task", e)
		}
	}
	calls := 0
	if e = s.ValidateStageStartAuthorized(identity, func() bool { calls++; return calls == 1 }); !errors.Is(e, ErrWorkflowAuthority) || calls != 2 {
		t.Fatal("revocation after fence did not roll back", calls, e)
	}
	for _, field := range []string{"generation", "revision", "role", "restore"} {
		bad := identity
		switch field {
		case "generation":
			bad.Generation--
		case "revision":
			bad.PlanRevision++
		case "role":
			bad.Role = stageplan.Review
		case "restore":
			bad.Restore = &RestoreIdentity{OriginRunID: "foreign-origin"}
		}
		if e = s.ValidateStageStartAuthorized(bad, func() bool { return true }); e == nil {
			t.Fatal("invalid or exhausted request admitted", field)
		}
	}
	got, e := s.Task(in.TaskID)
	if e != nil || !reflect.DeepEqual(before, got) {
		t.Fatal("denied request fenced task", e)
	}
	after, e := s.Events(in.TaskID, 0)
	if e != nil || !reflect.DeepEqual(events, after) {
		t.Fatal("denied request persisted events", e)
	}
	if e = s.ValidateStageStartAuthorized(identity, func() bool { return true }); !errors.Is(e, ErrStageLimit) {
		t.Fatal("authorized limit preflight did not fence", e)
	}
	got, e = s.Task(in.TaskID)
	if e != nil || got.State != "needs_review" || got.Generation != 3 {
		t.Fatal("authorized cap not durable", e)
	}
}

func TestStageAttemptLimitRestoreSharesCapAndCannotOverflowGeneration(t *testing.T) {
	for _, mode := range []string{"restore", "overflow"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := openFixture(t)
			in, _, runs := attemptLimitFixture(t, s)
			identity := identityOf(in)
			identity.Restore = &RestoreIdentity{OriginRunID: runs[1].ID, CheckpointID: hash([]byte("checkpoint")), CheckpointDigest: hash([]byte("digest"))}
			if mode == "overflow" {
				identity.Generation = int64(1<<63 - 1)
				if _, e := s.db.Exec("UPDATE tasks SET generation=? WHERE id=?", identity.Generation, in.TaskID); e != nil {
					t.Fatal(e)
				}
			}
			e := s.ValidateStageStartAuthorized(identity, func() bool { return true })
			got, readErr := s.Task(in.TaskID)
			if mode == "restore" {
				if !errors.Is(e, ErrStageLimit) || readErr != nil || got.State != "needs_review" || got.Generation != 3 {
					t.Fatal("restore evaded stage cap", e, readErr)
				}
			} else if !errors.Is(e, ErrInvalid) || readErr != nil || got.State != "ready" || got.Generation != identity.Generation {
				t.Fatal("limit fence overflowed task generation", e, readErr)
			}
		})
	}
}

func TestStageAttemptLimitEventFailureRollsBackFenceAndConcurrentRequestsFenceOnce(t *testing.T) {
	s, _ := openFixture(t)
	in, req, _ := attemptLimitFixture(t, s)
	before, e := s.Task(in.TaskID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec(`CREATE TRIGGER reject_limit BEFORE INSERT ON events WHEN NEW.kind='stage_attempt_limit' BEGIN SELECT RAISE(ABORT,'fixture limit event failure'); END`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.StartReservedOnce(in, req); e == nil || errors.Is(e, ErrStageLimit) {
		t.Fatal("failed event committed fence", e)
	}
	got, e := s.Task(in.TaskID)
	if e != nil || got != before {
		t.Fatal("failed event changed task", e)
	}
	if _, e = s.db.Exec("DROP TRIGGER reject_limit"); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for n := 0; n < 12; n++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.StartReservedOnce(in, req); results <- e }()
	}
	wg.Wait()
	close(results)
	fences := 0
	for e := range results {
		if errors.Is(e, ErrStageLimit) {
			fences++
		} else if !errors.Is(e, ErrConflict) {
			t.Fatal("unexpected race result", e)
		}
	}
	if fences != 1 {
		t.Fatal("race did not fence exactly once", fences)
	}
	events, e := s.Events(in.TaskID, 0)
	if e != nil {
		t.Fatal(e)
	}
	fences = 0
	for _, event := range events {
		if event.Kind == "stage_attempt_limit" {
			fences++
		}
	}
	if fences != 1 {
		t.Fatal("duplicate cap event", fences)
	}
}

func TestStageAttemptLimitHeldProcessAndInvalidTargetCannotFenceOrFreeReservations(t *testing.T) {
	s, _ := openFixture(t)
	in, req, runs := attemptLimitFixture(t, s)
	bad := in
	bad.Target.ResolvedModel = "unapproved-model"
	if _, e := s.StartReservedOnce(bad, req); !errors.Is(e, ErrInvalid) {
		t.Fatal("bad target changed task", e)
	}
	// Simulate a release rollback: protocol success alone is not process exit.
	if _, e := s.db.Exec("UPDATE reservations SET state='held',stop_proof_hash='' WHERE run_id=?", runs[1].ID); e != nil {
		t.Fatal(e)
	}
	if e := s.ValidateStageStartAuthorized(identityOf(in), func() bool { return true }); !errors.Is(e, ErrConflict) {
		t.Fatal("cap replaced reconciliation", e)
	}
	got, e := s.Task(in.TaskID)
	if e != nil || got.State != "ready" || got.Generation != 2 {
		t.Fatal("held process fenced by cap", e)
	}
	if _, e = s.Reservation(runs[1].ID); e != nil {
		t.Fatal("cap freed held reservation", e)
	}
}

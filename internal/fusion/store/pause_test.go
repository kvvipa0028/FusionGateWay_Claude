package store

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func versionOf(t Task) TaskVersion {
	return TaskVersion{PlanRevision: t.PlanRevision, Generation: t.Generation, State: t.State}
}
func pauseReserved(t *testing.T, s *Store) (Task, StageRun) {
	t.Helper()
	task := create(t, s)
	if e := s.ConfigureBudget(task.ID, Budget{MaxCalls: 7, MaxReworks: 1}); e != nil {
		t.Fatal(e)
	}
	p, e := s.Plan(task.ID, 1)
	if e != nil {
		t.Fatal(e)
	}
	run, e := s.StartReserved(StartRequest{TaskID: task.ID, Role: "design", PlanRevision: 1, Owner: "fixture-worker", TTL: time.Minute, Target: *p.Bindings["design"].Target}, ReservationRequest{GlobalLimit: 2, PoolKey: "fixture-pool", AdmissionHash: strings.Repeat("a", 64)})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConfirmStarted(run.ID, run.Generation, run.Owner, "fixture-session"); e != nil {
		t.Fatal(e)
	}
	task, e = s.Task(task.ID)
	if e != nil {
		t.Fatal(e)
	}
	return task, run
}
func TestTaskPauseContinueIdleIsAtomicAndFencesOldGeneration(t *testing.T) {
	s, _ := openFixture(t)
	task := create(t, s)
	before, e := s.Plan(task.ID, 1)
	if e != nil {
		t.Fatal(e)
	}
	paused, e := s.PauseTask(task.ID, versionOf(task), "fixture-worker")
	if e != nil || !paused.Changed || paused.Task.State != "paused" || paused.Task.Generation != 1 || paused.Run != nil {
		t.Fatal("idle pause", e)
	}
	repeat, e := s.PauseTask(task.ID, versionOf(paused.Task), "fixture-worker")
	if e != nil || repeat.Changed {
		t.Fatal("pause duplicate", e)
	}
	if _, e = s.PauseTask(task.ID, versionOf(task), "fixture-worker"); !errors.Is(e, ErrConflict) {
		t.Fatal("stale pause changed task", e)
	}
	resumed, e := s.ContinueTask(task.ID, versionOf(paused.Task))
	if e != nil || !resumed.Changed || resumed.Task.State != "ready" || resumed.Task.Generation != 2 {
		t.Fatal("continue", e)
	}
	repeat, e = s.ContinueTask(task.ID, versionOf(resumed.Task))
	if e != nil || repeat.Changed {
		t.Fatal("continue duplicate", e)
	}
	after, e := s.Plan(task.ID, 1)
	if e != nil || after.Hash != before.Hash {
		t.Fatal("snapshot changed", e)
	}
	gen := int64(0)
	if _, e = s.StartIntent(StartRequest{TaskID: task.ID, Role: "design", PlanRevision: 1, ExpectedGeneration: &gen, Owner: "fixture-worker", TTL: time.Minute, Target: *before.Bindings["design"].Target}); !errors.Is(e, ErrConflict) {
		t.Fatal("ABA start was not fenced", e)
	}
	events, e := s.Events(task.ID, 0)
	if e != nil || len(events) != 3 || events[1].Kind != "task_paused" || events[2].Kind != "task_continued" {
		t.Fatal("control events", e)
	}
}
func TestTaskPauseRunningBlocksCallsAndCannotContinueUntilProof(t *testing.T) {
	s, _ := openFixture(t)
	task, run := pauseReserved(t, s)
	if e := s.ReserveCall(run.ID, run.Generation); e != nil {
		t.Fatal(e)
	}
	before, e := s.Budget(task.ID)
	if e != nil {
		t.Fatal(e)
	}
	paused, e := s.PauseTask(task.ID, versionOf(task), run.Owner)
	if e != nil || paused.Task.State != "pausing" || paused.Task.Generation != run.Generation || paused.Run == nil || paused.Run.State != "cancelling" {
		t.Fatal("pause intent", e)
	}
	if e = s.ReserveCall(run.ID, run.Generation); !errors.Is(e, ErrFenced) {
		t.Fatal("new call after pause", e)
	}
	if _, e = s.ContinueTask(task.ID, versionOf(paused.Task)); !errors.Is(e, ErrPauseReconcile) {
		t.Fatal("pausing continued", e)
	}
	repeat, e := s.PauseTask(task.ID, versionOf(paused.Task), run.Owner)
	if e != nil || repeat.Changed {
		t.Fatal("pause intent duplicated", e)
	}
	if e = s.Finish(run.ID, run.Generation, run.Owner, "cancelled"); e != nil {
		t.Fatal(e)
	}
	task, e = s.Task(task.ID)
	if e != nil || task.State != "pausing" {
		t.Fatal("protocol finish invented stop", e)
	}
	if _, e = s.ContinueTask(task.ID, versionOf(task)); !errors.Is(e, ErrPauseReconcile) {
		t.Fatal("held reservation bypassed", e)
	}
	if e = s.ReleaseReserved(run.ID, run.Generation, strings.Repeat("b", 64), true); e != nil {
		t.Fatal(e)
	}
	task, e = s.Task(task.ID)
	if e != nil || task.State != "needs_review" {
		t.Fatal("cancelled worker allowed blind resume", e)
	}
	if _, e = s.ContinueTask(task.ID, versionOf(task)); !errors.Is(e, ErrPauseReconcile) {
		t.Fatal("cancelled work replayable", e)
	}
	after, e := s.Budget(task.ID)
	if e != nil || after != before {
		t.Fatal("pause reset budget", e)
	}
}
func TestTaskPauseFinishedSuccessRequiresReleasedProofBeforeBecomingPaused(t *testing.T) {
	s, _ := openFixture(t)
	task, run := pauseReserved(t, s)
	if e := s.Finish(run.ID, run.Generation, run.Owner, "succeeded"); e != nil {
		t.Fatal(e)
	}
	task, e := s.Task(task.ID)
	if e != nil {
		t.Fatal(e)
	}
	receipt, e := s.PauseTask(task.ID, versionOf(task), run.Owner)
	if e != nil || receipt.Task.State != "pausing" || receipt.Run == nil {
		t.Fatal("held finished execution claimed stopped", e)
	}
	if e = s.ReleaseReserved(run.ID, run.Generation, strings.Repeat("b", 64), false); !errors.Is(e, ErrInvalid) {
		t.Fatal("unverified proof", e)
	}
	if _, e = s.ContinueTask(task.ID, versionOf(receipt.Task)); !errors.Is(e, ErrPauseReconcile) {
		t.Fatal("held success bypass", e)
	}
	if e = s.ReleaseReserved(run.ID, run.Generation, strings.Repeat("b", 64), true); e != nil {
		t.Fatal(e)
	}
	task, e = s.Task(task.ID)
	if e != nil || task.State != "paused" {
		t.Fatal("actual release not settled", e)
	}
	next, e := s.ContinueTask(task.ID, versionOf(task))
	if e != nil || next.Task.State != "ready" || next.Task.Generation != run.Generation+1 {
		t.Fatal("continue after release", e)
	}
	if _, e = s.Reservation(run.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("continue reacquired capacity", e)
	}
}
func TestTaskPauseExpiryUnknownOwnerAndStalePlanStayFenced(t *testing.T) {
	s, _ := openFixture(t)
	task, run := pauseReserved(t, s)
	if _, e := s.PauseTask(task.ID, versionOf(task), "other-owner"); !errors.Is(e, ErrFenced) {
		t.Fatal("foreign owner cancelled", e)
	}
	wrong := versionOf(task)
	wrong.PlanRevision++
	if _, e := s.PauseTask(task.ID, wrong, run.Owner); !errors.Is(e, ErrConflict) {
		t.Fatal("stale plan paused", e)
	}
	s.now = func() time.Time { return run.LeaseUntil.Add(time.Minute) }
	// StartReserved's returned lease is captured before ConfirmStarted.
	if _, e := s.PauseTask(task.ID, versionOf(task), run.Owner); !errors.Is(e, ErrFenced) {
		t.Fatal("expired execution adopted", e)
	}
	task, e := s.Task(task.ID)
	if e != nil || task.State != "needs_review" {
		t.Fatal("expiry not fenced", e)
	}
	if _, e = s.PauseTask(task.ID, versionOf(task), run.Owner); !errors.Is(e, ErrPauseReconcile) {
		t.Fatal("unknown pause adopted", e)
	}
	if _, e = s.ContinueTask(task.ID, versionOf(task)); !errors.Is(e, ErrPauseReconcile) {
		t.Fatal("unknown continued", e)
	}
	if _, e = s.Reservation(run.ID); e != nil {
		t.Fatal("unknown released capacity", e)
	}
}

func TestTaskPauseAndStartRaceHaveOneDurableWinner(t *testing.T) {
	for n := 0; n < 12; n++ {
		s, _ := openFixture(t)
		task := create(t, s)
		if e := s.ConfigureBudget(task.ID, Budget{MaxCalls: 7}); e != nil {
			t.Fatal(e)
		}
		p, e := s.Plan(task.ID, 1)
		if e != nil {
			t.Fatal(e)
		}
		gen := task.Generation
		start := StartRequest{TaskID: task.ID, Role: "design", PlanRevision: 1, ExpectedGeneration: &gen, IdempotencyKey: "fixture-pause-race", Owner: "fixture-worker", TTL: time.Minute, Target: *p.Bindings["design"].Target}
		gate := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		var pauseErr, startErr error
		var receipt StartReceipt
		go func() { defer wg.Done(); <-gate; _, pauseErr = s.PauseTask(task.ID, versionOf(task), "fixture-worker") }()
		go func() {
			defer wg.Done()
			<-gate
			receipt, startErr = s.StartReservedOnce(start, ReservationRequest{GlobalLimit: 2, PoolKey: "fixture-pool", AdmissionHash: strings.Repeat("a", 64)})
		}()
		close(gate)
		wg.Wait()
		if (pauseErr == nil) == (startErr == nil) {
			t.Fatal("race did not have one winner", pauseErr, startErr)
		}
		if pauseErr != nil && !errors.Is(pauseErr, ErrConflict) || startErr != nil && !errors.Is(startErr, ErrConflict) {
			t.Fatal("unexpected race failure", pauseErr, startErr)
		}
		current, e := s.Task(task.ID)
		if e != nil || current.Generation != 1 {
			t.Fatal("race generation", e)
		}
		if pauseErr == nil {
			if current.State != "paused" {
				t.Fatal("pause winner lost")
			}
			if _, e = s.LookupStart(start.IdempotencyKey, startIdentity(start)); !errors.Is(e, ErrNotFound) {
				t.Fatal("loser persisted intent", e)
			}
		} else {
			if current.State != "running" || !receipt.Created {
				t.Fatal("start winner lost")
			}
		}
		s.Close()
	}
}
func TestTaskPauseEventFailureRollsBackCancellationAndState(t *testing.T) {
	s, _ := openFixture(t)
	task, run := pauseReserved(t, s)
	before, e := s.Events(task.ID, 0)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec("CREATE TRIGGER fixture_pause_event_fail BEFORE INSERT ON events WHEN NEW.kind='task_pause_requested' BEGIN SELECT RAISE(ABORT,'fixture'); END"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.PauseTask(task.ID, versionOf(task), run.Owner); e == nil {
		t.Fatal("failed pause committed")
	}
	current, e := s.Task(task.ID)
	if e != nil || current != task {
		t.Fatal("failed pause changed task", e)
	}
	active, e := s.Run(run.ID)
	if e != nil || active.State != "running" {
		t.Fatal("failed pause cancelled run", e)
	}
	after, e := s.Events(task.ID, 0)
	if e != nil || len(after) != len(before) {
		t.Fatal("failed pause left event", e)
	}
	if e = s.ReserveCall(run.ID, run.Generation); e != nil {
		t.Fatal("failed pause blocked caller", e)
	}
}
func TestTaskPauseReleaseSettlementFailureIsAtomicAndRetryHasOneEvent(t *testing.T) {
	s, _ := openFixture(t)
	task, run := pauseReserved(t, s)
	if e := s.Finish(run.ID, run.Generation, run.Owner, "succeeded"); e != nil {
		t.Fatal(e)
	}
	task, e := s.Task(task.ID)
	if e != nil {
		t.Fatal(e)
	}
	receipt, e := s.PauseTask(task.ID, versionOf(task), run.Owner)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec("CREATE TRIGGER fixture_pause_settle_fail BEFORE INSERT ON events WHEN NEW.kind='task_paused' BEGIN SELECT RAISE(ABORT,'fixture'); END"); e != nil {
		t.Fatal(e)
	}
	proof := strings.Repeat("b", 64)
	if e = s.ReleaseReserved(run.ID, run.Generation, proof, true); e == nil {
		t.Fatal("failed settlement accepted")
	}
	task, e = s.Task(task.ID)
	if e != nil || task != receipt.Task {
		t.Fatal("failed release settlement changed task", e)
	}
	if _, e = s.Reservation(run.ID); e != nil {
		t.Fatal("failed release dropped held", e)
	}
	if _, e = s.db.Exec("DROP TRIGGER fixture_pause_settle_fail"); e != nil {
		t.Fatal(e)
	}
	if e = s.ReleaseReserved(run.ID, run.Generation, proof, true); e != nil {
		t.Fatal(e)
	}
	before, e := s.Events(task.ID, 0)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ReleaseReserved(run.ID, run.Generation, proof, true); e != nil {
		t.Fatal(e)
	}
	after, e := s.Events(task.ID, 0)
	if e != nil || len(before) != len(after) {
		t.Fatal("stop replay duplicated settlement", e)
	}
}
func TestTaskPauseRestartRetainsPauseOrUnknownWithoutReplay(t *testing.T) {
	t.Run("idle", func(t *testing.T) {
		s, root := openFixture(t)
		task := create(t, s)
		receipt, e := s.PauseTask(task.ID, versionOf(task), "")
		if e != nil {
			t.Fatal(e)
		}
		s.Close()
		s, e = Open(root)
		if e != nil {
			t.Fatal(e)
		}
		defer s.Close()
		got, e := s.Task(task.ID)
		if e != nil || got != receipt.Task {
			t.Fatal("idle pause lost", e)
		}
		if _, e = s.ContinueTask(task.ID, versionOf(got)); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("active", func(t *testing.T) {
		s, root := openFixture(t)
		task, run := pauseReserved(t, s)
		if _, e := s.PauseTask(task.ID, versionOf(task), run.Owner); e != nil {
			t.Fatal(e)
		}
		s.Close()
		s, e := Open(root)
		if e != nil {
			t.Fatal(e)
		}
		defer s.Close()
		got, e := s.Task(task.ID)
		if e != nil || got.State != "needs_review" {
			t.Fatal("active pause falsely stopped", e)
		}
		if _, e = s.ContinueTask(task.ID, versionOf(got)); !errors.Is(e, ErrPauseReconcile) {
			t.Fatal("restart replayed", e)
		}
		if _, e = s.Reservation(run.ID); e != nil {
			t.Fatal("restart released held", e)
		}
	})
}
func TestTaskPauseLegacyWithoutStopProofAndOverflowCannotContinue(t *testing.T) {
	s, _ := openFixture(t)
	task := create(t, s)
	run := start(t, s, task)
	if e := s.ConfirmStarted(run.ID, run.Generation, run.Owner, "fixture-session"); e != nil {
		t.Fatal(e)
	}
	if e := s.Finish(run.ID, run.Generation, run.Owner, "succeeded"); e != nil {
		t.Fatal(e)
	}
	task, e := s.Task(task.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.PauseTask(task.ID, versionOf(task), run.Owner); !errors.Is(e, ErrPauseReconcile) {
		t.Fatal("legacy terminal was treated as stop proof", e)
	}
	if _, e = s.db.Exec("UPDATE tasks SET generation=?,state='paused' WHERE id=?", int64(1<<63-1), task.ID); e != nil {
		t.Fatal(e)
	}
	task, e = s.Task(task.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ContinueTask(task.ID, versionOf(task)); !errors.Is(e, ErrPauseReconcile) {
		t.Fatal("legacy paused bypassed proof", e)
	}
	empty, e := s.Create("fixture-max-key", CreateRequest{ProjectID: "fixture-project", Goal: "overflow", Plan: plan(t, 1)})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec("UPDATE tasks SET generation=? WHERE id=?", int64(1<<63-1), empty.ID); e != nil {
		t.Fatal(e)
	}
	empty, e = s.Task(empty.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.PauseTask(empty.ID, versionOf(empty), ""); !errors.Is(e, ErrInvalid) {
		t.Fatal("overflow pause", e)
	}
	if _, e = s.db.Exec("UPDATE tasks SET state='paused' WHERE id=?", empty.ID); e != nil {
		t.Fatal(e)
	}
	empty, e = s.Task(empty.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ContinueTask(empty.ID, versionOf(empty)); !errors.Is(e, ErrInvalid) {
		t.Fatal("overflow continue", e)
	}
}
func TestTaskPauseInterruptedWithoutHandleRequiresReviewBeforeRelease(t *testing.T) {
	s, _ := openFixture(t)
	task, run := pauseReserved(t, s)
	if _, e := s.PauseTask(task.ID, versionOf(task), run.Owner); e != nil {
		t.Fatal(e)
	}
	if e := s.Finish(run.ID, run.Generation, run.Owner, "interrupted"); e != nil {
		t.Fatal(e)
	}
	task, e := s.Task(task.ID)
	if e != nil || task.State != "needs_review" {
		t.Fatal("interrupted pause can resume", e)
	}
	if _, e = s.Reservation(run.ID); e != nil {
		t.Fatal("missing stop proof dropped reservation", e)
	}
}

func TestTaskPauseCurrentStopCannotProveEarlierLegacyExecutionStopped(t *testing.T) {
	s, _ := openFixture(t)
	task := create(t, s)
	if e := s.ConfigureBudget(task.ID, Budget{MaxCalls: 7}); e != nil {
		t.Fatal(e)
	}
	legacy := start(t, s, task)
	if e := s.ConfirmStarted(legacy.ID, legacy.Generation, legacy.Owner, "fixture-legacy-session"); e != nil {
		t.Fatal(e)
	}
	if e := s.Finish(legacy.ID, legacy.Generation, legacy.Owner, "succeeded"); e != nil {
		t.Fatal(e)
	}
	p, e := s.Plan(task.ID, 1)
	if e != nil {
		t.Fatal(e)
	}
	run, e := s.StartReserved(StartRequest{TaskID: task.ID, Role: "design", PlanRevision: 1, Owner: "fixture-worker", TTL: time.Minute, Target: *p.Bindings["design"].Target}, ReservationRequest{GlobalLimit: 2, PoolKey: "fixture-pool", AdmissionHash: strings.Repeat("a", 64)})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConfirmStarted(run.ID, run.Generation, run.Owner, "fixture-current-session"); e != nil {
		t.Fatal(e)
	}
	if e = s.Finish(run.ID, run.Generation, run.Owner, "succeeded"); e != nil {
		t.Fatal(e)
	}
	task, e = s.Task(task.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.PauseTask(task.ID, versionOf(task), run.Owner); e != nil {
		t.Fatal(e)
	}
	if e = s.ReleaseReserved(run.ID, run.Generation, strings.Repeat("b", 64), true); e != nil {
		t.Fatal(e)
	}
	task, e = s.Task(task.ID)
	if e != nil || task.State != "needs_review" {
		t.Fatal("current proof falsely settled earlier unproven execution", task.State, e)
	}
}

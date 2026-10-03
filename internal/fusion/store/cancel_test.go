package store

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTaskCancelIdleAndPausedPreservesSnapshotAndBudget(t *testing.T) {
	for _, state := range []string{"ready", "paused"} {
		t.Run(state, func(t *testing.T) {
			s, _ := openFixture(t)
			task := create(t, s)
			if e := s.ConfigureBudget(task.ID, Budget{MaxCalls: 7, MaxReworks: 1}); e != nil {
				t.Fatal(e)
			}
			if state == "paused" {
				x, e := s.PauseTask(task.ID, versionOf(task), "")
				if e != nil {
					t.Fatal(e)
				}
				task = x.Task
			}
			p, e := s.Plan(task.ID, 1)
			if e != nil {
				t.Fatal(e)
			}
			b, e := s.Budget(task.ID)
			if e != nil {
				t.Fatal(e)
			}
			x, e := s.CancelTask(task.ID, versionOf(task), "")
			if e != nil || !x.Changed || x.Run != nil || x.Task.State != "cancelled" || x.Task.Generation != task.Generation+1 {
				t.Fatal("idle cancel", x, e)
			}
			before, e := s.Events(task.ID, 0)
			if e != nil {
				t.Fatal(e)
			}
			repeat, e := s.CancelTask(task.ID, versionOf(x.Task), "")
			if e != nil || repeat.Changed || repeat.Run != nil || repeat.Task != x.Task {
				t.Fatal("cancel repeat", e)
			}
			after, e := s.Events(task.ID, 0)
			if e != nil || len(after) != len(before) || after[len(after)-1].Kind != "task_cancelled" {
				t.Fatal("cancel event", e)
			}
			if _, e = s.CancelTask(task.ID, versionOf(task), ""); !errors.Is(e, ErrConflict) {
				t.Fatal("stale cancel", e)
			}
			if _, e = s.ContinueTask(task.ID, versionOf(x.Task)); !errors.Is(e, ErrConflict) {
				t.Fatal("cancelled continued", e)
			}
			gen := task.Generation
			if _, e = s.StartIntent(StartRequest{TaskID: task.ID, Role: "design", PlanRevision: 1, ExpectedGeneration: &gen, Owner: "fixture-worker", TTL: time.Minute, Target: *p.Bindings["design"].Target}); !errors.Is(e, ErrConflict) {
				t.Fatal("cancelled start", e)
			}
			got, e := s.Plan(task.ID, 1)
			if e != nil || got.Hash != p.Hash {
				t.Fatal("snapshot changed", e)
			}
			budget, e := s.Budget(task.ID)
			if e != nil || budget != b {
				t.Fatal("budget reset", e)
			}
		})
	}
}

func TestTaskCancelRunningRequiresProofAndDoesNotRefund(t *testing.T) {
	s, _ := openFixture(t)
	task, run := pauseReserved(t, s)
	if e := s.ReserveCall(run.ID, run.Generation); e != nil {
		t.Fatal(e)
	}
	b, e := s.Budget(task.ID)
	if e != nil {
		t.Fatal(e)
	}
	x, e := s.CancelTask(task.ID, versionOf(task), run.Owner)
	if e != nil || !x.Changed || x.Task.State != "cancelling" || x.Task.Generation != run.Generation || x.Run == nil || x.Run.State != "cancelling" {
		t.Fatal("active cancel", e)
	}
	if e = s.ReserveCall(run.ID, run.Generation); !errors.Is(e, ErrFenced) {
		t.Fatal("post-cancel call", e)
	}
	repeat, e := s.CancelTask(task.ID, versionOf(x.Task), run.Owner)
	if e != nil || repeat.Changed || repeat.Run == nil {
		t.Fatal("duplicate intent", e)
	}
	if _, e = s.ContinueTask(task.ID, versionOf(x.Task)); !errors.Is(e, ErrConflict) {
		t.Fatal("cancelling continued", e)
	}
	if e = s.Finish(run.ID, run.Generation, run.Owner, "cancelled"); e != nil {
		t.Fatal(e)
	}
	task, e = s.Task(task.ID)
	if e != nil || task.State != "cancelling" {
		t.Fatal("protocol finish claimed stop", task.State, e)
	}
	if _, e = s.Reservation(run.ID); e != nil {
		t.Fatal("held lost", e)
	}
	if e = s.ReleaseReserved(run.ID, run.Generation, strings.Repeat("b", 64), false); !errors.Is(e, ErrInvalid) {
		t.Fatal("false proof accepted", e)
	}
	if e = s.ReleaseReserved(run.ID, run.Generation, strings.Repeat("b", 64), true); e != nil {
		t.Fatal(e)
	}
	task, e = s.Task(task.ID)
	if e != nil || task.State != "cancelled" {
		t.Fatal("proof did not settle", task.State, e)
	}
	budget, e := s.Budget(task.ID)
	if e != nil || budget != b {
		t.Fatal("calls refunded", e)
	}
	if _, e = s.Reservation(run.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("capacity held after proof", e)
	}
}

func TestTaskCancelFinishedHeldAndPausingWaitForProof(t *testing.T) {
	for _, mode := range []string{"finished-success", "finished-failure", "finished-cancelled", "pausing"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := openFixture(t)
			task, run := pauseReserved(t, s)
			if mode == "pausing" {
				x, e := s.PauseTask(task.ID, versionOf(task), run.Owner)
				if e != nil {
					t.Fatal(e)
				}
				task = x.Task
			} else {
				outcome := map[string]string{"finished-success": "succeeded", "finished-failure": "failed", "finished-cancelled": "cancelled"}[mode]
				if e := s.Finish(run.ID, run.Generation, run.Owner, outcome); e != nil {
					t.Fatal(e)
				}
				var e error
				task, e = s.Task(task.ID)
				if e != nil {
					t.Fatal(e)
				}
			}
			x, e := s.CancelTask(task.ID, versionOf(task), run.Owner)
			if e != nil || x.Task.State != "cancelling" || !x.Changed || x.Run == nil {
				t.Fatal("held cancellation", e)
			}
			if mode == "pausing" {
				if e = s.Finish(run.ID, run.Generation, run.Owner, "cancelled"); e != nil {
					t.Fatal(e)
				}
			}
			if e = s.ReleaseReserved(run.ID, run.Generation, strings.Repeat("b", 64), true); e != nil {
				t.Fatal(e)
			}
			task, e = s.Task(task.ID)
			if e != nil || task.State != "cancelled" {
				t.Fatal("cancel must override pause/ready", task.State, e)
			}
		})
	}
}

func TestTaskCancelFencesForeignExpiredAndStaleConditions(t *testing.T) {
	s, _ := openFixture(t)
	task, run := pauseReserved(t, s)
	if _, e := s.CancelTask(task.ID, versionOf(task), "other-owner"); !errors.Is(e, ErrFenced) {
		t.Fatal("foreign cancel", e)
	}
	for _, v := range []TaskVersion{{PlanRevision: 2, Generation: task.Generation, State: task.State}, {PlanRevision: 1, Generation: task.Generation + 1, State: task.State}, {PlanRevision: 1, Generation: task.Generation, State: "ready"}} {
		if _, e := s.CancelTask(task.ID, v, run.Owner); !errors.Is(e, ErrConflict) {
			t.Fatal("stale condition", e)
		}
	}
	current, e := s.Task(task.ID)
	if e != nil || current != task {
		t.Fatal("refusal changed state", e)
	}
	s.now = func() time.Time { return run.LeaseUntil.Add(time.Minute) }
	if _, e = s.CancelTask(task.ID, versionOf(task), run.Owner); !errors.Is(e, ErrFenced) {
		t.Fatal("expired owner", e)
	}
	task, e = s.Task(task.ID)
	if e != nil || task.State != "needs_review" {
		t.Fatal("expired not unknown", e)
	}
	if _, e = s.CancelTask(task.ID, versionOf(task), run.Owner); !errors.Is(e, ErrCancelReconcile) {
		t.Fatal("unknown adopted", e)
	}
	if _, e = s.Reservation(run.ID); e != nil {
		t.Fatal("unknown released", e)
	}
}

func TestTaskCancelAndStartRaceHaveOneDurableWinner(t *testing.T) {
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
		startReq := StartRequest{TaskID: task.ID, Role: "design", PlanRevision: 1, ExpectedGeneration: &gen, IdempotencyKey: "fixture-cancel-race", Owner: "fixture-worker", TTL: time.Minute, Target: *p.Bindings["design"].Target}
		gate := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		var ce, se error
		var started StartReceipt
		go func() { defer wg.Done(); <-gate; _, ce = s.CancelTask(task.ID, versionOf(task), "fixture-worker") }()
		go func() {
			defer wg.Done()
			<-gate
			started, se = s.StartReservedOnce(startReq, ReservationRequest{GlobalLimit: 2, PoolKey: "fixture-pool", AdmissionHash: strings.Repeat("a", 64)})
		}()
		close(gate)
		wg.Wait()
		if (ce == nil) == (se == nil) || ce != nil && !errors.Is(ce, ErrConflict) || se != nil && !errors.Is(se, ErrConflict) {
			t.Fatal("race", ce, se)
		}
		current, e := s.Task(task.ID)
		if e != nil || current.Generation != 1 {
			t.Fatal("generation", e)
		}
		if ce == nil {
			if current.State != "cancelled" {
				t.Fatal("cancel lost")
			}
			if _, e = s.LookupStart(startReq.IdempotencyKey, startIdentity(startReq)); !errors.Is(e, ErrNotFound) {
				t.Fatal("loser intent persisted", e)
			}
		} else if current.State != "running" || !started.Created {
			t.Fatal("start lost")
		}
		s.Close()
	}
}

func TestTaskCancelIntentAndSettlementFailureRollback(t *testing.T) {
	s, _ := openFixture(t)
	task, run := pauseReserved(t, s)
	before, e := s.Events(task.ID, 0)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec("CREATE TRIGGER fixture_cancel_intent_fail BEFORE INSERT ON events WHEN NEW.kind='task_cancel_requested' BEGIN SELECT RAISE(ABORT,'fixture'); END"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.CancelTask(task.ID, versionOf(task), run.Owner); e == nil {
		t.Fatal("failed intent committed")
	}
	got, e := s.Task(task.ID)
	if e != nil || got != task {
		t.Fatal("task not rolled back", e)
	}
	active, e := s.Run(run.ID)
	if e != nil || active.State != "running" {
		t.Fatal("run not rolled back", e)
	}
	after, e := s.Events(task.ID, 0)
	if e != nil || len(after) != len(before) {
		t.Fatal("event not rolled back", e)
	}
	if _, e = s.db.Exec("DROP TRIGGER fixture_cancel_intent_fail"); e != nil {
		t.Fatal(e)
	}
	x, e := s.CancelTask(task.ID, versionOf(task), run.Owner)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Finish(run.ID, run.Generation, run.Owner, "cancelled"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec("CREATE TRIGGER fixture_cancel_settle_fail BEFORE INSERT ON events WHEN NEW.kind='task_cancelled' BEGIN SELECT RAISE(ABORT,'fixture'); END"); e != nil {
		t.Fatal(e)
	}
	proof := strings.Repeat("b", 64)
	if e = s.ReleaseReserved(run.ID, run.Generation, proof, true); e == nil {
		t.Fatal("failed settlement accepted")
	}
	got, e = s.Task(task.ID)
	if e != nil || got != x.Task {
		t.Fatal("settlement changed task", e)
	}
	if _, e = s.Reservation(run.ID); e != nil {
		t.Fatal("settlement released held", e)
	}
	if _, e = s.db.Exec("DROP TRIGGER fixture_cancel_settle_fail"); e != nil {
		t.Fatal(e)
	}
	if e = s.ReleaseReserved(run.ID, run.Generation, proof, true); e != nil {
		t.Fatal(e)
	}
	before, e = s.Events(task.ID, 0)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ReleaseReserved(run.ID, run.Generation, proof, true); e != nil {
		t.Fatal(e)
	}
	after, e = s.Events(task.ID, 0)
	if e != nil || len(after) != len(before) {
		t.Fatal("proof repeat duplicated events", e)
	}
}

func TestTaskCancelRestartKeepsIdleTerminalAndActiveUnknown(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(map[bool]string{false: "idle", true: "active"}[active], func(t *testing.T) {
			s, root := openFixture(t)
			var task Task
			var run StageRun
			if active {
				task, run = pauseReserved(t, s)
			} else {
				task = create(t, s)
			}
			x, e := s.CancelTask(task.ID, versionOf(task), run.Owner)
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
			if e != nil {
				t.Fatal(e)
			}
			if !active {
				if got != x.Task {
					t.Fatal("terminal lost")
				}
				return
			}
			if got.State != "needs_review" {
				t.Fatal("active restart claims stopped")
			}
			if _, e = s.CancelTask(task.ID, versionOf(got), run.Owner); !errors.Is(e, ErrCancelReconcile) {
				t.Fatal("unknown replayed", e)
			}
			if _, e = s.Reservation(run.ID); e != nil {
				t.Fatal("unknown capacity dropped", e)
			}
		})
	}
}

func TestTaskCancelCannotInventLegacyStopAndInterruptedNeedsReview(t *testing.T) {
	t.Run("legacy", func(t *testing.T) {
		s, _ := openFixture(t)
		task := create(t, s)
		legacy := start(t, s, task)
		if e := s.ConfirmStarted(legacy.ID, legacy.Generation, legacy.Owner, "fixture-session"); e != nil {
			t.Fatal(e)
		}
		if e := s.Finish(legacy.ID, legacy.Generation, legacy.Owner, "succeeded"); e != nil {
			t.Fatal(e)
		}
		task, e := s.Task(task.ID)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.CancelTask(task.ID, versionOf(task), legacy.Owner); !errors.Is(e, ErrCancelReconcile) {
			t.Fatal("legacy proof invented", e)
		}
	})
	t.Run("interrupted", func(t *testing.T) {
		s, _ := openFixture(t)
		task, run := pauseReserved(t, s)
		if _, e := s.CancelTask(task.ID, versionOf(task), run.Owner); e != nil {
			t.Fatal(e)
		}
		if e := s.Finish(run.ID, run.Generation, run.Owner, "interrupted"); e != nil {
			t.Fatal(e)
		}
		task, e := s.Task(task.ID)
		if e != nil || task.State != "needs_review" {
			t.Fatal("interrupted trusted", e)
		}
		if _, e = s.Reservation(run.ID); e != nil {
			t.Fatal("no stop proof reservation lost", e)
		}
	})
}

func TestTaskCancelCurrentProofCannotProveEarlierLegacyStopped(t *testing.T) {
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
	task, e = s.Task(task.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.CancelTask(task.ID, versionOf(task), run.Owner); e != nil {
		t.Fatal(e)
	}
	if e = s.Finish(run.ID, run.Generation, run.Owner, "cancelled"); e != nil {
		t.Fatal(e)
	}
	if e = s.ReleaseReserved(run.ID, run.Generation, strings.Repeat("b", 64), true); e != nil {
		t.Fatal(e)
	}
	task, e = s.Task(task.ID)
	if e != nil || task.State != "needs_review" {
		t.Fatal("current proof substituted for earlier execution", task.State, e)
	}
}

func TestTaskCancelInvalidOverflowAndStartingStayFenced(t *testing.T) {
	s, _ := openFixture(t)
	task := create(t, s)
	for _, v := range []TaskVersion{{}, {PlanRevision: 1, Generation: -1, State: "ready"}, {PlanRevision: 1, State: ""}} {
		if _, e := s.CancelTask(task.ID, v, ""); !errors.Is(e, ErrInvalid) {
			t.Fatal("invalid version", e)
		}
	}
	if _, e := s.db.Exec("UPDATE tasks SET generation=? WHERE id=?", int64(1<<63-1), task.ID); e != nil {
		t.Fatal(e)
	}
	task, e := s.Task(task.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.CancelTask(task.ID, versionOf(task), ""); !errors.Is(e, ErrInvalid) {
		t.Fatal("overflow", e)
	}
	got, e := s.Task(task.ID)
	if e != nil || got != task {
		t.Fatal("invalid changed task", e)
	}
	other, e := s.Create("fixture-cancel-starting", CreateRequest{ProjectID: "fixture-project", Goal: "cancel starting", Plan: plan(t, 1)})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConfigureBudget(other.ID, Budget{MaxCalls: 7}); e != nil {
		t.Fatal(e)
	}
	p, e := s.Plan(other.ID, 1)
	if e != nil {
		t.Fatal(e)
	}
	run, e := s.StartReserved(StartRequest{TaskID: other.ID, Role: "design", PlanRevision: 1, Owner: "fixture-worker", TTL: time.Minute, Target: *p.Bindings["design"].Target}, ReservationRequest{GlobalLimit: 2, PoolKey: "fixture-pool", AdmissionHash: strings.Repeat("a", 64)})
	if e != nil {
		t.Fatal(e)
	}
	other, e = s.Task(other.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.CancelTask(other.ID, versionOf(other), run.Owner); e != nil {
		t.Fatal(e)
	}
	if e = s.ConfirmStarted(run.ID, run.Generation, run.Owner, "fixture-session"); !errors.Is(e, ErrFenced) {
		t.Fatal("late launch confirmation admitted", e)
	}
	if e = s.Finish(run.ID, run.Generation, run.Owner, "interrupted"); e != nil {
		t.Fatal(e)
	}
	other, e = s.Task(other.ID)
	if e != nil || other.State != "needs_review" {
		t.Fatal("unconfirmed launch claimed stopped", e)
	}
	if _, e = s.Reservation(run.ID); e != nil {
		t.Fatal("unconfirmed released", e)
	}
}

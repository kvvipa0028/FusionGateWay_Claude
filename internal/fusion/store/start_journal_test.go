package store

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func TestStartJournalCommitFailureRollsBackEveryDurableSideEffect(t *testing.T) {
	s, _ := openFixture(t)
	in, req := onceFixture(t, s)
	ident := identityOf(in)
	prepared, e := s.PrepareStart(in.IdempotencyKey, ident)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec("CREATE TRIGGER fixture_commit_fail AFTER UPDATE ON start_journals WHEN NEW.state='committed' BEGIN SELECT RAISE(ABORT,'fixture'); END"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.StartReservedOnce(in, req); e == nil {
		t.Fatal("failed journal commit accepted")
	}
	j, e := s.LookupStartJournal(in.IdempotencyKey, ident)
	if e != nil || !reflect.DeepEqual(j, prepared.Journal) {
		t.Fatal("partial journal committed", e)
	}
	for _, table := range []string{"stage_runs", "reservations", "start_requests"} {
		var n int
		if e = s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); e != nil || n != 0 {
			t.Fatal("partial durable work", table, e)
		}
	}
	task, e := s.Task(in.TaskID)
	if e != nil || task.Generation != 0 || task.State != "ready" {
		t.Fatal("partial Task", e)
	}
	events, e := s.Events(in.TaskID, 0)
	if e != nil || len(events) != 1 || events[0].Kind != "created" {
		t.Fatal("partial events", e)
	}
	if _, e = s.db.Exec("DROP TRIGGER fixture_commit_fail"); e != nil {
		t.Fatal(e)
	}
	if got, e := s.StartReservedOnce(in, req); e != nil || !got.Created {
		t.Fatal("original request could not retry", e)
	}
}

func TestStartJournalCorruptionCannotAuthorizeStartOrResolve(t *testing.T) {
	s, _ := openFixture(t)
	in, req := onceFixture(t, s)
	ident := identityOf(in)
	if _, e := s.PrepareStart(in.IdempotencyKey, ident); e != nil {
		t.Fatal(e)
	}
	// Simulate damaged storage, not a supported caller operation.
	if _, e := s.db.Exec("DROP TRIGGER immutable_start_draft; DROP TRIGGER start_journal_transition"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.db.Exec("UPDATE start_journals SET draft_json='{}' WHERE key=?", in.IdempotencyKey); e != nil {
		t.Fatal(e)
	}
	if _, e := s.LookupStartJournal(in.IdempotencyKey, ident); !errors.Is(e, ErrConflict) {
		t.Fatal("damaged read accepted", e)
	}
	if _, e := s.StartReservedOnce(in, req); !errors.Is(e, ErrConflict) {
		t.Fatal("damaged draft started", e)
	}
	if _, e := s.ResolveStart(in.IdempotencyKey, ident, "abandon", ""); !errors.Is(e, ErrConflict) {
		t.Fatal("damaged draft resolved", e)
	}
	var count int
	if e := s.db.QueryRow("SELECT COUNT(*) FROM stage_runs").Scan(&count); e != nil || count != 0 {
		t.Fatal("damaged draft created work", e)
	}
}

func TestStartJournalMissingLinkedTaskIsCorruptionRatherThanAbsentDraft(t *testing.T) {
	s, _ := openFixture(t)
	in, _ := onceFixture(t, s)
	ident := identityOf(in)
	if _, e := s.PrepareStart(in.IdempotencyKey, ident); e != nil {
		t.Fatal(e)
	}
	// Simulate a damaged foreign-key link in the private database.
	if _, e := s.db.Exec("PRAGMA foreign_keys=OFF"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.db.Exec("DELETE FROM tasks WHERE id=?", in.TaskID); e != nil {
		t.Fatal(e)
	}
	if _, e := s.LookupStart(in.IdempotencyKey, ident); !errors.Is(e, ErrConflict) {
		t.Fatal("known damaged draft interpreted as absent", e)
	}
	if _, e := s.LookupStartJournal(in.IdempotencyKey, ident); !errors.Is(e, ErrConflict) {
		t.Fatal("damaged history hidden as absent", e)
	}
}

func TestStartJournalPreparedRowWithCommittedMappingCannotBeSealed(t *testing.T) {
	s, _ := openFixture(t)
	in, req := onceFixture(t, s)
	ident := identityOf(in)
	if _, e := s.PrepareStart(in.IdempotencyKey, ident); e != nil {
		t.Fatal(e)
	}
	// Damage the coupling trigger to simulate an inconsistent journal state.
	if _, e := s.db.Exec("DROP TRIGGER commit_start_journal"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.StartReservedOnce(in, req); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ResolveStart(in.IdempotencyKey, ident, "abandon", ""); !errors.Is(e, ErrConflict) {
		t.Fatal("existing run mapping discarded", e)
	}
	if _, e := s.LookupStartJournal(in.IdempotencyKey, ident); !errors.Is(e, ErrConflict) {
		t.Fatal("inconsistent history accepted", e)
	}
}

func TestStartJournalPreparesOnlyExactLivePhaseAndPreservesOriginalAfterChanges(t *testing.T) {
	s, _ := openFixture(t)
	in, _ := onceFixture(t, s)
	ident := identityOf(in)
	for _, v := range []StartIdentity{{TaskID: in.TaskID, Role: in.Role, PlanRevision: 2, Generation: 0}, {TaskID: in.TaskID, Role: in.Role, PlanRevision: 1, Generation: 1}, {TaskID: in.TaskID, Role: "testing", PlanRevision: 1, Generation: 0}} {
		if _, e := s.PrepareStart("bad-new-key", v); e == nil {
			t.Fatal("invalid frozen phase prepared")
		}
	}
	original, e := s.PrepareStart(in.IdempotencyKey, ident)
	if e != nil {
		t.Fatal(e)
	}
	v := TaskVersion{PlanRevision: 1, Generation: 0, State: "ready"}
	if _, e = s.PauseTask(in.TaskID, v, "fixture-worker"); e != nil {
		t.Fatal(e)
	}
	if again, e := s.PrepareStart(in.IdempotencyKey, ident); e != nil || again.Created || !reflect.DeepEqual(again.Journal, original.Journal) {
		t.Fatal("task change reinterpreted original", e)
	}
	if _, e = s.PrepareStart("new-after-pause", ident); !errors.Is(e, ErrConflict) {
		t.Fatal("paused task prepared", e)
	}
	if _, e = s.ResolveStart(in.IdempotencyKey, ident, "abandon", ""); e != nil {
		t.Fatal("unwritten original could not be sealed", e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	if _, e = s.PendingStart("fixture-once-task"); !errors.Is(e, ErrClosed) {
		t.Fatal("closed store read", e)
	}
}

func TestStartJournalPrepareFreezesOriginalBeforeDispatchAndReopens(t *testing.T) {
	s, root := openFixture(t)
	in, _ := onceFixture(t, s)
	first, e := s.PrepareStart(in.IdempotencyKey, identityOf(in))
	if e != nil || !first.Created || first.Journal.State != "prepared" || first.Journal.RunID != "" {
		t.Fatal("prepare original", e)
	}
	task, _ := s.Task(in.TaskID)
	if first.Journal.Draft.Task != task || first.Journal.Draft.Plan.Hash == "" || first.Journal.Draft.Key != in.IdempotencyKey || first.Journal.Draft.Identity != identityOf(in) {
		t.Fatal("original request not frozen")
	}
	for _, table := range []string{"stage_runs", "start_requests", "reservations"} {
		var n int
		if e = s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); e != nil || n != 0 {
			t.Fatal("prepare executed work", table, e)
		}
	}
	budget, _ := s.Budget(in.TaskID)
	if budget.UsedCalls != 0 {
		t.Fatal("prepare consumed budget")
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	got, e := s.PendingStart(task.ProjectID)
	if e != nil || !reflect.DeepEqual(got, first.Journal) {
		t.Fatal("reopen lost original", e)
	}
	again, e := s.PrepareStart(in.IdempotencyKey, identityOf(in))
	if e != nil || again.Created || !reflect.DeepEqual(again.Journal, first.Journal) {
		t.Fatal("repeat changed original", e)
	}
	first.Journal.Draft.Plan.Bindings = nil
	got, e = s.LookupStartJournal(in.IdempotencyKey, identityOf(in))
	if e != nil || got.Draft.Plan.Bindings == nil {
		t.Fatal("caller mutated journal", e)
	}
}

func TestStartJournalRunReceiptCommitsAtomicallyAndAcknowledgesExactRun(t *testing.T) {
	s, root := openFixture(t)
	in, req := onceFixture(t, s)
	ident := identityOf(in)
	if _, e := s.PrepareStart(in.IdempotencyKey, ident); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ResolveStart(in.IdempotencyKey, ident, "acknowledge", "missing-run"); !errors.Is(e, ErrConflict) {
		t.Fatal("uncommitted acknowledged", e)
	}
	r, e := s.StartReservedOnce(in, req)
	if e != nil || !r.Created {
		t.Fatal(e)
	}
	j, e := s.PendingStart("fixture-once-task")
	if e != nil || j.State != "committed" || j.RunID != r.Run.ID {
		t.Fatal("run not atomically committed", e)
	}
	if _, e := s.ResolveStart(in.IdempotencyKey, ident, "abandon", ""); !errors.Is(e, ErrConflict) {
		t.Fatal("committed abandoned", e)
	}
	if _, e := s.ResolveStart(in.IdempotencyKey, ident, "acknowledge", "wrong-run"); !errors.Is(e, ErrConflict) {
		t.Fatal("substituted run acknowledged", e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	j, e = s.LookupStartJournal(in.IdempotencyKey, ident)
	if e != nil || j.RunID != r.Run.ID || j.State != "committed" {
		t.Fatal("recovery lost receipt", e)
	}
	retry, e := s.StartReservedOnce(in, req)
	if e != nil || retry.Created || retry.Run.ID != r.Run.ID || retry.Run.State != "unknown" {
		t.Fatal("recovery replayed work", e)
	}
	taskBefore, e := s.Task(in.TaskID)
	if e != nil {
		t.Fatal(e)
	}
	budgetBefore, e := s.Budget(in.TaskID)
	if e != nil {
		t.Fatal(e)
	}
	eventsBefore, e := s.Events(in.TaskID, 0)
	if e != nil {
		t.Fatal(e)
	}
	ack, e := s.ResolveStart(in.IdempotencyKey, ident, "acknowledge", r.Run.ID)
	if e != nil || ack.State != "acknowledged" {
		t.Fatal(e)
	}
	taskAfter, e := s.Task(in.TaskID)
	if e != nil || taskAfter != taskBefore {
		t.Fatal("ack changed Task", e)
	}
	budgetAfter, e := s.Budget(in.TaskID)
	if e != nil || budgetAfter != budgetBefore {
		t.Fatal("ack changed budget", e)
	}
	eventsAfter, e := s.Events(in.TaskID, 0)
	if e != nil || !reflect.DeepEqual(eventsBefore, eventsAfter) {
		t.Fatal("ack appended execution events", e)
	}
	var held int
	if e = s.db.QueryRow("SELECT COUNT(*) FROM reservations WHERE run_id=? AND state='held' AND stop_proof_hash=''", r.Run.ID).Scan(&held); e != nil || held != 1 {
		t.Fatal("ack inferred stop or released reservation", e)
	}
	if _, e = s.PendingStart(j.Draft.Task.ProjectID); !errors.Is(e, ErrNotFound) {
		t.Fatal("ack remains pending", e)
	}
	if again, e := s.ResolveStart(in.IdempotencyKey, ident, "acknowledge", r.Run.ID); e != nil || !reflect.DeepEqual(again, ack) {
		t.Fatal("ack not idempotent", e)
	}
	retry, e = s.StartReservedOnce(in, req)
	if e != nil || retry.Created || retry.Run.ID != r.Run.ID {
		t.Fatal("ack changed old start", e)
	}
}

func TestStartJournalAbandonSealsLateStartAndPreservesHistory(t *testing.T) {
	s, root := openFixture(t)
	in, req := onceFixture(t, s)
	ident := identityOf(in)
	if _, e := s.PrepareStart(in.IdempotencyKey, ident); e != nil {
		t.Fatal(e)
	}
	j, e := s.ResolveStart(in.IdempotencyKey, ident, "abandon", "")
	if e != nil || j.State != "abandoned" {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if _, e = s.StartReservedOnce(in, req); !errors.Is(e, ErrConflict) {
		t.Fatal("late start ignored seal", e)
	}
	if _, e = s.LookupStart(in.IdempotencyKey, ident); !errors.Is(e, ErrConflict) {
		t.Fatal("start lookup ignored seal", e)
	}
	if again, e := s.ResolveStart(in.IdempotencyKey, ident, "abandon", ""); e != nil || !reflect.DeepEqual(again, j) {
		t.Fatal("seal not idempotent", e)
	}
	if _, e = s.PendingStart(j.Draft.Task.ProjectID); !errors.Is(e, ErrNotFound) {
		t.Fatal("sealed pending", e)
	}
	in.IdempotencyKey = "next-explicit-key"
	if _, e = s.PrepareStart(in.IdempotencyKey, ident); e != nil {
		t.Fatal("history blocked new explicit request", e)
	}
	if _, e = s.db.Exec("DELETE FROM start_journals WHERE key=?", j.Draft.Key); e == nil {
		t.Fatal("history deleted")
	}
	if _, e = s.db.Exec("UPDATE start_journals SET draft_json='{}' WHERE key=?", j.Draft.Key); e == nil {
		t.Fatal("draft overwritten")
	}
}

func TestStartJournalOnlyOnePendingPerProjectAndRejectsChangedIdentity(t *testing.T) {
	s, _ := openFixture(t)
	in, _ := onceFixture(t, s)
	ident := identityOf(in)
	if _, e := s.PrepareStart(in.IdempotencyKey, ident); e != nil {
		t.Fatal(e)
	}
	if _, e := s.PrepareStart("different-key", ident); !errors.Is(e, ErrConflict) {
		t.Fatal("pending replaced", e)
	}
	for _, change := range []func(*StartIdentity){func(v *StartIdentity) { v.Generation++ }, func(v *StartIdentity) { v.PlanRevision++ }, func(v *StartIdentity) { v.Role = "testing" }, func(v *StartIdentity) { v.TaskID = "other-task" }} {
		v := ident
		change(&v)
		if _, e := s.LookupStartJournal(in.IdempotencyKey, v); !errors.Is(e, ErrConflict) {
			t.Fatal("changed journal identity", e)
		}
		if _, e := s.LookupStart(in.IdempotencyKey, v); !errors.Is(e, ErrConflict) {
			t.Fatal("changed start identity", e)
		}
	}
	otherTask, e := s.Create("second-task-in-project", CreateRequest{ProjectID: "fixture-once-task", Goal: "another task", Plan: plan(t, 1)})
	if e != nil {
		t.Fatal(e)
	}
	other := ident
	other.TaskID = otherTask.ID
	if _, e = s.PrepareStart("other-task-key", other); !errors.Is(e, ErrConflict) {
		t.Fatal("project pending replaced by another task", e)
	}
	otherTask, otherReq := scheduledTask(t, s, "independent-project")
	gen := otherTask.Generation
	otherReq.ExpectedGeneration = &gen
	if _, e = s.PrepareStart("independent-project-key", identityOf(otherReq)); e != nil {
		t.Fatal("unrelated project blocked", e)
	}
	for _, key := range []string{"", "white space"} {
		if _, e := s.PrepareStart(key, ident); !errors.Is(e, ErrInvalid) {
			t.Fatal("bad key", e)
		}
	}
	if _, e := s.ResolveStart(in.IdempotencyKey, ident, "reset", ""); !errors.Is(e, ErrInvalid) {
		t.Fatal("arbitrary resolve", e)
	}
}

func TestStartJournalAbandonAndStartHaveOneDurableWinner(t *testing.T) {
	for n := 0; n < 12; n++ {
		s, _ := openFixture(t)
		in, req := onceFixture(t, s)
		ident := identityOf(in)
		if _, e := s.PrepareStart(in.IdempotencyKey, ident); e != nil {
			t.Fatal(e)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		var started StartReceipt
		var sealed StartJournal
		var startErr, sealErr error
		gate := make(chan struct{})
		go func() { defer wg.Done(); <-gate; started, startErr = s.StartReservedOnce(in, req) }()
		go func() {
			defer wg.Done()
			<-gate
			sealed, sealErr = s.ResolveStart(in.IdempotencyKey, ident, "abandon", "")
		}()
		close(gate)
		wg.Wait()
		if startErr == nil {
			if !started.Created || !errors.Is(sealErr, ErrConflict) {
				t.Fatal("double race winner", startErr, sealErr)
			}
		} else {
			if !errors.Is(startErr, ErrConflict) || sealErr != nil || sealed.State != "abandoned" {
				t.Fatal("no durable winner", startErr, sealErr)
			}
		}
		var runs, budget, reservations int
		if e := s.db.QueryRow("SELECT COUNT(*) FROM stage_runs").Scan(&runs); e != nil {
			t.Fatal(e)
		}
		if e := s.db.QueryRow("SELECT COUNT(*) FROM reservations").Scan(&reservations); e != nil {
			t.Fatal(e)
		}
		if e := s.db.QueryRow("SELECT used_calls FROM task_budgets WHERE task_id=?", in.TaskID).Scan(&budget); e != nil {
			t.Fatal(e)
		}
		want := 0
		if startErr == nil {
			want = 1
		}
		if runs != want || reservations != want || budget != 0 {
			t.Fatal("race consumed duplicate work")
		}
	}
}

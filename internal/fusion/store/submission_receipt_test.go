package store

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

func submissionInput(t *testing.T) CreateRequest {
	return CreateRequest{ProjectID: "fixture-project", Goal: "durable receipt", Plan: plan(t, 1), Budget: &Budget{MaxCalls: 7, MaxReworks: 1}}
}

func TestSubmissionReceiptFailureRollsBackWholeCreation(t *testing.T) {
	s, root := openFixture(t)
	if _, e := s.db.Exec("CREATE TRIGGER fixture_receipt_fail BEFORE INSERT ON task_submissions BEGIN SELECT RAISE(ABORT,'fixture'); END"); e != nil {
		t.Fatal(e)
	}
	in := submissionInput(t)
	preset, e := s.SavePreset(in.ProjectID, "fixture-receipt-preset", 0, presetInput(t))
	if e != nil {
		t.Fatal(e)
	}
	in.Preset = &PresetRef{ID: preset.Preset.ID, Revision: preset.Preset.Revision, Hash: preset.Preset.Hash}
	if _, e := s.CreateSubmission("preview-atomic", "key-atomic", in, DefaultStamp{}); e == nil {
		t.Fatal("failed receipt acknowledged")
	}
	for _, table := range []string{"tasks", "plan_revisions", "task_budgets", "task_preset_refs", "idempotency", "events", "task_submissions"} {
		var count int
		if e := s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); e != nil || count != 0 {
			t.Fatal("partial task survived", table, count, e)
		}
	}
	if _, e := s.db.Exec("DROP TRIGGER fixture_receipt_fail"); e != nil {
		t.Fatal(e)
	}
	created, e := s.CreateSubmission("preview-atomic", "key-atomic", in, DefaultStamp{})
	if e != nil {
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
	got, e := s.LookupSubmission("preview-atomic", "key-atomic", in.Plan.Hash)
	if e != nil || got != created {
		t.Fatal("committed receipt not durable", got, e)
	}
	if ref, e := s.TaskPreset(got.ID); e != nil || ref == nil || *ref != *in.Preset {
		t.Fatal("committed preset not durable", e)
	}
	if _, e = s.db.Exec("UPDATE task_submissions SET idempotency_key='other'"); e == nil {
		t.Fatal("receipt was mutable")
	}
	if _, e = s.db.Exec("DELETE FROM task_submissions"); e == nil {
		t.Fatal("receipt was removable")
	}
}

func TestSubmissionReceiptKeepsProjectKeyScopeAndRejectsRebinding(t *testing.T) {
	s, _ := openFixture(t)
	in := submissionInput(t)
	if _, e := s.LookupCreationSubmission("preview-a", "shared-key", in); !errors.Is(e, ErrNotFound) {
		t.Fatal("lookup created a task", e)
	}
	a, e := s.CreateSubmission("preview-a", "shared-key", in, DefaultStamp{})
	if e != nil {
		t.Fatal(e)
	}
	other := in
	other.ProjectID = "fixture-other-project"
	b, e := s.CreateSubmission("preview-b", "shared-key", other, DefaultStamp{})
	if e != nil || b.ID == a.ID {
		t.Fatal("idempotency scope became global", e)
	}
	if _, e = s.LookupCreationSubmission("preview-peer", "shared-key", in); e != nil {
		t.Fatal("genuine peer receipt refused", e)
	}
	for _, tc := range []struct {
		name, preview, key string
		input              CreateRequest
	}{
		{"project", "preview-a", "shared-key", other},
		{"key", "preview-a", "changed-key", in},
		{"payload", "preview-a", "shared-key", func() CreateRequest { v := in; v.Goal = "different"; return v }()},
		{"key-payload", "preview-new", "shared-key", func() CreateRequest { v := in; v.Budget = &Budget{MaxCalls: 8, MaxReworks: 1}; return v }()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, e := s.CreateSubmission(tc.preview, tc.key, tc.input, DefaultStamp{}); !errors.Is(e, ErrConflict) {
				t.Fatal("receipt rebound", e)
			}
		})
	}
	for _, tc := range []struct {
		preview, key, hash string
		want               error
	}{
		{"preview-a", "wrong", in.Plan.Hash, ErrConflict},
		{"preview-a", "shared-key", "tampered", ErrConflict},
		{"missing", "shared-key", in.Plan.Hash, ErrNotFound},
		{"", "shared-key", in.Plan.Hash, ErrInvalid},
		{"preview-a", "", in.Plan.Hash, ErrInvalid},
		{"preview-a", "shared-key", "", ErrInvalid},
	} {
		if _, e := s.LookupSubmission(tc.preview, tc.key, tc.hash); !errors.Is(e, tc.want) {
			t.Fatal("identity check", e)
		}
	}
	for preview, want := range map[string]string{"preview-a": a.ID, "preview-peer": a.ID, "preview-b": b.ID} {
		if got, e := s.LookupSubmission(preview, "shared-key", in.Plan.Hash); e != nil || got.ID != want {
			t.Fatal("project mapping changed", preview, e)
		}
	}
	var count int
	if e = s.db.QueryRow("SELECT COUNT(*) FROM tasks").Scan(&count); e != nil || count != 2 {
		t.Fatal("conflict wrote tasks", count, e)
	}
	if _, e = s.db.Exec("INSERT INTO task_submissions VALUES('invalid','shared-key','fixture-other-project',?,?,?)", in.Plan.Hash, hash([]byte("wrong payload")), a.ID); e == nil {
		t.Fatal("inconsistent durable mapping accepted")
	}
	s.Close()
	if _, e = s.LookupSubmission("preview-a", "shared-key", in.Plan.Hash); !errors.Is(e, ErrClosed) {
		t.Fatal("closed lookup", e)
	}
}

func TestSubmissionReceiptReadsCurrentTaskWithoutResettingHistoryOrBudget(t *testing.T) {
	s, _ := openFixture(t)
	in := submissionInput(t)
	a, e := s.CreateSubmission("preview-current", "key-current", in, DefaultStamp{})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.RevisePlan(a.ID, 1, plan(t, 2)); e != nil {
		t.Fatal(e)
	}
	next, e := s.Plan(a.ID, 2)
	if e != nil {
		t.Fatal(e)
	}
	run, e := s.StartReserved(StartRequest{TaskID: a.ID, Role: next.RequiredRoles[0], PlanRevision: 2, Owner: "fixture-worker", TTL: time.Minute, Target: *next.Bindings[next.RequiredRoles[0]].Target}, ReservationRequest{PoolKey: "fixture-pool", AdmissionHash: hash([]byte("fixture-proof")), GlobalLimit: 2})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConfirmStarted(run.ID, run.Generation, run.Owner, "fixture-session"); e != nil {
		t.Fatal(e)
	}
	if e = s.ReserveCall(run.ID, run.Generation); e != nil {
		t.Fatal(e)
	}
	if e = s.ReserveRework(a.ID, run.Generation); e != nil {
		t.Fatal(e)
	}
	current, e := s.Task(a.ID)
	if e != nil {
		t.Fatal(e)
	}
	before, e := s.Events(a.ID, 0)
	if e != nil {
		t.Fatal(e)
	}
	if got, e := s.LookupSubmission("preview-current", "key-current", in.Plan.Hash); e != nil || got != current || got.PlanRevision != 2 {
		t.Fatal("recovery returned stale creation state", e)
	}
	if got, e := s.CreateSubmission("preview-current", "key-current", in, DefaultStamp{GlobalHash: "stale"}); e != nil || got != current {
		t.Fatal("committed replay evaluated new defaults", e)
	}
	after, e := s.Events(a.ID, 0)
	if e != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("recovery changed history", e)
	}
	b, e := s.Budget(a.ID)
	if e != nil || b.UsedCalls != 1 || b.UsedReworks != 1 || b.MaxCalls != 7 {
		t.Fatal("recovery reset counters", b, e)
	}
}

func TestSubmissionReceiptConcurrentSameIdentityCommitsOnce(t *testing.T) {
	s, _ := openFixture(t)
	in := submissionInput(t)
	var wg sync.WaitGroup
	results := make(chan Task, 8)
	errorsOut := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := s.CreateSubmission("preview-concurrent", "key-concurrent", in, DefaultStamp{})
			results <- v
			errorsOut <- e
		}()
	}
	wg.Wait()
	close(results)
	close(errorsOut)
	for e := range errorsOut {
		if e != nil {
			t.Fatal(e)
		}
	}
	var taskID string
	for got := range results {
		if taskID != "" && got.ID != taskID {
			t.Fatal("concurrent duplicate task")
		}
		taskID = got.ID
	}
	events, e := s.Events(taskID, 0)
	if e != nil || len(events) != 1 {
		t.Fatal("concurrent duplicate events", e)
	}
}

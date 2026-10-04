package store

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

func journalDraft(t *testing.T, preview string) SubmissionDraft {
	return SubmissionDraft{PreviewID: preview, Key: "journal-key", Request: submissionInput(t), ExpiresAt: time.Now().Add(5 * time.Minute).UTC(), ConfigurationRevision: 1}
}
func journalIdentity(d SubmissionDraft) SubmissionIdentity {
	return SubmissionIdentity{ProjectID: d.Request.ProjectID, PreviewID: d.PreviewID, Key: d.Key, PlanHash: d.Request.Plan.Hash}
}
func TestSubmissionJournalPreparePersistsOriginalRequestBeforeTask(t *testing.T) {
	s, root := openFixture(t)
	d := journalDraft(t, "journal-original")
	first, e := s.PrepareSubmission(d, DefaultStamp{})
	if e != nil || !first.Created || first.Submission.State != "prepared" || first.Submission.TaskID != "" {
		t.Fatal("prepare", e)
	}
	for _, table := range []string{"tasks", "events", "idempotency", "task_submissions"} {
		var count int
		if e = s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); e != nil || count != 0 {
			t.Fatal("prepare created a task", table, e)
		}
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	got, e := s.PendingSubmission(d.Request.ProjectID)
	if e != nil || !reflect.DeepEqual(got, first.Submission) {
		t.Fatal("reopen lost original pending identity", e)
	}
	if got, e := s.PrepareSubmission(d, DefaultStamp{GlobalHash: "stale"}); e != nil || got.Created || !reflect.DeepEqual(got.Submission, first.Submission) {
		t.Fatal("repeat changed frozen request", e)
	}
	different := d
	different.PreviewID = "different-preview"
	if _, e = s.PrepareSubmission(different, DefaultStamp{}); !errors.Is(e, ErrConflict) {
		t.Fatal("another pending request replaced original", e)
	}
}
func TestSubmissionJournalAbandonSealsLateOriginalCommit(t *testing.T) {
	s, _ := openFixture(t)
	d := journalDraft(t, "journal-abandon")
	if _, e := s.PrepareSubmission(d, DefaultStamp{}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ResolveSubmission(journalIdentity(d), "acknowledge"); !errors.Is(e, ErrConflict) {
		t.Fatal("uncommitted request acknowledged", e)
	}
	sealed, e := s.ResolveSubmission(journalIdentity(d), "abandon")
	if e != nil || sealed.State != "abandoned" || sealed.TaskID != "" {
		t.Fatal("abandon", e)
	}
	if _, e = s.CreateSubmission(d.PreviewID, d.Key, d.Request, DefaultStamp{}); !errors.Is(e, ErrConflict) {
		t.Fatal("late original request committed after abandon", e)
	}
	if _, e = s.LookupCreationSubmission(d.PreviewID, d.Key, d.Request); !errors.Is(e, ErrConflict) {
		t.Fatal("late peer reconciliation ignored seal", e)
	}
	if _, e = s.PendingSubmission(d.Request.ProjectID); !errors.Is(e, ErrNotFound) {
		t.Fatal("abandoned request remains pending", e)
	}
	if again, e := s.ResolveSubmission(journalIdentity(d), "abandon"); e != nil || !reflect.DeepEqual(again, sealed) {
		t.Fatal("abandon not idempotent", e)
	}
	next := journalDraft(t, "journal-next")
	next.Key = "next-key"
	if _, e = s.PrepareSubmission(next, DefaultStamp{}); e != nil {
		t.Fatal("sealed history blocked next draft", e)
	}
}
func TestSubmissionJournalCommitAndAcknowledgeAreExactAndAtomic(t *testing.T) {
	s, _ := openFixture(t)
	d := journalDraft(t, "journal-commit")
	if _, e := s.PrepareSubmission(d, DefaultStamp{}); e != nil {
		t.Fatal(e)
	}
	task, e := s.CreateSubmission(d.PreviewID, d.Key, d.Request, DefaultStamp{})
	if e != nil {
		t.Fatal(e)
	}
	got, e := s.PendingSubmission(d.Request.ProjectID)
	if e != nil || got.State != "committed" || got.TaskID != task.ID {
		t.Fatal("task and journal committed separately", e)
	}
	if _, e = s.ResolveSubmission(journalIdentity(d), "abandon"); !errors.Is(e, ErrConflict) {
		t.Fatal("committed task discarded as uncommitted", e)
	}
	wrong := journalIdentity(d)
	wrong.Key = "wrong-key"
	if _, e = s.ResolveSubmission(wrong, "acknowledge"); !errors.Is(e, ErrConflict) {
		t.Fatal("wrong request acknowledged", e)
	}
	ack, e := s.ResolveSubmission(journalIdentity(d), "acknowledge")
	if e != nil || ack.State != "acknowledged" || ack.TaskID != task.ID {
		t.Fatal("acknowledge", e)
	}
	if again, e := s.ResolveSubmission(journalIdentity(d), "acknowledge"); e != nil || !reflect.DeepEqual(ack, again) {
		t.Fatal("acknowledge not idempotent", e)
	}
	if _, e = s.PendingSubmission(d.Request.ProjectID); !errors.Is(e, ErrNotFound) {
		t.Fatal("acknowledged remains pending", e)
	}
	if again, e := s.CreateSubmission(d.PreviewID, d.Key, d.Request, DefaultStamp{}); e != nil || again.ID != task.ID {
		t.Fatal("ack destroyed original task receipt", e)
	}
	events, e := s.Events(task.ID, 0)
	if e != nil || len(events) != 1 {
		t.Fatal("journal emitted execution events", e)
	}
	if b, e := s.Budget(task.ID); e != nil || b.UsedCalls != 0 || b.UsedReworks != 0 || b.MaxCalls != d.Request.Budget.MaxCalls {
		t.Fatal("journal changed budget", e)
	}
}
func TestSubmissionJournalAbandonCreationRaceHasOneDurableWinner(t *testing.T) {
	for i := 0; i < 8; i++ {
		s, _ := openFixture(t)
		d := journalDraft(t, "journal-race")
		if _, e := s.PrepareSubmission(d, DefaultStamp{}); e != nil {
			t.Fatal(e)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		var created Task
		var creationErr, abandonErr error
		go func() {
			defer wg.Done()
			created, creationErr = s.CreateSubmission(d.PreviewID, d.Key, d.Request, DefaultStamp{})
		}()
		go func() { defer wg.Done(); _, abandonErr = s.ResolveSubmission(journalIdentity(d), "abandon") }()
		wg.Wait()
		if creationErr == nil {
			if !errors.Is(abandonErr, ErrConflict) {
				t.Fatal("committed request also abandoned", abandonErr)
			}
			pending, e := s.PendingSubmission(d.Request.ProjectID)
			if e != nil || pending.State != "committed" || pending.TaskID != created.ID {
				t.Fatal("race lost committed receipt", e)
			}
		} else if abandonErr == nil {
			if !errors.Is(creationErr, ErrConflict) {
				t.Fatal("abandoned request committed", creationErr)
			}
			if _, e := s.LookupSubmission(d.PreviewID, d.Key, d.Request.Plan.Hash); !errors.Is(e, ErrNotFound) {
				t.Fatal("abandoned side effect", e)
			}
		} else {
			t.Fatal("race had no winner", creationErr, abandonErr)
		}
	}
}

func TestSubmissionJournalCommitFailureRollsBackTaskAndRetainsPreparedRequest(t *testing.T) {
	s, _ := openFixture(t)
	d := journalDraft(t, "journal-fail-commit")
	preset, e := s.SavePreset(d.Request.ProjectID, "journal-preset", 0, presetInput(t))
	if e != nil {
		t.Fatal(e)
	}
	d.Request.Preset = &PresetRef{ID: preset.Preset.ID, Revision: 1, Hash: preset.Preset.Hash}
	prepared, e := s.PrepareSubmission(d, DefaultStamp{})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec("CREATE TRIGGER fixture_journal_fail BEFORE UPDATE ON submission_journals WHEN NEW.state='committed' BEGIN SELECT RAISE(ABORT,'fixture'); END"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateSubmission(d.PreviewID, d.Key, d.Request, DefaultStamp{}); e == nil {
		t.Fatal("failed journal update acknowledged task")
	}
	for _, table := range []string{"tasks", "plan_revisions", "task_budgets", "task_preset_refs", "events", "idempotency", "task_submissions"} {
		var count int
		if e = s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); e != nil || count != 0 {
			t.Fatal("partial creation survived", table, count, e)
		}
	}
	if got, e := s.PendingSubmission(d.Request.ProjectID); e != nil || !reflect.DeepEqual(got, prepared.Submission) {
		t.Fatal("failed commit lost original request", e)
	}
	if _, e = s.db.Exec("DROP TRIGGER fixture_journal_fail"); e != nil {
		t.Fatal(e)
	}
	task, e := s.CreateSubmission(d.PreviewID, d.Key, d.Request, DefaultStamp{})
	if e != nil {
		t.Fatal(e)
	}
	if ref, e := s.TaskPreset(task.ID); e != nil || ref == nil || *ref != *d.Request.Preset {
		t.Fatal("retry lost preset", e)
	}
	if got, e := s.PendingSubmission(d.Request.ProjectID); e != nil || got.State != "committed" || got.TaskID != task.ID {
		t.Fatal("retry failed", e)
	}
}

func TestSubmissionJournalScopesImmutableIdentityAndTerminalHistory(t *testing.T) {
	s, _ := openFixture(t)
	d := journalDraft(t, "journal-scope")
	if _, e := s.PrepareSubmission(d, DefaultStamp{}); e != nil {
		t.Fatal(e)
	}
	other := journalDraft(t, "journal-other")
	other.Request.ProjectID = "fixture-other-project"
	if _, e := s.PrepareSubmission(other, DefaultStamp{}); e != nil {
		t.Fatal("project key scope changed", e)
	}
	for _, change := range []struct {
		name string
		edit func(*SubmissionDraft)
	}{
		{"key", func(v *SubmissionDraft) { v.Key = "changed" }},
		{"project", func(v *SubmissionDraft) { v.Request.ProjectID = "fixture-other-project" }},
		{"goal", func(v *SubmissionDraft) { v.Request.Goal = "changed" }},
		{"budget", func(v *SubmissionDraft) { v.Request.Budget = &Budget{MaxCalls: 8, MaxReworks: 1} }},
		{"revision", func(v *SubmissionDraft) { v.ConfigurationRevision++ }},
		{"expiry", func(v *SubmissionDraft) { v.ExpiresAt = v.ExpiresAt.Add(time.Second) }},
	} {
		t.Run(change.name, func(t *testing.T) {
			copy := d
			change.edit(&copy)
			if _, e := s.PrepareSubmission(copy, DefaultStamp{}); !errors.Is(e, ErrConflict) {
				t.Fatal("original draft overwritten", e)
			}
		})
	}
	for _, identity := range []SubmissionIdentity{
		{ProjectID: other.Request.ProjectID, PreviewID: d.PreviewID, Key: d.Key, PlanHash: d.Request.Plan.Hash},
		{ProjectID: d.Request.ProjectID, PreviewID: d.PreviewID, Key: "changed", PlanHash: d.Request.Plan.Hash},
		{ProjectID: d.Request.ProjectID, PreviewID: d.PreviewID, Key: d.Key, PlanHash: "changed"},
	} {
		if _, e := s.ResolveSubmission(identity, "abandon"); !errors.Is(e, ErrConflict) {
			t.Fatal("wrong identity sealed request", e)
		}
	}
	for _, q := range []string{
		"UPDATE submission_journals SET draft_json='{}'",
		"UPDATE submission_journals SET project_id='changed'",
		"UPDATE submission_journals SET state='acknowledged'",
		"DELETE FROM submission_journals",
	} {
		if _, e := s.db.Exec(q); e == nil {
			t.Fatal("journal invariant writable")
		}
	}
	sealed, e := s.ResolveSubmission(journalIdentity(d), "abandon")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.PrepareSubmission(journalDraft(t, "journal-new"), DefaultStamp{}); e != nil {
		t.Fatal("terminal history blocked new request", e)
	}
	if old, e := s.PrepareSubmission(d, DefaultStamp{}); e != nil || old.Created || !reflect.DeepEqual(old.Submission, sealed) {
		t.Fatal("terminal prepare retry reactivated request", e)
	}
	if _, e = s.CreateSubmission(d.PreviewID, d.Key, d.Request, DefaultStamp{}); !errors.Is(e, ErrConflict) {
		t.Fatal("terminal retry unsealed original", e)
	}
	if pending, e := s.PendingSubmission(other.Request.ProjectID); e != nil || pending.Draft.PreviewID != other.PreviewID {
		t.Fatal("other project changed", e)
	}
}

func TestSubmissionJournalNewPrepareRequiresCurrentDefaultsAndValidFrozenRequest(t *testing.T) {
	s, _ := openFixture(t)
	d := journalDraft(t, "journal-defaults")
	if _, e := s.PrepareSubmission(d, DefaultStamp{GlobalHash: "stale"}); !errors.Is(e, ErrDefaultsChanged) {
		t.Fatal("stale new preparation accepted", e)
	}
	if _, e := s.PendingSubmission(d.Request.ProjectID); !errors.Is(e, ErrNotFound) {
		t.Fatal("failed prepare persisted", e)
	}
	for _, change := range []func(*SubmissionDraft){
		func(v *SubmissionDraft) { v.PreviewID = "" }, func(v *SubmissionDraft) { v.Key = "" },
		func(v *SubmissionDraft) { v.ConfigurationRevision = 0 }, func(v *SubmissionDraft) { v.ExpiresAt = time.Time{} },
		func(v *SubmissionDraft) { v.Request.Goal = "" }, func(v *SubmissionDraft) { v.Request.Plan.Hash = "bad" },
		func(v *SubmissionDraft) { v.Request.Budget = &Budget{MaxCalls: 7, UsedCalls: 1} },
	} {
		copy := d
		change(&copy)
		if _, e := s.PrepareSubmission(copy, DefaultStamp{}); !errors.Is(e, ErrInvalid) {
			t.Fatal("invalid frozen preparation accepted", e)
		}
	}
	if _, e := s.ResolveSubmission(journalIdentity(d), "unknown"); !errors.Is(e, ErrInvalid) {
		t.Fatal("unknown resolution accepted", e)
	}
	if _, e := s.PendingSubmission(""); !errors.Is(e, ErrInvalid) {
		t.Fatal("invalid project accepted", e)
	}
	if _, e := s.db.Exec("CREATE TRIGGER fixture_prepare_fail BEFORE INSERT ON submission_journals BEGIN SELECT RAISE(ABORT,'fixture'); END"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.PrepareSubmission(d, DefaultStamp{}); e == nil {
		t.Fatal("failed prepare acknowledged")
	}
	if _, e := s.db.Exec("DROP TRIGGER fixture_prepare_fail"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.PrepareSubmission(d, DefaultStamp{}); e != nil {
		t.Fatal(e)
	}
	s.Close()
	if _, e := s.PendingSubmission(d.Request.ProjectID); !errors.Is(e, ErrClosed) {
		t.Fatal("closed read", e)
	}
	if _, e := s.PrepareSubmission(d, DefaultStamp{}); !errors.Is(e, ErrClosed) {
		t.Fatal("closed prepare", e)
	}
	if _, e := s.ResolveSubmission(journalIdentity(d), "abandon"); !errors.Is(e, ErrClosed) {
		t.Fatal("closed resolution", e)
	}
}

func TestSubmissionJournalPeerReceiptAndConsumedTaskRemainSameAfterReopen(t *testing.T) {
	s, root := openFixture(t)
	d := journalDraft(t, "journal-peer")
	created, e := s.Create(d.Key, d.Request)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.PrepareSubmission(d, DefaultStamp{}); e != nil {
		t.Fatal(e)
	}
	if got, e := s.LookupCreationSubmission(d.PreviewID, d.Key, d.Request); e != nil || got.ID != created.ID {
		t.Fatal("peer reconciliation created task", e)
	}
	if got, e := s.PendingSubmission(d.Request.ProjectID); e != nil || got.State != "committed" || got.TaskID != created.ID {
		t.Fatal("peer receipt not journaled", e)
	}
	if e = s.RevisePlan(created.ID, 1, plan(t, 2)); e != nil {
		t.Fatal(e)
	}
	snapshot, e := s.Plan(created.ID, 2)
	if e != nil {
		t.Fatal(e)
	}
	run, e := s.StartReserved(StartRequest{TaskID: created.ID, Role: snapshot.RequiredRoles[0], PlanRevision: 2, Owner: "journal-worker", TTL: time.Minute, Target: *snapshot.Bindings[snapshot.RequiredRoles[0]].Target}, ReservationRequest{PoolKey: "journal-pool", AdmissionHash: hash([]byte("fixture-proof")), GlobalLimit: 2})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConfirmStarted(run.ID, run.Generation, run.Owner, "fixture-session"); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		if e = s.ReserveCall(run.ID, run.Generation); e != nil {
			t.Fatal(e)
		}
	}
	if e = s.ReserveRework(created.ID, run.Generation); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ResolveSubmission(journalIdentity(d), "acknowledge"); e != nil {
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
	if got, e := s.LookupSubmission(d.PreviewID, d.Key, d.Request.Plan.Hash); e != nil || got.ID != created.ID || got.PlanRevision != 2 {
		t.Fatal("journal recovery followed new plan or lost task", e)
	}
	if _, e = s.PendingSubmission(d.Request.ProjectID); !errors.Is(e, ErrNotFound) {
		t.Fatal("acknowledged journal reappeared", e)
	}
	if budget, e := s.Budget(created.ID); e != nil || budget.UsedCalls != 2 || budget.UsedReworks != 1 {
		t.Fatal("journal/reopen refunded consumed counters", e)
	}
	if again, e := s.ResolveSubmission(journalIdentity(d), "acknowledge"); e != nil || again.TaskID != created.ID || again.State != "acknowledged" {
		t.Fatal("reopen lost resolution receipt", e)
	}
}

func TestSubmissionJournalCorruptFrozenRequestCannotCommit(t *testing.T) {
	s, _ := openFixture(t)
	d := journalDraft(t, "journal-corrupt")
	// Inject a corrupted fixture row without disabling any production trigger.
	_, payloadHash, raw, e := canonicalSubmissionDraft(d)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := s.db.Exec("INSERT INTO submission_journals VALUES(?,?,?,?,?,?,?,'prepared',NULL)", d.PreviewID, d.Request.ProjectID, d.Key, d.Request.Plan.Hash, payloadHash, string(raw), "fixture-corruption"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.PendingSubmission(d.Request.ProjectID); !errors.Is(e, ErrConflict) {
		t.Fatal("corrupt recovery accepted", e)
	}
	if _, e := s.ResolveSubmission(journalIdentity(d), "abandon"); !errors.Is(e, ErrConflict) {
		t.Fatal("corrupt record resolved", e)
	}
	if _, e := s.CreateSubmission(d.PreviewID, d.Key, d.Request, DefaultStamp{}); !errors.Is(e, ErrConflict) {
		t.Fatal("corrupt original request committed", e)
	}
}

func TestSubmissionJournalResolutionFailureKeepsOriginalPendingAndThenRetries(t *testing.T) {
	s, _ := openFixture(t)
	d := journalDraft(t, "journal-resolve-fail")
	first, e := s.PrepareSubmission(d, DefaultStamp{})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec("CREATE TRIGGER fixture_resolve_fail BEFORE UPDATE ON submission_journals WHEN NEW.state='abandoned' BEGIN SELECT RAISE(ABORT,'fixture'); END"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ResolveSubmission(journalIdentity(d), "abandon"); e == nil {
		t.Fatal("failed resolution acknowledged")
	}
	if got, e := s.PendingSubmission(d.Request.ProjectID); e != nil || !reflect.DeepEqual(got, first.Submission) {
		t.Fatal("failed resolution lost original pending request", e)
	}
	if _, e = s.PrepareSubmission(journalDraft(t, "another"), DefaultStamp{}); !errors.Is(e, ErrConflict) {
		t.Fatal("failed resolution released slot", e)
	}
	if _, e = s.db.Exec("DROP TRIGGER fixture_resolve_fail"); e != nil {
		t.Fatal(e)
	}
	if got, e := s.ResolveSubmission(journalIdentity(d), "abandon"); e != nil || got.State != "abandoned" {
		t.Fatal("resolution retry failed", e)
	}
}

func TestSubmissionJournalPrepareRecognizesProvedCommittedReceipt(t *testing.T) {
	s, _ := openFixture(t)
	d := journalDraft(t, "journal-already-committed")
	task, e := s.CreateSubmission(d.PreviewID, d.Key, d.Request, DefaultStamp{})
	if e != nil {
		t.Fatal(e)
	}
	result, e := s.PrepareSubmission(d, DefaultStamp{GlobalHash: "stale"})
	if e != nil || !result.Created || result.Submission.State != "committed" || result.Submission.TaskID != task.ID {
		t.Fatal("known commit was prepared as uncommitted", e)
	}
	if _, e = s.ResolveSubmission(journalIdentity(d), "acknowledge"); e != nil {
		t.Fatal(e)
	}
	events, e := s.Events(task.ID, 0)
	if e != nil || len(events) != 1 {
		t.Fatal("late preparation created events", e)
	}
}

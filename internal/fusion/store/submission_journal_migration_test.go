package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func schemaSixJournalFixture(t *testing.T) (string, CreateRequest, Task, DefaultLayerReceipt) {
	t.Helper()
	root, in, task, defaults := schemaFiveSubmissionFixture(t)
	db, e := sql.Open("sqlite", filepath.Join(root, "fusion.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec(migrationSix); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("INSERT INTO metadata VALUES('migration_006_sha256',?)", hash([]byte(migrationSix))); e != nil {
		t.Fatal(e)
	}
	_, payloadHash, e := creationPayload("fixture-schema4-key", in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("INSERT INTO task_submissions VALUES('legacy-preview','fixture-schema4-key',?,?,?,?)", in.ProjectID, in.Plan.Hash, payloadHash, task.ID); e != nil {
		t.Fatal(e)
	}
	var version int
	if e = db.QueryRow("PRAGMA user_version").Scan(&version); e != nil || version != 6 {
		t.Fatal("fixture not schema6", version, e)
	}
	return root, in, task, defaults
}

func TestSubmissionJournalMigrationSevenPreservesReceiptsAndLegacyHistory(t *testing.T) {
	root, in, original, defaults := schemaSixJournalFixture(t)
	s, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var version, count int
	if e = s.db.QueryRow("PRAGMA user_version").Scan(&version); e != nil || version != 9 {
		t.Fatal("current schema8 missing", e)
	}
	if e = s.db.QueryRow("SELECT COUNT(*) FROM submission_journals").Scan(&count); e != nil || count != 0 {
		t.Fatal("migration guessed pending input", e)
	}
	if got, e := s.LookupSubmission("legacy-preview", "fixture-schema4-key", in.Plan.Hash); e != nil || got != original {
		t.Fatal("legacy HTTP receipt changed", e)
	}
	if got, e := s.LookupCreation("fixture-schema4-key", in); e != nil || got != original {
		t.Fatal("legacy payload changed", e)
	}
	if got, e := s.Plan(original.ID, 1); e != nil || !reflect.DeepEqual(got, in.Plan) {
		t.Fatal("original plan changed", e)
	}
	if got, e := s.TaskPreset(original.ID); e != nil || got == nil || *got != *in.Preset {
		t.Fatal("preset changed", e)
	}
	if got, e := s.DefaultLayers(in.ProjectID); e != nil || got.Project == nil || !reflect.DeepEqual(*got.Project, defaults.DefaultLayer) {
		t.Fatal("defaults changed", e)
	}
	if got, e := s.Budget(original.ID); e != nil || got.UsedCalls != 3 || got.UsedReworks != 1 || got.MaxCalls != 9 {
		t.Fatal("budget changed", e)
	}
	if got, e := s.Events(original.ID, 0); e != nil || len(got) != 1 || got[0].Kind != "created" {
		t.Fatal("history changed", e)
	}
	if _, e = s.db.Exec("UPDATE metadata SET value='fixture-drift' WHERE key='migration_007_sha256'"); e != nil {
		t.Fatal(e)
	}
	s.Close()
	if opened, e := Open(root); !errors.Is(e, ErrUnsupported) {
		if opened != nil {
			opened.Close()
		}
		t.Fatal("schema7 checksum drift accepted", e)
	}
}

func TestSubmissionJournalMigrationFailureLeavesSchemaSixReceiptAndRetries(t *testing.T) {
	root, in, original, _ := schemaSixJournalFixture(t)
	db, e := sql.Open("sqlite", filepath.Join(root, "fusion.db"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("CREATE TRIGGER fixture_migration_seven_fail BEFORE INSERT ON metadata WHEN NEW.key='migration_007_sha256' BEGIN SELECT RAISE(ABORT,'fixture'); END"); e != nil {
		t.Fatal(e)
	}
	db.Close()
	if opened, e := Open(root); e == nil {
		if opened != nil {
			opened.Close()
		}
		t.Fatal("failed migration acknowledged")
	}
	db, e = sql.Open("sqlite", filepath.Join(root, "fusion.db"))
	if e != nil {
		t.Fatal(e)
	}
	var version, count int
	if e = db.QueryRow("PRAGMA user_version").Scan(&version); e != nil || version != 6 {
		t.Fatal("failed migration changed version", e)
	}
	if e = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name IN ('submission_journals','one_pending_submission_per_project','immutable_submission_draft','submission_journal_transition','validate_journal_task_insert','validate_journal_task_update','preserve_submission_journals','reject_abandoned_submission')").Scan(&count); e != nil || count != 0 {
		t.Fatal("partial migration persisted", count, e)
	}
	var taskID string
	if e = db.QueryRow("SELECT task_id FROM task_submissions WHERE preview_id='legacy-preview'").Scan(&taskID); e != nil || taskID != original.ID {
		t.Fatal("rollback damaged original receipt", e)
	}
	if _, e = db.Exec("DROP TRIGGER fixture_migration_seven_fail"); e != nil {
		t.Fatal(e)
	}
	db.Close()
	s, e := Open(root)
	if e != nil {
		t.Fatal("retry failed", e)
	}
	defer s.Close()
	if got, e := s.LookupSubmission("legacy-preview", "fixture-schema4-key", in.Plan.Hash); e != nil || got != original {
		t.Fatal("retry lost receipt", e)
	}
}

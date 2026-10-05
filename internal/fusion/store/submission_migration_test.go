package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func schemaFiveSubmissionFixture(t *testing.T) (string, CreateRequest, Task, DefaultLayerReceipt) {
	t.Helper()
	root, in, id := schemaFourDefaultsFixture(t)
	db, e := sql.Open("sqlite", filepath.Join(root, "fusion.db"))
	if e != nil {
		t.Fatal(e)
	}
	s := &Store{db: db, now: time.Now}
	defer s.Close()
	if _, e = db.Exec(migrationFive); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("INSERT INTO metadata VALUES('migration_005_sha256',?)", hash([]byte(migrationFive))); e != nil {
		t.Fatal(e)
	}
	d, e := s.SaveDefaultLayer("project", in.ProjectID, 0, presetInput(t).Layer)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("UPDATE task_budgets SET used_calls=3,used_reworks=1 WHERE task_id=?", id); e != nil {
		t.Fatal(e)
	}
	task, e := s.Task(id)
	if e != nil {
		t.Fatal(e)
	}
	var version int
	if e = db.QueryRow("PRAGMA user_version").Scan(&version); e != nil || version != 5 {
		t.Fatal("fixture not schema5", version, e)
	}
	return root, in, task, d
}

func TestSubmissionMigrationSixPreservesLegacyDataWithoutInventingPreview(t *testing.T) {
	root, in, original, defaults := schemaFiveSubmissionFixture(t)
	s, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var version, count int
	if e = s.db.QueryRow("PRAGMA user_version").Scan(&version); e != nil || version != 10 {
		t.Fatal("current schema missing", e)
	}
	if e = s.db.QueryRow("SELECT COUNT(*) FROM task_submissions").Scan(&count); e != nil || count != 0 {
		t.Fatal("migration invented HTTP receipt", e)
	}
	if got, e := s.LookupCreation("fixture-schema4-key", in); e != nil || got != original {
		t.Fatal("original request identity changed", e)
	}
	if got, e := s.Plan(original.ID, 1); e != nil || !reflect.DeepEqual(got, in.Plan) {
		t.Fatal("plan changed", e)
	}
	if got, e := s.TaskPreset(original.ID); e != nil || got == nil || *got != *in.Preset {
		t.Fatal("preset changed", e)
	}
	if got, e := s.Budget(original.ID); e != nil || got.UsedCalls != 3 || got.UsedReworks != 1 || got.MaxCalls != 9 {
		t.Fatal("budget changed", got, e)
	}
	if got, e := s.Events(original.ID, 0); e != nil || len(got) != 1 || got[0].Kind != "created" {
		t.Fatal("history changed", e)
	}
	if got, e := s.DefaultLayers(in.ProjectID); e != nil || got.Project == nil || !reflect.DeepEqual(*got.Project, defaults.DefaultLayer) {
		t.Fatal("defaults changed", e)
	}
	if _, e = s.LookupSubmission("guessed-preview", "fixture-schema4-key", in.Plan.Hash); !errors.Is(e, ErrNotFound) {
		t.Fatal("legacy receipt guessed", e)
	}
	// A full trusted payload can attach a genuine live preview to a legacy key.
	if got, e := s.LookupCreationSubmission("genuine-preview", "fixture-schema4-key", in); e != nil || got != original {
		t.Fatal("genuine legacy reconciliation refused", e)
	}
	if _, e = s.db.Exec("UPDATE metadata SET value='fixture-drift' WHERE key='migration_006_sha256'"); e != nil {
		t.Fatal(e)
	}
	s.Close()
	if opened, e := Open(root); !errors.Is(e, ErrUnsupported) {
		if opened != nil {
			opened.Close()
		}
		t.Fatal("schema6 checksum drift accepted", e)
	}
}

func TestSubmissionMigrationSixFailureRollsBackAndRetries(t *testing.T) {
	root, in, original, _ := schemaFiveSubmissionFixture(t)
	db, e := sql.Open("sqlite", filepath.Join(root, "fusion.db"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("CREATE TRIGGER fixture_migration_six_fail BEFORE INSERT ON metadata WHEN NEW.key='migration_006_sha256' BEGIN SELECT RAISE(ABORT,'fixture'); END"); e != nil {
		t.Fatal(e)
	}
	db.Close()
	if s, e := Open(root); e == nil {
		if s != nil {
			s.Close()
		}
		t.Fatal("failed migration acknowledged")
	}
	db, e = sql.Open("sqlite", filepath.Join(root, "fusion.db"))
	if e != nil {
		t.Fatal(e)
	}
	var version, count int
	if e = db.QueryRow("PRAGMA user_version").Scan(&version); e != nil || version != 5 {
		t.Fatal("failed migration changed version", version, e)
	}
	if e = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name IN ('task_submissions','validate_task_submission','immutable_task_submissions','preserve_task_submissions')").Scan(&count); e != nil || count != 0 {
		t.Fatal("partial migration persisted", count, e)
	}
	var digest string
	if e = db.QueryRow("SELECT payload_hash FROM idempotency WHERE project_id=? AND key='fixture-schema4-key'", in.ProjectID).Scan(&digest); e != nil {
		t.Fatal(e)
	}
	_, want, e := creationPayload("fixture-schema4-key", in)
	if e != nil || digest != want {
		t.Fatal("rollback changed original payload", e)
	}
	if _, e = db.Exec("DROP TRIGGER fixture_migration_six_fail"); e != nil {
		t.Fatal(e)
	}
	db.Close()
	s, e := Open(root)
	if e != nil {
		t.Fatal("migration retry failed", e)
	}
	defer s.Close()
	if got, e := s.LookupCreation("fixture-schema4-key", in); e != nil || got != original {
		t.Fatal("retry lost original task", e)
	}
}

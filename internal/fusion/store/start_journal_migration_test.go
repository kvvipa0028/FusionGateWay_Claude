package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func schemaSevenStartFixture(t *testing.T) (string, CreateRequest, Task) {
	t.Helper()
	root, in, task, _ := schemaSixJournalFixture(t)
	db, e := sql.Open("sqlite", filepath.Join(root, "fusion.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec(migrationSeven); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("INSERT INTO metadata VALUES('migration_007_sha256',?)", hash([]byte(migrationSeven))); e != nil {
		t.Fatal(e)
	}
	var v int
	if e = db.QueryRow("PRAGMA user_version").Scan(&v); e != nil || v != 7 {
		t.Fatal("fixture is not schema7", e)
	}
	return root, in, task
}

func TestStartJournalMigrationEightPreservesLegacyDataWithoutGuessingStarts(t *testing.T) {
	root, in, task := schemaSevenStartFixture(t)
	s, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	var version, count int
	if e = s.db.QueryRow("PRAGMA user_version").Scan(&version); e != nil || version != 11 {
		t.Fatal("schema8 missing", e)
	}
	if e = s.db.QueryRow("SELECT COUNT(*) FROM start_journals").Scan(&count); e != nil || count != 0 {
		t.Fatal("migration guessed old UI key", e)
	}
	if got, e := s.LookupSubmission("legacy-preview", "fixture-schema4-key", in.Plan.Hash); e != nil || got != task {
		t.Fatal("legacy receipt changed", e)
	}
	if got, e := s.Plan(task.ID, 1); e != nil || !reflect.DeepEqual(got, in.Plan) {
		t.Fatal("legacy plan changed", e)
	}
	if got, e := s.Budget(task.ID); e != nil || got.UsedCalls != 3 || got.UsedReworks != 1 || got.MaxCalls != 9 {
		t.Fatal("legacy budget changed", e)
	}
	if _, e = s.db.Exec("UPDATE metadata SET value='fixture-drift' WHERE key='migration_008_sha256'"); e != nil {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	if opened, e := Open(root); !errors.Is(e, ErrUnsupported) {
		if opened != nil {
			opened.Close()
		}
		t.Fatal("schema8 drift accepted", e)
	}
}

func TestStartJournalMigrationEightFailureKeepsSevenAndRetries(t *testing.T) {
	root, in, task := schemaSevenStartFixture(t)
	db, e := sql.Open("sqlite", filepath.Join(root, "fusion.db"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("CREATE TRIGGER fixture_eight_fail BEFORE INSERT ON metadata WHEN NEW.key='migration_008_sha256' BEGIN SELECT RAISE(ABORT,'fixture'); END"); e != nil {
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
	if e = db.QueryRow("PRAGMA user_version").Scan(&version); e != nil || version != 7 {
		t.Fatal("failed migration changed version", e)
	}
	if e = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name='start_journals'").Scan(&count); e != nil || count != 0 {
		t.Fatal("partial table survived", e)
	}
	var id string
	if e = db.QueryRow("SELECT task_id FROM task_submissions WHERE preview_id='legacy-preview'").Scan(&id); e != nil || id != task.ID {
		t.Fatal("legacy receipt lost", e)
	}
	if _, e = db.Exec("DROP TRIGGER fixture_eight_fail"); e != nil {
		t.Fatal(e)
	}
	db.Close()
	s, e := Open(root)
	if e != nil {
		t.Fatal("failed migration lock prevented retry", e)
	}
	defer s.Close()
	if got, e := s.LookupSubmission("legacy-preview", "fixture-schema4-key", in.Plan.Hash); e != nil || got != task {
		t.Fatal("retry changed receipt", e)
	}
}

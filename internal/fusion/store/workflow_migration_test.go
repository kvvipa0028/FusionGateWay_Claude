package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func schemaEightWorkflowFixture(t *testing.T) (string, CreateRequest, Task) {
	t.Helper()
	root, in, task := schemaSevenStartFixture(t)
	db, e := sql.Open("sqlite", filepath.Join(root, "fusion.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec(migrationEight); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("INSERT INTO metadata VALUES('migration_008_sha256',?)", hash([]byte(migrationEight))); e != nil {
		t.Fatal(e)
	}
	var version int
	if e = db.QueryRow("PRAGMA user_version").Scan(&version); e != nil || version != 8 {
		t.Fatal("fixture not schema8", e)
	}
	return root, in, task
}

func TestWorkflowMigrationNinePreservesLegacyAndRejectsChecksumDrift(t *testing.T) {
	root, in, task := schemaEightWorkflowFixture(t)
	s, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	var version int
	if e = s.db.QueryRow("PRAGMA user_version").Scan(&version); e != nil || version != 10 {
		t.Fatal("schema9 missing", e)
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
	if _, e = s.Workflow(task.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("migration invented workflow", e)
	}
	for _, table := range []string{"task_workflows", "workflow_designs", "workflow_approvals", "start_journals"} {
		var count int
		if e = s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); e != nil || count != 0 {
			t.Fatal("migration invented history", table, e)
		}
	}
	if _, e = s.db.Exec("UPDATE metadata SET value='fixture-drift' WHERE key='migration_009_sha256'"); e != nil {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	if opened, e := Open(root); !errors.Is(e, ErrUnsupported) {
		if opened != nil {
			opened.Close()
		}
		t.Fatal("schema9 drift accepted", e)
	}
}

func TestWorkflowMigrationNineFailureRollsBackAndRetries(t *testing.T) {
	root, in, task := schemaEightWorkflowFixture(t)
	db, e := sql.Open("sqlite", filepath.Join(root, "fusion.db"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("CREATE TRIGGER fixture_nine_fail BEFORE INSERT ON metadata WHEN NEW.key='migration_009_sha256' BEGIN SELECT RAISE(ABORT,'fixture'); END"); e != nil {
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
	if e = db.QueryRow("PRAGMA user_version").Scan(&version); e != nil || version != 8 {
		t.Fatal("failed migration changed version", e)
	}
	if e = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name IN ('task_workflows','workflow_designs','workflow_approvals','immutable_task_workflows','validate_workflow_design','validate_workflow_approval')").Scan(&count); e != nil || count != 0 {
		t.Fatal("partial schema survived", e)
	}
	var id string
	if e = db.QueryRow("SELECT task_id FROM task_submissions WHERE preview_id='legacy-preview'").Scan(&id); e != nil || id != task.ID {
		t.Fatal("legacy receipt lost", e)
	}
	if _, e = db.Exec("DROP TRIGGER fixture_nine_fail"); e != nil {
		t.Fatal(e)
	}
	db.Close()
	s, e := Open(root)
	if e != nil {
		t.Fatal("retry failed", e)
	}
	defer s.Close()
	if got, e := s.LookupSubmission("legacy-preview", "fixture-schema4-key", in.Plan.Hash); e != nil || got != task {
		t.Fatal("retry changed receipt", e)
	}
	if e = s.db.QueryRow("PRAGMA user_version").Scan(&version); e != nil || version != 10 {
		t.Fatal("retry not schema9", e)
	}
}

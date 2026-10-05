package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func schemaNineArtifactFixture(t *testing.T) (string, CreateRequest, Task) {
	t.Helper()
	root, in, task := schemaEightWorkflowFixture(t)
	db, err := sql.Open("sqlite", filepath.Join(root, "fusion.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(migrationNine); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO metadata VALUES('migration_009_sha256',?)", hash([]byte(migrationNine))); err != nil {
		t.Fatal(err)
	}
	return root, in, task
}
func TestArtifactMigrationTenPreservesLegacyAndRejectsDrift(t *testing.T) {
	root, in, task := schemaNineArtifactFixture(t)
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	var version, count int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 12 {
		t.Fatal("schema10", err)
	}
	if got, err := s.LookupSubmission("legacy-preview", "fixture-schema4-key", in.Plan.Hash); err != nil || got != task {
		t.Fatal("legacy changed", err)
	}
	if err := s.db.QueryRow("SELECT COUNT(*) FROM stage_artifacts").Scan(&count); err != nil || count != 0 {
		t.Fatal("migration invented artifacts", err)
	}
	if _, err := s.db.Exec("UPDATE metadata SET value='drift' WHERE key='migration_010_sha256'"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if opened, err := Open(root); !errors.Is(err, ErrUnsupported) {
		if opened != nil {
			opened.Close()
		}
		t.Fatal("schema10 drift accepted", err)
	}
}
func TestArtifactMigrationFailureRollsBackAndRetries(t *testing.T) {
	root, in, task := schemaNineArtifactFixture(t)
	db, err := sql.Open("sqlite", filepath.Join(root, "fusion.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TRIGGER fail_artifact_migration BEFORE INSERT ON metadata WHEN NEW.key='migration_010_sha256' BEGIN SELECT RAISE(ABORT,'fixture'); END"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if s, err := Open(root); err == nil {
		if s != nil {
			s.Close()
		}
		t.Fatal("migration failure acknowledged")
	}
	db, err = sql.Open("sqlite", filepath.Join(root, "fusion.db"))
	if err != nil {
		t.Fatal(err)
	}
	var version, count int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 9 {
		t.Fatal("failed migration changed version", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name IN ('stage_artifacts','immutable_stage_artifacts','preserve_stage_artifacts','validate_stage_artifact')").Scan(&count); err != nil || count != 0 {
		t.Fatal("partial schema10 survived", err)
	}
	if _, err := db.Exec("DROP TRIGGER fail_artifact_migration"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(root)
	if err != nil {
		t.Fatal("retry", err)
	}
	defer s.Close()
	if got, err := s.LookupSubmission("legacy-preview", "fixture-schema4-key", in.Plan.Hash); err != nil || got != task {
		t.Fatal("retry changed legacy", err)
	}
}

package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func schemaElevenReworkFixture(t *testing.T) string {
	t.Helper()
	root, _, _ := schemaNineArtifactFixture(t)
	db, e := sql.Open("sqlite", filepath.Join(root, "fusion.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	for _, m := range []struct{ sql, key string }{{migrationTen, "migration_010_sha256"}, {migrationEleven, "migration_011_sha256"}} {
		if _, e = db.Exec(m.sql); e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec("INSERT INTO metadata VALUES(?,?)", m.key, hash([]byte(m.sql))); e != nil {
			t.Fatal(e)
		}
	}
	return root
}

func TestReworkMigrationTwelvePreservesElevenAndFailureRollsBack(t *testing.T) {
	root := schemaElevenReworkFixture(t)
	db, e := sql.Open("sqlite", filepath.Join(root, "fusion.db"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`CREATE TRIGGER reject_rework_migration BEFORE INSERT ON metadata WHEN NEW.key='migration_012_sha256' BEGIN SELECT RAISE(ABORT,'fixture'); END`); e != nil {
		t.Fatal(e)
	}
	db.Close()
	if s, e := Open(root); e == nil {
		s.Close()
		t.Fatal("failed migration committed")
	}
	db, e = sql.Open("sqlite", filepath.Join(root, "fusion.db"))
	if e != nil {
		t.Fatal(e)
	}
	var version, count int
	if e = db.QueryRow("PRAGMA user_version").Scan(&version); e != nil || version != 11 {
		t.Fatal("migration failure changed schema", e, version)
	}
	if e = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name='workflow_reworks'").Scan(&count); e != nil || count != 0 {
		t.Fatal("partial rework schema survived", e)
	}
	if _, e = db.Exec("DROP TRIGGER reject_rework_migration"); e != nil {
		t.Fatal(e)
	}
	db.Close()
	s, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.db.QueryRow("PRAGMA user_version").Scan(&version); e != nil || version != 12 {
		t.Fatal("migration retry failed", e)
	}
	if e = s.db.QueryRow("SELECT COUNT(*) FROM workflow_reworks").Scan(&count); e != nil || count != 0 {
		t.Fatal("migration invented authorized round", e)
	}
	if _, e = s.db.Exec("UPDATE metadata SET value='changed' WHERE key='migration_012_sha256'"); e != nil {
		t.Fatal(e)
	}
	s.Close()
	if s, e := Open(root); !errors.Is(e, ErrUnsupported) {
		if s != nil {
			s.Close()
		}
		t.Fatal("migration checksum drift accepted", e)
	}
}

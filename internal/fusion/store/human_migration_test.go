//go:build darwin

package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func schemaTenHumanFixture(t *testing.T) (string, string, ArtifactRecord) {
	t.Helper()
	root, _, legacy := schemaNineArtifactFixture(t)
	db, err := sql.Open("sqlite", filepath.Join(root, "fusion.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(migrationTen); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO metadata VALUES('migration_010_sha256',?)", hash([]byte(migrationTen))); err != nil {
		t.Fatal(err)
	}
	s := &Store{db: db, now: time.Now}
	a, _, _, _ := acceptanceFixture(t, s)
	if err := s.RecordAcceptanceArtifactAuthorized(a, humanAcceptedFixture, func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	if err := s.ReleaseReserved(a.Reference.Binding.RunID, a.Reference.Binding.Generation, a.Reference.StopProofHash, true); err != nil {
		t.Fatal(err)
	}
	got, err := s.Artifact(a.Reference.Binding.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	return root, legacy.ID, got
}
func TestHumanMigrationElevenPreservesActualReceiptAndNoHumanDecisionInvented(t *testing.T) {
	root, legacy, a := schemaTenHumanFixture(t)
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var version, count int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 12 {
		t.Fatal("schema11", err, version)
	}
	if _, err := s.Task(legacy); err != nil {
		t.Fatal("legacy task lost", err)
	}
	if got, err := s.Artifact(a.Reference.Binding.RunID); err != nil || !reflect.DeepEqual(got, a) {
		t.Fatal("migration changed actual model receipt", err)
	}
	if err := s.db.QueryRow("SELECT COUNT(*) FROM human_acceptance_decisions").Scan(&count); err != nil || count != 0 {
		t.Fatal("migration invented operator approval", err)
	}
}
func TestHumanMigrationFailureLeavesSchemaTenAndRetries(t *testing.T) {
	root, _, a := schemaTenHumanFixture(t)
	db, err := sql.Open("sqlite", filepath.Join(root, "fusion.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("CREATE TRIGGER fail_human_migration BEFORE INSERT ON metadata WHEN NEW.key='migration_011_sha256' BEGIN SELECT RAISE(ABORT,'fixture'); END"); err != nil {
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
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 10 {
		t.Fatal("partial migration version", err, version)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name IN ('human_acceptance_decisions','human_acceptance_task','immutable_human_acceptance','preserve_human_acceptance','validate_human_acceptance')").Scan(&count); err != nil || count != 0 {
		t.Fatal("partial schema11 survived", err, count)
	}
	if _, err = db.Exec("DROP TRIGGER fail_human_migration"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, err := s.Artifact(a.Reference.Binding.RunID); err != nil || !reflect.DeepEqual(got, a) {
		t.Fatal("retry changed model receipt", err)
	}
}
func TestHumanMigrationChecksumDriftAndFutureTwelveRefused(t *testing.T) {
	for _, mode := range []string{"checksum", "future"} {
		t.Run(mode, func(t *testing.T) {
			s, root := openFixture(t)
			query := "UPDATE metadata SET value='drift' WHERE key='migration_011_sha256'"
			if mode == "future" {
				query = "PRAGMA user_version=13"
			}
			if _, err := s.db.Exec(query); err != nil {
				t.Fatal(err)
			}
			s.Close()
			if got, err := Open(root); !errors.Is(err, ErrUnsupported) {
				if got != nil {
					got.Close()
				}
				t.Fatal("unsafe schema accepted", err)
			}
		})
	}
}

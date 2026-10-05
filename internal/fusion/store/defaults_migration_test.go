package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

func schemaFourDefaultsFixture(t *testing.T) (string, CreateRequest, string) {
	t.Helper()
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(root, 0700); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(root, "fusion.db")
	if e = os.WriteFile(path, nil, 0600); e != nil {
		t.Fatal(e)
	}
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	for _, m := range []struct{ name, sql string }{{"migration_001_sha256", migration}, {"migration_002_sha256", migrationTwo}, {"migration_003_sha256", migrationThree}, {"migration_004_sha256", migrationFour}} {
		if _, e = db.Exec(m.sql); e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec("INSERT INTO metadata VALUES(?,?)", m.name, hash([]byte(m.sql))); e != nil {
			t.Fatal(e)
		}
	}
	s := &Store{db: db, now: time.Now}
	v, e := s.SavePreset("fixture-project", "fixture-preset", 0, presetInput(t))
	if e != nil {
		t.Fatal(e)
	}
	in := CreateRequest{ProjectID: "fixture-project", Goal: "preserve schema4", Plan: plan(t, 1), Budget: &Budget{MaxCalls: 9, MaxReworks: 1}, Preset: &PresetRef{ID: v.Preset.ID, Revision: 1, Hash: v.Preset.Hash}}
	task, e := s.Create("fixture-schema4-key", in)
	if e != nil {
		t.Fatal(e)
	}
	var version int
	db.QueryRow("PRAGMA user_version").Scan(&version)
	if version != 4 {
		t.Fatal("fixture not schema4", version)
	}
	s.Close()
	return root, in, task.ID
}
func TestDefaultsMigrationFivePreservesPresetReferencesAndRejectsChecksumDrift(t *testing.T) {
	root, in, id := schemaFourDefaultsFixture(t)
	s, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var version int
	s.db.QueryRow("PRAGMA user_version").Scan(&version)
	if version != 10 {
		t.Fatal("current schema missing")
	}
	task, e := s.CreateCurrent("fixture-schema4-key", in, DefaultStamp{})
	if e != nil || task.ID != id {
		t.Fatal("old payload hash changed", e)
	}
	ref, e := s.TaskPreset(id)
	if e != nil || ref == nil || *ref != *in.Preset {
		t.Fatal("schema4 provenance changed", e)
	}
	budget, e := s.Budget(id)
	if e != nil || budget.MaxCalls != 9 || budget.UsedCalls != 0 {
		t.Fatal("budget changed", e)
	}
	events, e := s.Events(id, 0)
	if e != nil || len(events) != 1 {
		t.Fatal("migration rewrote history", e)
	}
	d, e := s.DefaultLayers(in.ProjectID)
	if e != nil || d.Global != nil || d.Project != nil {
		t.Fatal("migration invented model defaults", e)
	}
	if _, e = s.SaveDefaultLayer("global", "global", 0, stageplan.Layer{}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec("UPDATE metadata SET value='fixture-drift' WHERE key='migration_005_sha256'"); e != nil {
		t.Fatal(e)
	}
	s.Close()
	if opened, e := Open(root); !errors.Is(e, ErrUnsupported) {
		if opened != nil {
			opened.Close()
		}
		t.Fatal("schema5 drift accepted", e)
	}
}
func TestDefaultsMigrationFailureRollsBackTablesVersionAndLeavesSchemaFourData(t *testing.T) {
	root, in, id := schemaFourDefaultsFixture(t)
	path := filepath.Join(root, "fusion.db")
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("CREATE TRIGGER fixture_migration_fail BEFORE INSERT ON metadata WHEN NEW.key='migration_005_sha256' BEGIN SELECT RAISE(ABORT,'fixture'); END"); e != nil {
		t.Fatal(e)
	}
	db.Close()
	if s, e := Open(root); e == nil {
		if s != nil {
			s.Close()
		}
		t.Fatal("failed migration reported success")
	}
	db, e = sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	var version, count int
	db.QueryRow("PRAGMA user_version").Scan(&version)
	db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name='default_layer_heads'").Scan(&count)
	if version != 4 || count != 0 {
		t.Fatal("partial migration persisted", version, count)
	}
	var hashValue string
	db.QueryRow("SELECT hash FROM task_preset_refs WHERE task_id=?", id).Scan(&hashValue)
	if hashValue != in.Preset.Hash {
		t.Fatal("failed migration changed task reference")
	}
	if _, e = db.Exec("DROP TRIGGER fixture_migration_fail"); e != nil {
		t.Fatal(e)
	}
	db.Close()
	s, e := Open(root)
	if e != nil {
		t.Fatal("rollback prevented retry", e)
	}
	defer s.Close()
	if got, e := s.CreateCurrent("fixture-schema4-key", in, DefaultStamp{}); e != nil || got.ID != id {
		t.Fatal("retry lost schema4 task", e)
	}
}

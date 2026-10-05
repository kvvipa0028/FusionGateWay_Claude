package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

func TestPresetMigrationFourPreservesSchemaThreeReceiptsBudgetsAndHistory(t *testing.T) {
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
	for _, m := range []struct{ name, sql string }{{"migration_001_sha256", migration}, {"migration_002_sha256", migrationTwo}, {"migration_003_sha256", migrationThree}} {
		if _, e = db.Exec(m.sql); e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec("INSERT INTO metadata VALUES(?,?)", m.name, hash([]byte(m.sql))); e != nil {
			t.Fatal(e)
		}
	}
	in := CreateRequest{ProjectID: "fixture-project", Goal: "preserve schema3", Plan: plan(t, 1), Budget: &Budget{MaxCalls: 17, MaxReworks: 1}}
	raw, _ := json.Marshal(in)
	snapshot, _ := json.Marshal(in.Plan)
	target, _ := json.Marshal(in.Plan.Bindings[stageplan.Design].Target)
	statements := []struct {
		q    string
		args []any
	}{
		{"UPDATE controller_policy SET max_active=4", nil},
		{"INSERT INTO tasks VALUES('fixture-task','fixture-project','preserve schema3','ready',1,1,1)", nil},
		{"INSERT INTO plan_revisions VALUES('fixture-task',1,?,?)", []any{string(snapshot), in.Plan.Hash}},
		{"INSERT INTO task_budgets VALUES('fixture-task',17,1,3,0)", nil},
		{"INSERT INTO events(task_id,seq,kind,generation) VALUES('fixture-task',1,'created',0)", nil},
		{"INSERT INTO idempotency VALUES('fixture-project','fixture-key',?,'fixture-task')", []any{hash(raw)}},
		{"INSERT INTO stage_runs(id,task_id,role,attempt,generation,plan_revision,state,native_session_id,lease_owner,lease_until,startup_intent,launch_confirmed,target) VALUES('fixture-run','fixture-task','design',1,1,1,'succeeded','fixture-session','',0,1,1,?)", []any{string(target)}},
		{"INSERT INTO start_requests VALUES('fixture-start','fixture-task',?,'fixture-run')", []any{startHash(StartIdentity{TaskID: "fixture-task", Role: stageplan.Design, PlanRevision: 1, Generation: 0})}},
		{"INSERT INTO reservations VALUES('fixture-run','fixture-pool','','held','fixture-proof','')", nil},
	}
	for _, x := range statements {
		if _, e = db.Exec(x.q, x.args...); e != nil {
			t.Fatal(e)
		}
	}
	db.Close()
	s, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var version int
	s.db.QueryRow("PRAGMA user_version").Scan(&version)
	if version != 11 {
		t.Fatal("migration not installed", version)
	}
	replay, e := s.Create("fixture-key", in)
	if e != nil || replay.ID != "fixture-task" || replay.Generation != 1 {
		t.Fatal("schema3 payload receipt changed", e)
	}
	b, e := s.Budget(replay.ID)
	if e != nil || b.UsedCalls != 3 || b.MaxCalls != 17 {
		t.Fatal("budget changed", e)
	}
	r, e := s.LookupStart("fixture-start", StartIdentity{TaskID: replay.ID, Role: stageplan.Design, PlanRevision: 1, Generation: 0})
	if e != nil || r.ID != "fixture-run" || r.State != "succeeded" {
		t.Fatal("startup mapping changed", e)
	}
	var state string
	s.db.QueryRow("SELECT state FROM reservations WHERE run_id='fixture-run'").Scan(&state)
	if state != "held" {
		t.Fatal("migration released unproven capacity")
	}
	events, e := s.Events(replay.ID, 0)
	if e != nil || len(events) != 1 {
		t.Fatal("migration added/removed events", e)
	}
	ref, e := s.TaskPreset(replay.ID)
	if e != nil || ref != nil {
		t.Fatal("migration invented preset provenance", e)
	}
	if limit, e := s.Capacity(); e != nil || limit != 4 {
		t.Fatal("policy changed", e)
	}
	if _, e = s.SavePreset("fixture-project", "fixture-preset", 0, presetInput(t)); e != nil {
		t.Fatal("new tables unavailable", e)
	}
	if _, e = s.db.Exec("UPDATE metadata SET value='fixture-drift' WHERE key='migration_004_sha256'"); e != nil {
		t.Fatal(e)
	}
	s.Close()
	if opened, e := Open(root); !errors.Is(e, ErrUnsupported) {
		if opened != nil {
			opened.Close()
		}
		t.Fatal("migration checksum drift accepted", e)
	}
}

func TestPresetFutureSchemaIsRejectedWithoutDowngrade(t *testing.T) {
	s, root := openFixture(t)
	if _, e := s.db.Exec("PRAGMA user_version=12"); e != nil {
		t.Fatal(e)
	}
	s.Close()
	if opened, e := Open(root); !errors.Is(e, ErrUnsupported) {
		if opened != nil {
			opened.Close()
		}
		t.Fatal("future schema accepted", e)
	}
	db, e := sql.Open("sqlite", filepath.Join(root, "fusion.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var version int
	db.QueryRow("PRAGMA user_version").Scan(&version)
	if version != 12 {
		t.Fatal("future schema was rewritten", version)
	}
}

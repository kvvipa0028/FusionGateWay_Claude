package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/testsupport"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func plan(t *testing.T, rev int64) stageplan.Snapshot {
	t.Helper()
	def := "medium"
	route := stageplan.Route{ID: "fixture-route", Revision: 1, Model: "fixture-model-a", Account: "fixture-account-a", Workspace: "fixture-workspace", CredentialIdentity: "fixture-credential-a", RuntimeVersion: "fixture-runtime-v1", BillingPath: "fixture-subscription", BillingKnown: true, Admitted: true, Efforts: []string{"medium"}, DefaultEffort: &def, Capabilities: []string{"text"}, LockEnforcement: stageplan.ControlledCalls}
	binding := stageplan.Binding{Mode: stageplan.Locked, Route: &stageplan.RouteRef{ID: route.ID, Revision: 1}, Model: route.Model, Effort: &stageplan.EffortSelection{Mode: stageplan.EffortDefault}}
	s, e := stageplan.Compile(rev, []stageplan.Role{stageplan.Design}, stageplan.Layer{Roles: map[stageplan.Role]stageplan.Binding{stageplan.Design: binding}}, stageplan.Layer{}, stageplan.Layer{}, []stageplan.Route{route})
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func openFixture(t *testing.T) (*Store, string) {
	t.Helper()
	root := t.TempDir()
	os.Chmod(root, 0700)
	root, _ = filepath.EvalSymlinks(root)
	s, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s, root
}
func create(t *testing.T, s *Store) Task {
	t.Helper()
	task, e := s.Create("fixture-key", CreateRequest{ProjectID: "fixture-project", Goal: "fixture-goal", Plan: plan(t, 1)})
	if e != nil {
		t.Fatal(e)
	}
	return task
}
func start(t *testing.T, s *Store, task Task) StageRun {
	t.Helper()
	snap, e := s.Plan(task.ID, task.PlanRevision)
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.StartIntent(StartRequest{TaskID: task.ID, Role: stageplan.Design, PlanRevision: task.PlanRevision, Owner: "fixture-worker", TTL: time.Minute, Target: *snap.Bindings[stageplan.Design].Target})
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestTaskIdempotencyAndPayloadConflict(t *testing.T) {
	s, _ := openFixture(t)
	a := create(t, s)
	b := create(t, s)
	if a.ID != b.ID {
		t.Fatal("duplicate task")
	}
	_, e := s.Create("fixture-key", CreateRequest{ProjectID: "fixture-project", Goal: "different", Plan: plan(t, 1)})
	if !errors.Is(e, ErrConflict) {
		t.Fatalf("different payload accepted: %v", e)
	}
}
func TestSnapshotRevisionsAndIfMatch(t *testing.T) {
	s, _ := openFixture(t)
	task := create(t, s)
	if e := s.RevisePlan(task.ID, 1, plan(t, 2)); e != nil {
		t.Fatal(e)
	}
	if e := s.RevisePlan(task.ID, 1, plan(t, 2)); !errors.Is(e, ErrConflict) {
		t.Fatal("stale If-Match accepted")
	}
	a, _ := s.Plan(task.ID, 1)
	b, _ := s.Plan(task.ID, 2)
	if a.Revision != 1 || b.Revision != 2 || a.Hash == b.Hash {
		t.Fatal("history overwritten")
	}
}
func TestUniqueActiveAttemptAndOrderedEvents(t *testing.T) {
	s, _ := openFixture(t)
	task := create(t, s)
	r := start(t, s, task)
	snap, _ := s.Plan(task.ID, 1)
	_, e := s.StartIntent(StartRequest{TaskID: task.ID, Role: stageplan.Design, PlanRevision: 1, Owner: "fixture-worker-2", TTL: time.Minute, Target: *snap.Bindings[stageplan.Design].Target})
	if !errors.Is(e, ErrConflict) {
		t.Fatalf("duplicate run accepted: %v", e)
	}
	if e = s.ConfirmStarted(r.ID, r.Generation, r.Owner, "fixture-session"); e != nil {
		t.Fatal(e)
	}
	events, e := s.Events(task.ID, 0)
	if e != nil {
		t.Fatal(e)
	}
	if len(events) != 3 {
		t.Fatalf("events %v", events)
	}
	for i, v := range events {
		if v.Seq != int64(i+1) {
			t.Fatal("event gap")
		}
	}
	tail, e := s.Events(task.ID, 2)
	if e != nil || len(tail) != 1 || tail[0].Kind != "started" {
		t.Fatal("replay broken")
	}
}
func TestLeaseGenerationFencesLateEvents(t *testing.T) {
	s, _ := openFixture(t)
	task := create(t, s)
	r := start(t, s, task)
	if e := s.ConfirmStarted(r.ID, r.Generation+1, r.Owner, "fixture-session"); !errors.Is(e, ErrFenced) {
		t.Fatalf("stale generation accepted: %v", e)
	}
	if e := s.ConfirmStarted(r.ID, r.Generation, "wrong-worker", "fixture-session"); !errors.Is(e, ErrFenced) {
		t.Fatal("wrong owner accepted")
	}
	s.now = func() time.Time { return r.LeaseUntil.Add(time.Second) }
	if e := s.ConfirmStarted(r.ID, r.Generation, r.Owner, "fixture-session"); !errors.Is(e, ErrFenced) {
		t.Fatal("expired lease accepted")
	}
}
func TestRestartPreservesIntentAndBlocksBlindRerun(t *testing.T) {
	s, root := openFixture(t)
	task := create(t, s)
	r := start(t, s, task)
	if e := s.Close(); e != nil {
		t.Fatal(e)
	}
	fresh, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer fresh.Close()
	got, e := fresh.Run(r.ID)
	if e != nil || got.State != "unknown" || got.Generation <= r.Generation {
		t.Fatalf("intent lost or replayed: %+v %v", got, e)
	}
	if e = fresh.ConfirmStarted(r.ID, r.Generation, r.Owner, "fixture-old"); !errors.Is(e, ErrFenced) {
		t.Fatal("old execution reattached")
	}
	snap, _ := fresh.Plan(task.ID, 1)
	_, e = fresh.StartIntent(StartRequest{TaskID: task.ID, Role: stageplan.Design, PlanRevision: 1, Owner: "fixture-new", TTL: time.Minute, Target: *snap.Bindings[stageplan.Design].Target})
	if !errors.Is(e, ErrConflict) {
		t.Fatal("unknown write automatically rerun")
	}
}
func TestCancellationCompletionRaceIsSingleTerminal(t *testing.T) {
	s, _ := openFixture(t)
	task := create(t, s)
	r := start(t, s, task)
	if e := s.ConfirmStarted(r.ID, r.Generation, r.Owner, "fixture-session"); e != nil {
		t.Fatal(e)
	}
	if e := s.CancelIntent(r.ID, r.Generation, r.Owner); e != nil {
		t.Fatal(e)
	}
	if e := s.Finish(r.ID, r.Generation, r.Owner, "cancelled"); e != nil {
		t.Fatal(e)
	}
	if e := s.Finish(r.ID, r.Generation, r.Owner, "succeeded"); !errors.Is(e, ErrFenced) {
		t.Fatal("late success overwrote cancellation")
	}
	got, _ := s.Run(r.ID)
	if got.State != "cancelled" {
		t.Fatal("terminal changed")
	}
}
func TestControllerAndPathIsolation(t *testing.T) {
	s, root := openFixture(t)
	if other, e := Open(root); !errors.Is(e, ErrControllerActive) {
		if other != nil {
			other.Close()
		}
		t.Fatalf("second controller accepted: %v", e)
	}
	s.Close()
	parent := t.TempDir()
	os.Chmod(parent, 0700)
	if e := os.Symlink(filepath.Join(root, "fusion.db"), filepath.Join(parent, "fusion.db")); e != nil {
		t.Fatal(e)
	}
	if other, e := Open(parent); e == nil {
		other.Close()
		t.Fatal("linked database accepted")
	}
	info, e := os.Stat(filepath.Join(root, "fusion.db"))
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("database not private")
	}
}
func TestForgedExecutionTargetAndSnapshotRefused(t *testing.T) {
	s, _ := openFixture(t)
	snap := plan(t, 1)
	snap.Bindings[stageplan.Design].Target.Account = "fixture-forged"
	if _, e := s.Create("fixture-key", CreateRequest{ProjectID: "fixture-project", Goal: "fixture-goal", Plan: snap}); e == nil {
		t.Fatal("bad snapshot stored")
	}
	task := create(t, s)
	target := *plan(t, 1).Bindings[stageplan.Design].Target
	target.Account = "fixture-forged"
	if _, e := s.StartIntent(StartRequest{TaskID: task.ID, Role: stageplan.Design, PlanRevision: 1, Owner: "fixture-worker", TTL: time.Minute, Target: target}); e == nil {
		t.Fatal("forged target ran")
	}
}
func TestImmutableRoutesAndEvidenceRefs(t *testing.T) {
	s, _ := openFixture(t)
	task := create(t, s)
	r := start(t, s, task)
	route := stageplan.Route{ID: "fixture-route", Revision: 1, Model: "fixture-model-a"}
	if e := s.SaveRoute(route); e != nil {
		t.Fatal(e)
	}
	if e := s.SaveRoute(route); e != nil {
		t.Fatal(e)
	}
	route.Model = "changed"
	if e := s.SaveRoute(route); !errors.Is(e, ErrConflict) {
		t.Fatal("route history overwritten")
	}
	if e := s.AddEvidence(EvidenceRef{ID: "fixture-evidence", TaskID: task.ID, RunID: r.ID, ArtifactHash: plan(t, 1).Hash, ReportHash: plan(t, 1).Hash}); e != nil {
		t.Fatal(e)
	}
	if e := s.AddEvidence(EvidenceRef{ID: "fixture-evidence", TaskID: task.ID, RunID: r.ID, ArtifactHash: "different", ReportHash: plan(t, 1).Hash}); e == nil {
		t.Fatal("evidence mutated")
	}
}

func TestConcurrentDuplicateSubmissionCreatesOneTask(t *testing.T) {
	s, _ := openFixture(t)
	in := CreateRequest{ProjectID: "fixture-project", Goal: "fixture-goal", Plan: plan(t, 1)}
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); v, e := s.Create("fixture-same-key", in); ids <- v.ID; errs <- e }()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatal("concurrent duplicate created extra task")
		}
	}
}
func TestStartupIntentWriteFailureRollsBackAllState(t *testing.T) {
	s, _ := openFixture(t)
	task := create(t, s)
	if _, e := s.db.Exec("CREATE TRIGGER fixture_fail_events BEFORE INSERT ON events BEGIN SELECT RAISE(ABORT,'fixture injected event failure'); END"); e != nil {
		t.Fatal(e)
	}
	snap, _ := s.Plan(task.ID, 1)
	_, e := s.StartIntent(StartRequest{TaskID: task.ID, Role: stageplan.Design, PlanRevision: 1, Owner: "fixture-worker", TTL: time.Minute, Target: *snap.Bindings[stageplan.Design].Target})
	if e == nil {
		t.Fatal("failed intent reported success")
	}
	got, _ := s.Task(task.ID)
	if got.State != "ready" || got.Generation != 0 {
		t.Fatal("partial task mutation persisted")
	}
	var count int
	if e = s.db.QueryRow("SELECT COUNT(*) FROM stage_runs").Scan(&count); e != nil || count != 0 {
		t.Fatal("partial intent persisted")
	}
	if _, e = s.db.Exec("DROP TRIGGER fixture_fail_events"); e != nil {
		t.Fatal(e)
	}
	r := start(t, s, task)
	if r.Attempt != 1 || r.Generation != 1 {
		t.Fatal("rolled back intent consumed attempt/generation")
	}
}
func TestStoreCrashHelper(t *testing.T) {
	if os.Getenv("FUSION_STORE_CRASH_HELPER") != "1" {
		t.Skip("isolated helper child only")
	}
	root := os.Getenv("FUSION_STORE_FIXTURE_ROOT")
	s, e := Open(root)
	if e != nil {
		os.Exit(71)
	}
	task := create(t, s)
	r := start(t, s, task)
	if e = s.ConfirmStarted(r.ID, r.Generation, r.Owner, "fixture-native-session"); e != nil {
		os.Exit(72)
	}
	if e = os.WriteFile(filepath.Join(root, "fixture-effect.txt"), []byte("fixture-effect-before-crash\n"), 0600); e != nil {
		os.Exit(73)
	}
	json.NewEncoder(os.Stdout).Encode(map[string]string{"task_id": task.ID, "run_id": r.ID})
	for {
		time.Sleep(time.Second)
	}
}
func TestNativeControllerCrashIsUnknownWithoutEffectReplay(t *testing.T) {
	root := t.TempDir()
	os.Chmod(root, 0700)
	root, _ = filepath.EvalSymlinks(root)
	env, e := testsupport.RuntimeEnvironment(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	env = append(env, "FUSION_STORE_CRASH_HELPER=1", "FUSION_STORE_FIXTURE_ROOT="+root)
	executable, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, executable, "-test.run=^TestStoreCrashHelper$")
	child.Env = env
	stdout, e := child.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = child.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { child.Process.Kill(); child.Wait() }()
	var ids map[string]string
	if e = json.NewDecoder(stdout).Decode(&ids); e != nil {
		t.Fatal(e)
	}
	if e = child.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	if e = child.Wait(); e == nil {
		t.Fatal("crash did not terminate controller")
	}
	s, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	r, e := s.Run(ids["run_id"])
	if e != nil || r.State != "unknown" || r.NativeSessionID != "fixture-native-session" || !r.StartupIntent || !r.LaunchConfirmed || r.Owner != "" {
		t.Fatalf("crash state: %+v %v", r, e)
	}
	task, e := s.Task(ids["task_id"])
	if e != nil || task.State != "needs_review" {
		t.Fatal("crashed task not paused")
	}
	effect, e := os.ReadFile(filepath.Join(root, "fixture-effect.txt"))
	if e != nil || string(effect) != "fixture-effect-before-crash\n" {
		t.Fatal("side effect missing or rewritten")
	}
	var count int
	if e = s.db.QueryRow("SELECT COUNT(*) FROM stage_runs").Scan(&count); e != nil || count != 1 {
		t.Fatal("side effect blindly replayed")
	}
}

func TestExpiredLeaseIsDurablyFencedAcrossClockRollback(t *testing.T) {
	s, _ := openFixture(t)
	task := create(t, s)
	r := start(t, s, task)
	if e := s.ConfirmStarted(r.ID, r.Generation, r.Owner, "fixture-session"); e != nil {
		t.Fatal(e)
	}
	s.now = func() time.Time { return r.LeaseUntil }
	if e := s.RenewLease(r.ID, r.Generation, r.Owner, time.Minute); !errors.Is(e, ErrFenced) {
		t.Fatal("expired lease resurrected")
	}
	got, e := s.Run(r.ID)
	if e != nil || got.State != "unknown" || got.Generation <= r.Generation {
		t.Fatal("expiry not durably fenced")
	}
	s.now = time.Now
	if e := s.Finish(r.ID, r.Generation, r.Owner, "succeeded"); !errors.Is(e, ErrFenced) {
		t.Fatal("clock rollback restored expired worker")
	}
}
func TestSuccessfulStageIsNotTaskAcceptance(t *testing.T) {
	s, _ := openFixture(t)
	task := create(t, s)
	r := start(t, s, task)
	if e := s.ConfirmStarted(r.ID, r.Generation, r.Owner, "fixture-session"); e != nil {
		t.Fatal(e)
	}
	if e := s.Finish(r.ID, r.Generation, r.Owner, "succeeded"); e != nil {
		t.Fatal(e)
	}
	got, _ := s.Task(task.ID)
	if got.State != "ready" {
		t.Fatal("stage success promoted to task acceptance")
	}
	var seq int64
	err := s.db.QueryRow("SELECT seq FROM events WHERE task_id=? ORDER BY seq DESC LIMIT 1", task.ID).Scan(&seq)
	if err != nil && err != sql.ErrNoRows {
		t.Fatal(err)
	}
	if seq != 4 {
		t.Fatal("unexpected event gap")
	}
}
func TestPrivateSidecarsAndUnrelatedDatabaseAreRefused(t *testing.T) {
	root := t.TempDir()
	os.Chmod(root, 0700)
	root, _ = filepath.EvalSymlinks(root)
	if e := os.WriteFile(filepath.Join(root, "fusion.db-wal"), []byte("fixture-not-a-wal"), 0644); e != nil {
		t.Fatal(e)
	}
	if s, e := Open(root); e == nil {
		s.Close()
		t.Fatal("public sidecar accepted")
	}
	if e := os.Remove(filepath.Join(root, "fusion.db-wal")); e != nil {
		t.Fatal(e)
	}
	db, e := sql.Open("sqlite", filepath.Join(root, "fusion.db"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("CREATE TABLE fixture_original_accounts(id TEXT)"); e != nil {
		t.Fatal(e)
	}
	db.Close()
	if s, e := Open(root); !errors.Is(e, ErrUnsupported) {
		if s != nil {
			s.Close()
		}
		t.Fatalf("unrelated database migrated: %v", e)
	}
	db, e = sql.Open("sqlite", filepath.Join(root, "fusion.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var count int
	if e = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name='tasks'").Scan(&count); e != nil || count != 0 {
		t.Fatal("unrelated database rewritten")
	}
}

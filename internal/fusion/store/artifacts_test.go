package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/yetone/magpie/internal/fusion/handoff"
	"github.com/yetone/magpie/internal/fusion/workspace"
)

func artifactRecordFixture(t *testing.T, s *Store) (ArtifactRecord, StageRun, string, string) {
	t.Helper()
	task, in := scheduledTask(t, s, "artifact-fixture")
	run, err := s.StartReserved(in, ReservationRequest{PoolKey: "artifact-pool", GlobalLimit: 2, AdmissionHash: hash([]byte("admission"))})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmStarted(run.ID, run.Generation, run.Owner, "fixture-artifact-session"); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(run.ID, run.Generation, run.Owner, "succeeded"); err != nil {
		t.Fatal(err)
	}
	root, source := t.TempDir(), t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	source, _ = filepath.EvalSymlinks(source)
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "code"), []byte("base"), 0600); err != nil {
		t.Fatal(err)
	}
	copy, err := workspace.Copy(source, root, "producer")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(copy.Path, "code"), []byte("actual stage output"), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := workspace.Freeze(copy, root, "artifact")
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Plan(task.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := json.Marshal(run.Target)
	b := handoff.Binding{TaskID: task.ID, PlanRevision: 1, PlanHash: p.Hash, RunID: run.ID, Generation: run.Generation, Role: run.Role, TargetHash: hash(target)}
	h, err := handoff.Publish(a, b, task.Goal, handoff.Evidence{OutputHash: hash([]byte("synthetic output")), StopProofHash: hash([]byte("fixture-verified-stop"))})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := h.Reference(root)
	if err != nil {
		t.Fatal(err)
	}
	return ArtifactRecord{Reference: ref, InputTreeHash: ref.BaseTreeHash}, run, source, root
}

func TestArtifactIndexRequiresReleasedProofAndSurvivesRestart(t *testing.T) {
	s, dbroot := openFixture(t)
	record, run, source, root := artifactRecordFixture(t, s)
	if err := s.RecordArtifact(record); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Artifact(run.ID); !errors.Is(err, ErrArtifactPending) {
		t.Fatal("indexed held run became consumable", err)
	}
	if err := s.ReleaseReserved(run.ID, run.Generation, record.Reference.StopProofHash, true); err != nil {
		t.Fatal(err)
	}
	want, err := s.Artifact(run.ID)
	if err != nil || !reflect.DeepEqual(want, record) {
		t.Fatal("released receipt", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	opened, err := Open(dbroot)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	got, err := opened.Artifact(run.ID)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("restart lost artifact receipt", err)
	}
	bundle, err := handoff.Restore(got.Reference, source, root)
	if err != nil {
		t.Fatal("durable authority", err)
	}
	next, err := bundle.Copy(got.Reference.Binding, root, "next-stage")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(next.Path, "code"))
	if err != nil || string(b) != "actual stage output" {
		t.Fatal("restart discarded code", err)
	}
}

func TestArtifactIndexIsIdempotentConcurrentAndImmutable(t *testing.T) {
	s, _ := openFixture(t)
	record, run, _, _ := artifactRecordFixture(t, s)
	before, err := s.Events(run.TaskID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.RecordArtifact(record); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	after, err := s.Events(run.TaskID, 0)
	if err != nil || len(after) != len(before)+1 {
		t.Fatal("duplicate registration events", err)
	}
	bad := record
	bad.InputTreeHash = strings.Repeat("f", 64)
	if err := s.RecordArtifact(bad); !errors.Is(err, ErrConflict) {
		t.Fatal("receipt replaced", err)
	}
	for _, q := range []string{"UPDATE stage_artifacts SET record_hash='changed'", "DELETE FROM stage_artifacts"} {
		if _, err := s.db.Exec(q); err == nil {
			t.Fatal("artifact history mutable")
		}
	}
}

func TestArtifactIndexRejectsFalseBindingsAndCurrentRevocation(t *testing.T) {
	for _, mode := range []string{"task", "run", "plan", "generation", "role", "target", "input", "parent", "proof", "witness", "current"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := openFixture(t)
			r, run, _, _ := artifactRecordFixture(t, s)
			current := true
			switch mode {
			case "task":
				r.Reference.Binding.TaskID = "other"
			case "run":
				r.Reference.Binding.RunID = "missing"
			case "plan":
				r.Reference.Binding.PlanHash = strings.Repeat("f", 64)
			case "generation":
				r.Reference.Binding.Generation++
			case "role":
				r.Reference.Binding.Role = "implementation"
			case "target":
				r.Reference.Binding.TargetHash = strings.Repeat("f", 64)
			case "input":
				r.InputTreeHash = strings.Repeat("f", 64)
			case "parent":
				r.ParentRunID = "foreign-parent"
			case "proof":
				if err := s.ReleaseReserved(run.ID, run.Generation, hash([]byte("different verified stop")), true); err != nil {
					t.Fatal(err)
				}
			case "witness":
				r.Reference.WitnessHash = "forged"
			case "current":
				current = false
			}
			if err := s.RecordArtifactAuthorized(r, func() bool { return current }); err == nil {
				t.Fatal("false artifact binding accepted")
			}
			var count int
			if err := s.db.QueryRow("SELECT COUNT(*) FROM stage_artifacts").Scan(&count); err != nil || count != 0 {
				t.Fatal("rejection created partial artifact", err)
			}
		})
	}
}

func TestArtifactIndexEventFailureAndRevocationRollBack(t *testing.T) {
	s, _ := openFixture(t)
	r, run, _, _ := artifactRecordFixture(t, s)
	if _, err := s.db.Exec("CREATE TRIGGER artifact_event_fail BEFORE INSERT ON events WHEN NEW.kind='artifact_recorded' BEGIN SELECT RAISE(ABORT,'fixture'); END"); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordArtifact(r); err == nil {
		t.Fatal("failed event acknowledged")
	}
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM stage_artifacts").Scan(&count)
	if count != 0 {
		t.Fatal("partial artifact survived")
	}
	if _, err := s.db.Exec("DROP TRIGGER artifact_event_fail"); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	if err := s.RecordArtifactAuthorized(r, func() bool { return calls.Add(1) < 3 }); err == nil {
		t.Fatal("late revocation committed receipt")
	}
	s.db.QueryRow("SELECT COUNT(*) FROM stage_artifacts").Scan(&count)
	if count != 0 {
		t.Fatal("revoked artifact survived")
	}
	if _, err := s.Reservation(run.ID); err != nil {
		t.Fatal("failed publication released reservation", err)
	}
	if err := s.RecordArtifact(r); err != nil {
		t.Fatal("retry", err)
	}
}

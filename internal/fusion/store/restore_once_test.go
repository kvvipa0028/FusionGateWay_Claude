package store

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

func restoredOnceFixture(t *testing.T, s *Store) (StartRequest, ReservationRequest) {
	t.Helper()
	in, req := onceFixture(t, s)
	old, err := s.StartReservedOnce(in, req)
	if err != nil {
		t.Fatal(err)
	}
	r := old.Run
	if err = s.ConfirmStarted(r.ID, r.Generation, r.Owner, "fixture-original-session"); err != nil {
		t.Fatal(err)
	}
	if err = s.Finish(r.ID, r.Generation, r.Owner, "succeeded"); err != nil {
		t.Fatal(err)
	}
	if err = s.ReleaseReserved(r.ID, r.Generation, strings.Repeat("a", 64), true); err != nil {
		t.Fatal(err)
	}
	task, err := s.Task(in.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	in.IdempotencyKey = "fixture-resume-key"
	in.ExpectedGeneration = &task.Generation
	in.Restore = &RestoreIdentity{OriginRunID: r.ID, CheckpointID: strings.Repeat("b", 64), CheckpointDigest: strings.Repeat("c", 64)}
	return in, req
}

func TestRestoreIdentityKeepsLegacyStartHash(t *testing.T) {
	s, _ := openFixture(t)
	in, _ := onceFixture(t, s)
	raw, _ := json.Marshal(startIdentity(in))
	want := `{"task_id":"` + in.TaskID + `","role":"design","plan_revision":1,"generation":0}`
	if string(raw) != want || startHash(startIdentity(in)) != hash([]byte(want)) {
		t.Fatal("legacy request identity changed")
	}
}

func TestRestoreOncePayloadAndScopeAreAtomic(t *testing.T) {
	for _, mode := range []string{"missing", "uppercase", "digest", "origin", "foreign_role", "foreign_target", "held", "not_success"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := openFixture(t)
			in, req := restoredOnceFixture(t, s)
			switch mode {
			case "missing":
				in.Restore.CheckpointID = ""
			case "uppercase":
				in.Restore.CheckpointID = strings.Repeat("B", 64)
			case "digest":
				in.Restore.CheckpointDigest = "bad"
			case "origin":
				in.Restore.OriginRunID = "fixture-not-found"
			case "foreign_role":
				s.db.Exec("UPDATE stage_runs SET role='testing' WHERE id=?", in.Restore.OriginRunID)
			case "foreign_target":
				s.db.Exec("UPDATE stage_runs SET target='{}' WHERE id=?", in.Restore.OriginRunID)
			case "held":
				s.db.Exec("UPDATE reservations SET state='held' WHERE run_id=?", in.Restore.OriginRunID)
			case "not_success":
				s.db.Exec("UPDATE stage_runs SET state='failed' WHERE id=?", in.Restore.OriginRunID)
			}
			before, _ := s.Task(in.TaskID)
			events, _ := s.Events(in.TaskID, 0)
			got, err := s.StartReservedOnce(in, req)
			if err == nil || got.Created {
				t.Fatal("invalid restore created intent")
			}
			after, _ := s.Task(in.TaskID)
			afterEvents, _ := s.Events(in.TaskID, 0)
			if before != after || len(events) != len(afterEvents) {
				t.Fatal("restore refusal changed task")
			}
		})
	}
}

func TestRestoreOnceRetryCannotBecomeStartOrAnotherCheckpoint(t *testing.T) {
	s, _ := openFixture(t)
	in, req := restoredOnceFixture(t, s)
	var wg sync.WaitGroup
	results := make(chan StartReceipt, 10)
	for n := 0; n < 10; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := s.StartReservedOnce(in, req)
			if e != nil {
				t.Error(e)
				return
			}
			results <- r
		}()
	}
	wg.Wait()
	close(results)
	var winner StageRun
	count := 0
	for r := range results {
		if r.Created {
			count++
			winner = r.Run
		}
	}
	if count != 1 || winner.Generation != 2 || winner.Attempt != 2 {
		t.Fatal("restore launched multiple intents")
	}
	original := startIdentity(in)
	got, e := s.LookupStart(in.IdempotencyKey, original)
	if e != nil || got.ID != winner.ID {
		t.Fatal("restore receipt unavailable")
	}
	for _, mode := range []string{"start", "origin", "id", "digest"} {
		bad := original
		cp := *original.Restore
		bad.Restore = &cp
		switch mode {
		case "start":
			bad.Restore = nil
		case "origin":
			cp.OriginRunID = "fixture-other"
		case "id":
			cp.CheckpointID = strings.Repeat("d", 64)
		case "digest":
			cp.CheckpointDigest = strings.Repeat("e", 64)
		}
		if _, e = s.LookupStart(in.IdempotencyKey, bad); !errors.Is(e, ErrConflict) {
			t.Fatal("same key changed restore identity")
		}
	}
}

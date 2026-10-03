package policy

import (
	"context"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
	"strings"
	"testing"
)

func TestPrepareOnceRestoreIdentityPersistsAndFreezesThroughInspection(t *testing.T) {
	s, in, _, root := onceScheduleFixture(t)
	// Produce a stopped original run without consuming the task's call budget.
	old, e := s.PrepareOnce(context.Background(), in)
	if e != nil {
		t.Fatal(e)
	}
	r := old.Run
	if e = s.Store.ConfirmStarted(r.ID, r.Generation, r.Owner, "fixture-old-session"); e != nil {
		t.Fatal(e)
	}
	if e = s.Store.Finish(r.ID, r.Generation, r.Owner, "succeeded"); e != nil {
		t.Fatal(e)
	}
	if e = s.Store.ReleaseReserved(r.ID, r.Generation, strings.Repeat("a", 64), true); e != nil {
		t.Fatal(e)
	}
	task, _ := s.Store.Task(in.TaskID)
	in.ExpectedGeneration = &task.Generation
	in.IdempotencyKey = "fixture-resume-once"
	reference := &store.RestoreIdentity{OriginRunID: r.ID, CheckpointID: strings.Repeat("b", 64), CheckpointDigest: strings.Repeat("c", 64)}
	in.Restore = reference
	original := s.Inspect
	s.Inspect = func(ctx context.Context, task store.Task, role stageplan.Role, target stageplan.ExecutionTarget) (Inspection, error) {
		reference.CheckpointDigest = strings.Repeat("e", 64)
		return original(ctx, task, role, target)
	}
	first, e := s.PrepareOnce(context.Background(), in)
	if e != nil || !first.Created {
		t.Fatal(e)
	}
	in.Restore = &store.RestoreIdentity{OriginRunID: r.ID, CheckpointID: strings.Repeat("b", 64), CheckpointDigest: strings.Repeat("c", 64)}
	if e = s.Store.Close(); e != nil {
		t.Fatal(e)
	}
	reopened, e := store.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	s.Store = reopened
	s.Inspect = nil
	retry, e := s.PrepareOnce(context.Background(), in)
	if e != nil || retry.Created || retry.Run.ID != first.Run.ID || retry.Run.State != "unknown" {
		t.Fatal("restore mapping lost or replayed", e)
	}
	if _, e = reopened.Reservation(first.Run.ID); e != nil {
		t.Fatal("unknown restore capacity freed")
	}
	in.Restore.CheckpointDigest = strings.Repeat("e", 64)
	_, e = s.PrepareOnce(context.Background(), in)
	requireBlock(t, e, "start_request_conflict")
	in.Restore = nil
	_, e = s.PrepareOnce(context.Background(), in)
	requireBlock(t, e, "start_request_conflict")
}

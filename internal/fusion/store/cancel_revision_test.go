package store

import (
	"errors"
	"testing"
)

func TestCancelIntentChecksCurrentTaskRevisionBeforeMutation(t *testing.T) {
	s, _ := openFixture(t)
	task := create(t, s)
	r := start(t, s, task)
	if e := s.ConfirmStarted(r.ID, r.Generation, r.Owner, "fixture-session"); e != nil {
		t.Fatal(e)
	}
	// A valid revision preserves the active binding and run generation.
	if e := s.RevisePlan(task.ID, 1, plan(t, 2)); e != nil {
		t.Fatal(e)
	}
	before, _ := s.Events(task.ID, 0)
	for _, rev := range []int64{1, 3, 0, -1} {
		want := ErrConflict
		if rev < 1 {
			want = ErrInvalid
		}
		if e := s.CancelIntentAtRevision(r.ID, r.Generation, r.Owner, rev); !errors.Is(e, want) {
			t.Fatalf("revision %d: %v", rev, e)
		}
	}
	current, _ := s.Run(r.ID)
	after, _ := s.Events(task.ID, 0)
	if current.State != "running" || len(after) != len(before) {
		t.Fatal("rejected cancel mutated the run or event stream")
	}
	if e := s.CancelIntentAtRevision(r.ID, r.Generation, r.Owner, 2); e != nil {
		t.Fatal(e)
	}
	if e := s.CancelIntentAtRevision(r.ID, r.Generation, r.Owner, 2); e != nil {
		t.Fatal("same revision cancel not idempotent", e)
	}
	if e := s.RevisePlan(task.ID, 2, plan(t, 3)); e != nil {
		t.Fatal(e)
	}
	if e := s.CancelIntentAtRevision(r.ID, r.Generation, r.Owner, 2); !errors.Is(e, ErrConflict) {
		t.Fatal("already cancelling bypassed current revision", e)
	}
	after, _ = s.Events(task.ID, 0)
	if len(after) != len(before)+2 || after[len(before)].Kind != "cancel_intent" {
		t.Fatal("cancel replay added an event")
	}
}

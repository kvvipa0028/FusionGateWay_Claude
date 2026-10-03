package store

import (
	"errors"
	"testing"
)

func TestEventsPageIsBoundedOrderedAndTaskScoped(t *testing.T) {
	s, _ := openFixture(t)
	a := create(t, s)
	b, e := s.Create("fixture-other-key", CreateRequest{ProjectID: "fixture-project", Goal: "fixture-other", Plan: plan(t, 1)})
	if e != nil {
		t.Fatal(e)
	}
	for rev := int64(1); rev < 5; rev++ {
		if e = s.RevisePlan(a.ID, rev, plan(t, rev+1)); e != nil {
			t.Fatal(e)
		}
	}
	page, e := s.EventsPage(a.ID, 0, 2)
	if e != nil || len(page) != 2 || page[0].Seq != 1 || page[1].Seq != 2 {
		t.Fatal(page, e)
	}
	next, e := s.EventsPage(a.ID, page[1].Seq, 2)
	if e != nil || len(next) != 2 || next[0].Seq != 3 || next[1].Seq != 4 {
		t.Fatal(next, e)
	}
	tail, e := s.EventsPage(a.ID, 4, 2)
	if e != nil || len(tail) != 1 || tail[0].Seq != 5 {
		t.Fatal(tail, e)
	}
	other, e := s.EventsPage(b.ID, 0, 2)
	if e != nil || len(other) != 1 || other[0].TaskID != b.ID {
		t.Fatal("events crossed task", other, e)
	}
	for _, in := range []struct {
		after int64
		limit int
	}{{-1, 1}, {0, 0}, {0, 257}} {
		if _, e = s.EventsPage(a.ID, in.after, in.limit); !errors.Is(e, ErrInvalid) {
			t.Fatal("invalid page accepted", e)
		}
	}
	if _, e = s.EventsPage(a.ID, 6, 2); !errors.Is(e, ErrConflict) {
		t.Fatal("cursor beyond persisted tail accepted", e)
	}
	if _, e = s.EventsPage("fixture-missing", 0, 2); !errors.Is(e, ErrNotFound) {
		t.Fatal("missing task hidden as empty events", e)
	}
}

func TestEventsPageRejectsMissingDurableTail(t *testing.T) {
	s, _ := openFixture(t)
	task := create(t, s)
	if e := s.RevisePlan(task.ID, 1, plan(t, 2)); e != nil {
		t.Fatal(e)
	}
	if _, e := s.db.Exec("DELETE FROM events WHERE task_id=? AND seq=2", task.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := s.EventsPage(task.ID, 0, 256); !errors.Is(e, ErrInvalid) {
		t.Fatal("missing durable event accepted as full page", e)
	}
}

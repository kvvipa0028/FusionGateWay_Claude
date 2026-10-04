package store

import (
	"errors"
	"reflect"
	"testing"
)

func TestSubmissionJournalLookupExactOriginalAndTerminalHistory(t *testing.T) {
	s, root := openFixture(t)
	d := journalDraft(t, "lookup-original")
	first, e := s.PrepareSubmission(d, DefaultStamp{})
	if e != nil {
		t.Fatal(e)
	}
	identity := journalIdentity(d)
	got, e := s.LookupSubmissionJournal(identity)
	if e != nil || !reflect.DeepEqual(got, first.Submission) {
		t.Fatal("original lookup", e)
	}
	got.Draft.Request.Plan.RequiredRoles[0] = "mutated"
	got, e = s.LookupSubmissionJournal(identity)
	if e != nil || !reflect.DeepEqual(got, first.Submission) {
		t.Fatal("lookup returned mutable owner data", e)
	}
	for _, field := range []string{"project", "key", "hash"} {
		wrong := identity
		switch field {
		case "project":
			wrong.ProjectID = "other"
		case "key":
			wrong.Key = "other"
		case "hash":
			wrong.PlanHash = "other"
		}
		if _, e = s.LookupSubmissionJournal(wrong); !errors.Is(e, ErrConflict) {
			t.Fatal("identity accepted", field, e)
		}
	}
	wrong := identity
	wrong.PreviewID = "missing"
	if _, e = s.LookupSubmissionJournal(wrong); !errors.Is(e, ErrNotFound) {
		t.Fatal("missing guessed", e)
	}
	wrong = identity
	wrong.Key = ""
	if _, e = s.LookupSubmissionJournal(wrong); !errors.Is(e, ErrInvalid) {
		t.Fatal("invalid lookup", e)
	}
	sealed, e := s.ResolveSubmission(identity, "abandon")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	if _, e = s.LookupSubmissionJournal(identity); !errors.Is(e, ErrClosed) {
		t.Fatal("closed lookup", e)
	}
	s, e = Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	got, e = s.LookupSubmissionJournal(identity)
	if e != nil || !reflect.DeepEqual(got, sealed) {
		t.Fatal("terminal history lost", e)
	}
}

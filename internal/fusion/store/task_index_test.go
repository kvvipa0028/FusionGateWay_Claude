package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestProjectTasksPagesRemainProjectBoundAndDurable(t *testing.T) {
	s, root := openFixture(t)
	snapshot := plan(t, 1)
	var ids []string
	for i := 0; i < 67; i++ {
		for _, project := range []string{"fixture-project", "other-project"} {
			task, err := s.Create(fmt.Sprint(i), CreateRequest{ProjectID: project, Goal: fmt.Sprint(i), Plan: snapshot})
			if err != nil {
				t.Fatal(err)
			}
			if project == "fixture-project" {
				ids = append(ids, task.ID)
			}
		}
	}
	page, err := s.ProjectTasks(context.Background(), "fixture-project", "")
	if err != nil || len(page.Tasks) != 32 || page.NextBefore != ids[35] {
		t.Fatal("first bounded page", err)
	}
	// A newly inserted task must not shift or duplicate older keyset pages.
	if _, err := s.Create("new", CreateRequest{ProjectID: "fixture-project", Goal: "new", Plan: snapshot}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var seen []string
	for {
		for _, task := range page.Tasks {
			if task.ProjectID != "fixture-project" {
				t.Fatal("cross-project row")
			}
			seen = append(seen, task.ID)
		}
		if page.NextBefore == "" {
			break
		}
		page, err = s.ProjectTasks(context.Background(), "fixture-project", page.NextBefore)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 67 {
		t.Fatal("lost or repeated tasks", len(seen))
	}
	for i, id := range seen {
		if id != ids[66-i] {
			t.Fatal("not creation order", i)
		}
	}
	page, err = s.ProjectTasks(context.Background(), "fixture-project", ids[0])
	if err != nil || page.Tasks == nil || len(page.Tasks) != 0 || page.NextBefore != "" {
		t.Fatal("last empty page", err)
	}
}

func TestProjectTasksRejectsForeignCursorAndStorageFailure(t *testing.T) {
	s, _ := openFixture(t)
	task := create(t, s)
	for _, tc := range []struct {
		project, before string
		want            error
	}{
		{"other-project", task.ID, ErrNotFound}, {"fixture-project", "missing", ErrNotFound},
		{"", "", ErrInvalid}, {"fixture-project", "bad cursor", ErrInvalid},
	} {
		out, err := s.ProjectTasks(context.Background(), tc.project, tc.before)
		if !errors.Is(err, tc.want) || len(out.Tasks) != 0 || out.NextBefore != "" {
			t.Fatal("unsafe cursor", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if out, err := s.ProjectTasks(ctx, "fixture-project", ""); !errors.Is(err, context.Canceled) || len(out.Tasks) != 0 {
		t.Fatal("cancelled read", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if out, err := s.ProjectTasks(context.Background(), "fixture-project", ""); !errors.Is(err, ErrClosed) || len(out.Tasks) != 0 {
		t.Fatal("closed read", err)
	}
}

func TestProjectTasksExactPageBoundaryAndCurrentVersion(t *testing.T) {
	s, _ := openFixture(t)
	snapshot := plan(t, 1)
	var last Task
	for i := 0; i < 32; i++ {
		var err error
		last, err = s.Create(fmt.Sprint(i), CreateRequest{ProjectID: "fixture-project", Goal: "fixture", Plan: snapshot})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RevisePlan(last.ID, last.PlanRevision, plan(t, 2)); err != nil {
		t.Fatal(err)
	}
	page, err := s.ProjectTasks(context.Background(), "fixture-project", "")
	if err != nil || len(page.Tasks) != 32 || page.NextBefore != "" || page.Tasks[0].ID != last.ID || page.Tasks[0].PlanRevision != 2 {
		t.Fatal("exact boundary/current revision", err)
	}
	current, err := s.Task(last.ID)
	if err != nil || page.Tasks[0] != current {
		t.Fatal("stale summary", err)
	}
}

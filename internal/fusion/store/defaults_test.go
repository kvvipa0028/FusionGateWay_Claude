package store

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

func TestCreationLookupIsExactScopedReadOnly(t *testing.T) {
	s, _ := openFixture(t)
	in := CreateRequest{ProjectID: "fixture-project", Goal: "fixture goal", Plan: plan(t, 1)}
	if _, e := s.LookupCreation("fixture-lookup", in); !errors.Is(e, ErrNotFound) {
		t.Fatal("lookup created missing receipt", e)
	}
	task, e := s.Create("fixture-lookup", in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveDefaultLayer("global", "global", 0, stageplan.Layer{}); e != nil {
		t.Fatal(e)
	}
	got, e := s.LookupCreation("fixture-lookup", in)
	if e != nil || got.ID != task.ID {
		t.Fatal("existing receipt not readable", e)
	}
	in.Goal = "changed goal"
	if _, e = s.LookupCreation("fixture-lookup", in); !errors.Is(e, ErrConflict) {
		t.Fatal("changed payload accepted", e)
	}
	in.ProjectID = "other-project"
	if _, e = s.LookupCreation("fixture-lookup", in); !errors.Is(e, ErrNotFound) {
		t.Fatal("cross-project receipt leaked", e)
	}
	events, e := s.Events(task.ID, 0)
	if e != nil || len(events) != 1 {
		t.Fatal("lookup mutated events", e)
	}
}

func TestDefaultsCapacityAndSubjectBoundsKeepExistingRevisionsAvailable(t *testing.T) {
	s, _ := openFixture(t)
	for n := 0; n < 128; n++ {
		if _, e := s.SaveDefaultLayer("project", fmt.Sprintf("fixture-%03d", n), 0, stageplan.Layer{}); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.SaveDefaultLayer("project", "overflow", 0, stageplan.Layer{}); !errors.Is(e, ErrDefaultsCapacity) {
		t.Fatal("unbounded project default heads", e)
	}
	if _, e := s.SaveDefaultLayer("project", "fixture-000", 1, presetInput(t).Layer); e != nil {
		t.Fatal("capacity blocked existing revision", e)
	}
	if _, e := s.SaveDefaultLayer("global", "global", 0, stageplan.Layer{}); e != nil {
		t.Fatal("project cap blocked single global", e)
	}
	for _, subject := range []struct{ scope, id string }{{"task", "fixture"}, {"global", "other"}, {"project", "folder/project"}, {"project", ""}} {
		if _, e := s.SaveDefaultLayer(subject.scope, subject.id, 0, stageplan.Layer{}); !errors.Is(e, ErrInvalid) {
			t.Fatal("unsafe scope/id", e)
		}
	}
	if _, e := s.SaveDefaultLayer("global", "global", int64(1<<63-1), stageplan.Layer{}); !errors.Is(e, ErrInvalid) {
		t.Fatal("overflow base", e)
	}
}

func TestDefaultsVersionsCanonicalScopedImmutableAndRestartSafe(t *testing.T) {
	s, root := openFixture(t)
	in := presetInput(t).Layer
	first, e := s.SaveDefaultLayer("global", "global", 0, in)
	if e != nil || !first.Created || first.DefaultLayer.Revision != 1 || len(first.DefaultLayer.Layer.Roles) != 5 {
		t.Fatal("canonical global defaults", e)
	}
	second, e := s.SaveDefaultLayer("global", "global", 1, stageplan.Layer{})
	if e != nil || !second.Created || second.DefaultLayer.Revision != 2 {
		t.Fatal(e)
	}
	replay, e := s.SaveDefaultLayer("global", "global", 0, in)
	if e != nil || replay.Created || replay.DefaultLayer.Hash != first.DefaultLayer.Hash {
		t.Fatal("old retry changed latest", e)
	}
	if _, e = s.SaveDefaultLayer("global", "global", 0, stageplan.Layer{}); !errors.Is(e, ErrConflict) {
		t.Fatal("base changed payload accepted", e)
	}
	if _, e = s.SaveDefaultLayer("project", "fixture-project", 0, in); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DefaultLayer("project", "other-project", 1); !errors.Is(e, ErrNotFound) {
		t.Fatal("cross-project defaults", e)
	}
	if _, e = s.SaveDefaultLayer("global", "arbitrary", 0, in); !errors.Is(e, ErrInvalid) {
		t.Fatal("multiple global namespaces", e)
	}
	if _, e = s.db.Exec("UPDATE default_layer_revisions SET layer_json='{}'"); e == nil {
		t.Fatal("immutable history overwritten")
	}
	if _, e = s.db.Exec("DELETE FROM default_layer_heads"); e == nil {
		t.Fatal("head disappeared instead of explicit inherit revision")
	}
	s.Close()
	s, e = Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	old, e := s.DefaultLayer("global", "global", 1)
	if e != nil || old.Hash != first.DefaultLayer.Hash {
		t.Fatal("history lost on restart", e)
	}
	layers, e := s.DefaultLayers("fixture-project")
	if e != nil || layers.Global.Revision != 2 || layers.Project.Revision != 1 || layers.Stamp().GlobalHash != second.DefaultLayer.Hash {
		t.Fatal("latest pair changed", e)
	}
}

func TestDefaultsConcurrentSaveAndHeadFailureAreAtomic(t *testing.T) {
	s, _ := openFixture(t)
	var wg sync.WaitGroup
	var created atomic.Int64
	for n := 0; n < 12; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := s.SaveDefaultLayer("project", "fixture-project", 0, presetInput(t).Layer)
			if e != nil {
				t.Error(e)
			}
			if v.Created {
				created.Add(1)
			}
		}()
	}
	wg.Wait()
	if created.Load() != 1 {
		t.Fatal("duplicate revisions", created.Load())
	}
	if _, e := s.db.Exec("CREATE TRIGGER fixture_default_fail BEFORE INSERT ON default_layer_heads BEGIN SELECT RAISE(ABORT,'fixture'); END"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.SaveDefaultLayer("global", "global", 0, stageplan.Layer{}); e == nil {
		t.Fatal("failed head committed")
	}
	if _, e := s.DefaultLayer("global", "global", 1); !errors.Is(e, ErrNotFound) {
		t.Fatal("orphan default revision", e)
	}
	if _, e := s.db.Exec("DROP TRIGGER fixture_default_fail"); e != nil {
		t.Fatal(e)
	}
	if v, e := s.SaveDefaultLayer("global", "global", 0, stageplan.Layer{}); e != nil || !v.Created {
		t.Fatal("failed write consumed version", e)
	}
}

func TestDefaultsStampFencesNewTaskInTransactionWithoutChangingOldPayloadHash(t *testing.T) {
	s, _ := openFixture(t)
	in := CreateRequest{ProjectID: "fixture-project", Goal: "fixture", Plan: plan(t, 1), Budget: &Budget{MaxCalls: 3}}
	old, e := s.Create("old-key", in)
	if e != nil {
		t.Fatal(e)
	}
	before, e := s.DefaultLayers(in.ProjectID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveDefaultLayer("project", in.ProjectID, 0, presetInput(t).Layer); e != nil {
		t.Fatal(e)
	}
	if replay, e := s.CreateCurrent("old-key", in, before.Stamp()); e != nil || replay.ID != old.ID {
		t.Fatal("old receipt hash changed", e)
	}
	if _, e = s.CreateCurrent("new-key", in, before.Stamp()); !errors.Is(e, ErrDefaultsChanged) {
		t.Fatal("stale defaults created task", e)
	}
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM tasks").Scan(&count)
	if count != 1 {
		t.Fatal("failed default fence leaked task")
	}
	now, e := s.DefaultLayers(in.ProjectID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateCurrent("new-key", in, now.Stamp()); e != nil {
		t.Fatal("current defaults rejected", e)
	}
	if _, e = s.SaveDefaultLayer("project", "other-project", 0, stageplan.Layer{}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateCurrent("third-key", in, now.Stamp()); e != nil {
		t.Fatal("other project invalidated own defaults", e)
	}
}

func TestDefaultsStampFencesPlanRevisionAndCommittedRetryIsReadOnly(t *testing.T) {
	s, _ := openFixture(t)
	in := CreateRequest{ProjectID: "fixture-project", Goal: "fixture", Plan: plan(t, 1)}
	task, e := s.Create("fixture-key", in)
	if e != nil {
		t.Fatal(e)
	}
	before, e := s.DefaultLayers(in.ProjectID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveDefaultLayer("global", "global", 0, presetInput(t).Layer); e != nil {
		t.Fatal(e)
	}
	if e = s.RevisePlanCurrent(task.ID, 1, plan(t, 2), before.Stamp()); !errors.Is(e, ErrDefaultsChanged) {
		t.Fatal("stale defaults revised plan", e)
	}
	current, _ := s.Task(task.ID)
	if current.PlanRevision != 1 {
		t.Fatal("failed default fence changed plan")
	}
	now, e := s.DefaultLayers(in.ProjectID)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.RevisePlanCurrent(task.ID, 1, plan(t, 2), now.Stamp()); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveDefaultLayer("global", "global", 1, stageplan.Layer{}); e != nil {
		t.Fatal(e)
	}
	// Public API owns receipt replay. Store ordinary revision fencing remains
	// unchanged: it cannot create another revision from the stale base.
	if e = s.RevisePlanCurrent(task.ID, 1, plan(t, 2), now.Stamp()); e == nil {
		t.Fatal("stale base created repeated revision")
	}
	events, e := s.Events(task.ID, 0)
	if e != nil || len(events) != 2 {
		t.Fatal("failed fence wrote extra event", e)
	}
}

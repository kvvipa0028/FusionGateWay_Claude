package store

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

func presetInput(t *testing.T) PresetInput {
	t.Helper()
	target := plan(t, 1).Bindings[stageplan.Design].Target
	b := stageplan.Binding{Mode: stageplan.Locked, Route: &target.Route, Model: target.ResolvedModel, Effort: &stageplan.EffortSelection{Mode: stageplan.EffortDefault}}
	return PresetInput{Name: "工程预设", Layer: stageplan.Layer{Groups: map[stageplan.Group]stageplan.Binding{stageplan.DesignPlanning: b, stageplan.ImplementationTesting: b, stageplan.ReviewAcceptance: b}, Roles: map[stageplan.Role]stageplan.Binding{stageplan.Testing: {Mode: stageplan.Inherit}}}}
}
func TestPresetVersionsAreImmutableScopedAndRetriesReadExactHistory(t *testing.T) {
	s, root := openFixture(t)
	in := presetInput(t)
	first, e := s.SavePreset("fixture-project", "fixture-preset", 0, in)
	if e != nil || !first.Created || first.Preset.Revision != 1 || len(first.Preset.Layer.Roles) != 5 || first.Preset.Layer.Roles[stageplan.Testing].Mode != stageplan.Inherit {
		t.Fatal("initial canonical preset failed", e)
	}
	in.Name = "修订预设"
	second, e := s.SavePreset("fixture-project", "fixture-preset", 1, in)
	if e != nil || !second.Created || second.Preset.Revision != 2 {
		t.Fatal(e)
	}
	got, e := s.SavePreset("fixture-project", "fixture-preset", 0, presetInput(t))
	if e != nil || got.Created || got.Preset.Hash != first.Preset.Hash {
		t.Fatal("old retry followed latest", e)
	}
	if _, e = s.SavePreset("fixture-project", "fixture-preset", 0, in); !errors.Is(e, ErrConflict) {
		t.Fatal("stale changed payload accepted", e)
	}
	if _, e = s.Preset("other-project", "fixture-preset", 1); !errors.Is(e, ErrNotFound) {
		t.Fatal("cross-project preset read", e)
	}
	if _, e = s.db.Exec("UPDATE preset_revisions SET name='tamper' WHERE revision=1"); e == nil {
		t.Fatal("immutable revision overwritten")
	}
	s.Close()
	s, e = Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	old, e := s.Preset("fixture-project", "fixture-preset", 1)
	if e != nil || old.Hash != first.Preset.Hash || old.Name != "工程预设" {
		t.Fatal("restart lost old preset", e)
	}
	latest, e := s.Preset("fixture-project", "fixture-preset", 0)
	if e != nil || latest.Revision != 2 {
		t.Fatal("restart lost latest head", e)
	}
}
func TestConcurrentPresetSaveHasOneCreatedVersion(t *testing.T) {
	s, _ := openFixture(t)
	in := presetInput(t)
	var wg sync.WaitGroup
	var created atomic.Int64
	for n := 0; n < 12; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := s.SavePreset("fixture-project", "fixture-preset", 0, in)
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
		t.Fatal("concurrent duplicate versions", created.Load())
	}
	list, e := s.Presets("fixture-project")
	if e != nil || len(list) != 1 || list[0].Revision != 1 {
		t.Fatal("unexpected heads", e)
	}
}
func TestPresetHeadFailureRollsBackRevisionAndRetry(t *testing.T) {
	s, _ := openFixture(t)
	in := presetInput(t)
	if _, e := s.db.Exec("CREATE TRIGGER fixture_reject_head BEFORE INSERT ON preset_heads BEGIN SELECT RAISE(ABORT,'fixture'); END"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.SavePreset("fixture-project", "fixture-preset", 0, in); e == nil {
		t.Fatal("failed head reported success")
	}
	if _, e := s.Preset("fixture-project", "fixture-preset", 1); !errors.Is(e, ErrNotFound) {
		t.Fatal("orphan revision committed", e)
	}
	if _, e := s.db.Exec("DROP TRIGGER fixture_reject_head"); e != nil {
		t.Fatal(e)
	}
	if v, e := s.SavePreset("fixture-project", "fixture-preset", 0, in); e != nil || !v.Created {
		t.Fatal("rollback consumed revision", e)
	}
}
func TestTaskPresetProvenanceIsAtomicAndIndependentOfLatest(t *testing.T) {
	s, _ := openFixture(t)
	v, e := s.SavePreset("fixture-project", "fixture-preset", 0, presetInput(t))
	if e != nil {
		t.Fatal(e)
	}
	ref := PresetRef{ID: v.Preset.ID, Revision: 1, Hash: v.Preset.Hash}
	in := CreateRequest{ProjectID: "fixture-project", Goal: "fixture-goal", Plan: plan(t, 1), Preset: &ref, Budget: &Budget{MaxCalls: 3}}
	task, e := s.Create("fixture-key", in)
	if e != nil {
		t.Fatal(e)
	}
	changed := presetInput(t)
	changed.Name = "new name"
	if _, e = s.SavePreset("fixture-project", ref.ID, 1, changed); e != nil {
		t.Fatal(e)
	}
	got, e := s.TaskPreset(task.ID)
	if e != nil || got == nil || *got != ref {
		t.Fatal("task followed latest", e)
	}
	replay, e := s.Create("fixture-key", in)
	if e != nil || replay.ID != task.ID {
		t.Fatal("task receipt replay changed provenance", e)
	}
	ref.Hash = "wrong"
	in.Preset = &ref
	if _, e = s.Create("fixture-malformed-key", in); !errors.Is(e, ErrInvalid) {
		t.Fatal("malformed preset hash accepted", e)
	}
	ref.Hash = strings.Repeat("0", 64)
	if _, e = s.Create("fixture-bad-key", in); !errors.Is(e, ErrConflict) {
		t.Fatal("forged preset hash accepted", e)
	}
	ref.Hash = v.Preset.Hash
	in.ProjectID = "other-project"
	if _, e = s.Create("fixture-other-key", in); !errors.Is(e, ErrNotFound) {
		t.Fatal("cross-project reference adopted", e)
	}
	if _, e = s.db.Exec("CREATE TRIGGER fixture_reject_preset_ref BEFORE INSERT ON task_preset_refs BEGIN SELECT RAISE(ABORT,'fixture'); END"); e != nil {
		t.Fatal(e)
	}
	in.ProjectID = "fixture-project"
	if _, e = s.Create("fixture-rollback-key", in); e == nil {
		t.Fatal("failed provenance hidden")
	}
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM tasks").Scan(&count)
	if count != 1 {
		t.Fatal("partial task survived failed provenance")
	}
}

func TestPresetCapacityKeepsExistingVersionUpdatesAvailable(t *testing.T) {
	s, _ := openFixture(t)
	in := presetInput(t)
	for n := 0; n < 128; n++ {
		if _, e := s.SavePreset("fixture-project", fmt.Sprintf("preset-%03d", n), 0, in); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.SavePreset("fixture-project", "overflow", 0, in); !errors.Is(e, ErrPresetCapacity) {
		t.Fatal("unbounded named presets", e)
	}
	if _, e := s.SavePreset("fixture-project", "preset-000", 1, in); e != nil {
		t.Fatal("capacity blocked existing version", e)
	}
	list, e := s.Presets("fixture-project")
	if e != nil || len(list) != 128 || list[0].Revision != 2 {
		t.Fatal("head listing lost existing revisions", e)
	}
}

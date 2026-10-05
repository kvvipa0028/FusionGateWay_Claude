package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
	"github.com/yetone/magpie/internal/fusion/workflow"
)

func TestGLMStageWritePathsNarrowApprovedScopeWithoutFallback(t *testing.T) {
	in := store.StageInput{Workflow: &store.WorkflowView{Definition: workflow.Definition{RequiredRoles: stageplan.AllRoles()}, Design: &store.WorkflowDesign{Snapshot: workflow.DesignSnapshot{Document: workflow.DesignDocument{Scope: []string{"src", "tests/unit"}}}}, Approval: &store.WorkflowApproval{}}}
	paths, ok := glmStageWritePaths(stageplan.Implementation, in, []string{"tests"})
	if !ok || !reflect.DeepEqual(paths, []string{"src", "tests/unit"}) {
		t.Fatal("implementation scope", paths)
	}
	paths[0] = "other"
	if in.Workflow.Design.Snapshot.Document.Scope[0] != "src" {
		t.Fatal("scope return aliases approval")
	}
	paths, ok = glmStageWritePaths(stageplan.Testing, in, []string{"tests"})
	if !ok || !reflect.DeepEqual(paths, []string{"tests/unit"}) {
		t.Fatal("testing scope intersection", paths)
	}
	for _, tests := range [][]string{nil, {"tests2"}, {"."}, {"../tests"}} {
		if _, ok := glmStageWritePaths(stageplan.Testing, in, tests); ok {
			t.Fatal("unavailable testing scope broadened")
		}
	}
	in.Workflow.Approval = nil
	if _, ok := glmStageWritePaths(stageplan.Implementation, in, nil); ok {
		t.Fatal("missing approval allowed writes")
	}
	if paths, ok := glmStageWritePaths(stageplan.Review, in, nil); !ok || len(paths) != 0 {
		t.Fatal("review received write paths")
	}
}

func TestGLMFactoryRejectsUnsafeTestingRootsAndRefusesMissingScope(t *testing.T) {
	for _, paths := range [][]string{{"."}, {"../tests"}, {"/tmp"}, {"tests", "tests"}} {
		_, _, c, _, inspected := glmFactoryFixture(t, true)
		c.TestingWritePaths = paths
		if _, err := NewGLMRuntimeFactory(c); err == nil || inspected.Load() != 0 {
			t.Fatal("unsafe test roots admitted")
		}
	}
	path, d, c, route, _ := glmFactoryFixture(t, true)
	c.TestingWritePaths = nil
	factory, err := NewGLMRuntimeFactory(c)
	if err != nil {
		t.Fatal(err)
	}
	h, err := OpenExecutionControl(context.Background(), path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0", factory)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	plan, err := stageplan.Compile(1, []stageplan.Role{stageplan.Testing}, stageplan.Layer{}, d.Projects[0].Layer, stageplan.Layer{}, []stageplan.Route{route})
	if err != nil {
		t.Fatal(err)
	}
	task, err := h.store.Create("no-test-scope", store.CreateRequest{ProjectID: c.ProjectID, Goal: "test scope fixture", Plan: plan})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.resolveExecution(context.Background(), task, stageplan.Testing, *plan.Bindings[stageplan.Testing].Target); err == nil {
		t.Fatal("testing received full-project fallback")
	}
	if entries, err := os.ReadDir(c.ExecutionRoot); err != nil || len(entries) != 0 {
		t.Fatal("missing scope copied code", err)
	}
}

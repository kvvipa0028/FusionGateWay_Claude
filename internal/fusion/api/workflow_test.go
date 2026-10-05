package api

import (
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/fusion/store"
)

func TestExecutionWorkflowGateReturnsConflictWithoutAnotherRuntime(t *testing.T) {
	f := setupExecution(t, "")
	if _, e := f.st.AttachWorkflow(f.task.ID, store.TaskVersion{PlanRevision: 1, State: "ready"}, "investigate"); e != nil {
		t.Fatal(e)
	}
	w := executionRequest(f.h, "POST", f.startPath(), `{"role":"design"}`, "workflow-design", f.tag(t), "fixture-management")
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	r := executionReply(t, w)
	close(f.finish)
	waitAPIExecution(t, f, r.Run.ID)
	before, e := f.st.Task(f.task.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, credential := range []string{"", "fixture-management"} {
		w = executionRequest(f.h, "POST", f.startPath(), `{"role":"design"}`, "workflow-repeat", f.tag(t), credential)
		if credential == "" {
			if w.Code != 401 {
				t.Fatal("workflow bypassed management auth", w.Code)
			}
		} else if w.Code != 409 || !strings.Contains(w.Body.String(), `"code":"workflow_requires_review"`) {
			t.Fatal("workflow gate misclassified", w.Code, w.Body.String())
		}
	}
	after, e := f.st.Task(f.task.ID)
	if e != nil || after != before || f.started.Load() != 1 {
		t.Fatal("blocked stage changed Task or launched Runtime", e)
	}
}

package api

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/store"
)

func TestExecutionCancelRevisionRejectsStalePrecheckWithoutStopping(t *testing.T) {
	f := setupExecution(t, "revision")
	w := executionRequest(f.h, "POST", f.startPath(), `{"role":"design"}`, "fixture-start", f.tag(t), "fixture-management")
	var reply ExecutionReply
	if w.Code != 202 || json.Unmarshal(w.Body.Bytes(), &reply) != nil {
		t.Fatal("start failed", w.Code)
	}
	run, e := f.st.Run(reply.Run.ID)
	if e != nil {
		t.Fatal(e)
	}
	oldTag := f.tag(t)
	p := revisionPreview(t, f.h, f.task.ID)
	if w := applyRevision(f.h, f.task.ID, p, `"1"`); w.Code != 200 {
		t.Fatal("valid future-role revision failed", w.Code)
	}
	before, _ := f.st.Events(f.task.ID, 0)
	// Model the gap between the API's old read and the durable mutation.
	// Generation and the running stage's own frozen revision stay unchanged.
	if _, e = f.c.CancelAtRevision(f.task.ID, run.ID, run.Generation, 1); !errors.Is(e, store.ErrConflict) {
		t.Fatal("stale precheck cancelled the worker", e)
	}
	path := "/control/v1/tasks/" + f.task.ID + "/runs/" + run.ID + "/cancel"
	w = executionRequest(f.h, "POST", path, `{}`, "", oldTag, "fixture-management")
	if w.Code != 412 {
		t.Fatal("stale HTTP condition accepted", w.Code)
	}
	current, _ := f.st.Run(run.ID)
	after, _ := f.st.Events(f.task.ID, 0)
	if current.State != "running" || current.PlanRevision != 1 || len(after) != len(before) {
		t.Fatal("stale cancellation changed the execution or history")
	}
	w = executionRequest(f.h, "POST", path, `{}`, "", f.tag(t), "fixture-management")
	if w.Code != 202 {
		t.Fatal("current task revision cannot cancel the old frozen run", w.Code)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done, e := f.c.Wait(ctx, run.ID)
	if e != nil || done.State != "cancelled" || !done.Released {
		t.Fatal("owned cancellation did not finish/release", done, e)
	}
	if _, e = f.c.CancelAtRevision(f.task.ID, run.ID, run.Generation, 1); !errors.Is(e, store.ErrConflict) {
		t.Fatal("terminal receipt bypassed current revision", e)
	}
	w = executionRequest(f.h, "POST", path, `{}`, "", f.tag(t), "fixture-management")
	if w.Code != 200 {
		t.Fatal("current terminal cancel not read-only", w.Code)
	}
}

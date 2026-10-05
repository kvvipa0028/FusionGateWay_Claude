package api

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/fusion/store"
)

func TestStageAttemptLimitUsesExistingReviewRequiredErrorContract(t *testing.T) {
	status, body := controlErrorValue(fmt.Errorf("private diagnostic: %w", store.ErrStageLimit))
	if status != 409 || body.Code != "workflow_requires_review" || body.Message != "Conflict" {
		t.Fatal("attempt cap broke the public error contract", status, body)
	}
}

func TestStageAttemptLimitAuthenticatedStartFencesTaskAndKeepsOriginalReplay(t *testing.T) {
	f := setupExecution(t, "")
	close(f.finish)
	var oldTag, oldRun string
	for n, key := range []string{"attempt-first", "attempt-remedial"} {
		oldTag = f.tag(t)
		w := executionRequest(f.h, "POST", f.startPath(), `{"role":"design"}`, key, oldTag, "fixture-management")
		if w.Code != 202 {
			t.Fatal("first or remedial HTTP request failed", w.Code, w.Body.String())
		}
		out := executionReply(t, w)
		if !out.Created || out.Run.Attempt != int64(n+1) {
			t.Fatal("attempt number/replay changed")
		}
		oldRun = out.Run.ID
		waitAPIExecution(t, f, oldRun)
	}
	tag := f.tag(t)
	budget, e := f.st.Budget(f.task.ID)
	if e != nil {
		t.Fatal(e)
	}
	// Authentication and task conditions must fail before the cap mutation.
	for _, denied := range []struct {
		credential, tag string
		code            int
	}{{"fixture-wrong", tag, 401}, {"fixture-management", `"p1-g1-ready"`, 412}} {
		w := executionRequest(f.h, "POST", f.startPath(), `{"role":"design"}`, "attempt-third", denied.tag, denied.credential)
		if w.Code != denied.code || f.tag(t) != tag {
			t.Fatal("denied HTTP request mutated exhausted task", w.Code)
		}
	}
	w := executionRequest(f.h, "POST", f.startPath(), `{"role":"design"}`, "attempt-third", tag, "fixture-management")
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"code":"workflow_requires_review"`) {
		t.Fatal("third HTTP attempt admitted", w.Code, w.Body.String())
	}
	task, e := f.st.Task(f.task.ID)
	if e != nil || task.State != "needs_review" || task.Generation != 3 || f.started.Load() != 2 {
		t.Fatal("HTTP cap did not preserve actual dispatch count", e)
	}
	after, e := f.st.Budget(f.task.ID)
	if e != nil || budget != after {
		t.Fatal("HTTP cap changed budget", e)
	}
	w = executionRequest(f.h, "POST", f.startPath(), `{"role":"design"}`, "attempt-remedial", oldTag, "fixture-management")
	if w.Code != 200 {
		t.Fatal("original key cannot reconnect after cap", w.Code, w.Body.String())
	}
	out := executionReply(t, w)
	if out.Created || out.Run.ID != oldRun || f.started.Load() != 2 {
		t.Fatal("HTTP replay relaunched Runtime")
	}
}

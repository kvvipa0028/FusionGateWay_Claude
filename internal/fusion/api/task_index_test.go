package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/fusion/store"
)

func TestTaskIndexEmptyRegisteredProject(t *testing.T) {
	_, _, h := setup(t)
	w := request(h, "GET", "/control/v1/projects/fixture-project/tasks", "", "", "fixture-management")
	if w.Code != 200 {
		t.Fatal("missing workbench task index", w.Code)
	}
	var out struct {
		Tasks []json.RawMessage `json:"tasks"`
		Next  string            `json:"next_before"`
	}
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Tasks == nil || len(out.Tasks) != 0 || out.Next != "" {
		t.Fatal("empty index must be an explicit array without an invented cursor")
	}
}

func TestTaskIndexProjectionPagingAndCurrentCondition(t *testing.T) {
	_, st, h := setup(t)
	p := preview(t, h)
	var tasks []store.Task
	for i := 0; i < 35; i++ {
		task, err := st.Create(fmt.Sprint(i), store.CreateRequest{ProjectID: "fixture-project", Goal: strings.Repeat("中😀\n", 512), Plan: p.Plan})
		if err != nil {
			t.Fatal(err)
		}
		tasks = append(tasks, task)
	}
	foreign, err := st.Create("foreign", store.CreateRequest{ProjectID: "other-project", Goal: "PRIVATE_FOREIGN_GOAL", Plan: p.Plan})
	if err != nil {
		t.Fatal(err)
	}
	last := tasks[34]
	if _, err := st.PauseTask(last.ID, store.TaskVersion{PlanRevision: last.PlanRevision, Generation: last.Generation, State: last.State}, ""); err != nil {
		t.Fatal(err)
	}
	path := "/control/v1/projects/fixture-project/tasks"
	w := request(h, "GET", path, "", "", "fixture-management")
	var out TaskListReply
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Tasks) != 32 || out.NextBefore != tasks[3].ID {
		t.Fatal("index page", w.Code)
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("index cache")
	}
	for i, entry := range out.Tasks {
		if entry.ID != tasks[34-i].ID || entry.ProjectID != "fixture-project" || !entry.GoalTruncated || utf8.RuneCountInString(entry.Goal) != 512 || !utf8.ValidString(entry.Goal) {
			t.Fatal("summary/order", i)
		}
		taskRead := request(h, "GET", "/agent/v1/tasks/"+entry.ID, "", "", "fixture-management")
		var complete store.Task
		if taskRead.Code != 200 || json.Unmarshal(taskRead.Body.Bytes(), &complete) != nil || entry.ETag != taskRead.Header().Get("ETag") || entry.State != complete.State || entry.Generation != complete.Generation || len(complete.Goal) <= len(entry.Goal) {
			t.Fatal("condition or full goal changed", i)
		}
		var raw map[string]any
		b, _ := json.Marshal(entry)
		if json.Unmarshal(b, &raw) != nil || len(raw) != 8 {
			t.Fatal("private fields exposed")
		}
	}
	if strings.Contains(w.Body.String(), "PRIVATE_FOREIGN") {
		t.Fatal("foreign task exposed")
	}
	w = request(h, "GET", path+"/before/"+out.NextBefore, "", "", "fixture-management")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Tasks) != 3 || out.NextBefore != "" {
		t.Fatal("older page", w.Code)
	}
	for i, entry := range out.Tasks {
		if entry.ID != tasks[2-i].ID {
			t.Fatal("old page order")
		}
	}
	for _, anchor := range []string{foreign.ID, "missing"} {
		if w := request(h, "GET", path+"/before/"+anchor, "", "", "fixture-management"); w.Code != 404 || strings.Contains(w.Body.String(), "PRIVATE_FOREIGN") {
			t.Fatal("foreign/missing cursor", w.Code)
		}
	}
	events, err := st.Events(tasks[0].ID, 0)
	if err != nil || len(events) != 1 || events[0].Kind != "created" {
		t.Fatal("list mutated execution", err)
	}
}

func TestTaskIndexResourceSegmentsAndShortGoals(t *testing.T) {
	s, st, h := setup(t)
	p := preview(t, h)
	for _, project := range []string{"tasks", "presets", "quota", "configuration"} {
		if err := s.SetProject(project, configuration()); err != nil {
			t.Fatal(err)
		}
		task, err := st.Create("same", store.CreateRequest{ProjectID: project, Goal: "brief", Plan: p.Plan})
		if err != nil {
			t.Fatal(err)
		}
		w := request(h, "GET", "/control/v1/projects/"+project+"/tasks", "", "", "fixture-management")
		var out TaskListReply
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Tasks) != 1 || out.Tasks[0].ID != task.ID || out.Tasks[0].Goal != "brief" || out.Tasks[0].GoalTruncated {
			t.Fatal("resource collision", project, w.Code)
		}
	}
}

func TestTaskIndexManagementPathAndReadOnlyBoundary(t *testing.T) {
	path := "/control/v1/projects/fixture-project/tasks"
	for _, tc := range []struct {
		name, method, path, token string
		status                    int
	}{
		{"missing", "GET", path, "", 401}, {"wrong", "GET", path, "wrong", 401}, {"stage", "GET", path, "fgs_fixture", 403},
		{"post", "POST", path, "fixture-management", 405}, {"put", "PUT", path, "fixture-management", 405}, {"head", "HEAD", path, "fixture-management", 405},
		{"unknown", "GET", "/control/v1/projects/unknown/tasks", "fixture-management", 404},
		{"query", "GET", path + "?before=other", "fixture-management", 400},
		{"empty-anchor", "GET", path + "/before/", "fixture-management", 400}, {"extra", "GET", path + "/before/id/extra", "fixture-management", 400},
		{"trailing", "GET", path + "/", "fixture-management", 400}, {"unknown-child", "GET", path + "/start", "fixture-management", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, h := setup(t)
			w := request(h, tc.method, tc.path, "", "", tc.token)
			if w.Code != tc.status {
				t.Fatal("index boundary", w.Code, tc.status)
			}
			if tc.status == 405 && w.Header().Get("Allow") != "GET" {
				t.Fatal("methods")
			}
		})
	}
	t.Run("cross-site", func(t *testing.T) {
		_, _, h := setup(t)
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Authorization", "Bearer fixture-management")
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal(w.Code)
		}
	})
	t.Run("closed-store", func(t *testing.T) {
		_, st, h := setup(t)
		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
		w := request(h, "GET", path, "", "", "fixture-management")
		if w.Code != 500 || strings.Contains(w.Body.String(), "fixture-project") {
			t.Fatal("partial failed index", w.Code)
		}
	})
}

func TestTaskIndexRechecksAuthorityAfterBlockedRead(t *testing.T) {
	for _, mode := range []string{"revoke", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			s, _, _ := setup(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered, done := make(chan struct{}), make(chan struct{})
			h := s.auth.Management(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); s.taskIndex(w, r) }))
			r := httptest.NewRequest("GET", "/control/v1/projects/fixture-project/tasks", nil).WithContext(ctx)
			r.Header.Set("Authorization", "Bearer fixture-management")
			w := httptest.NewRecorder()
			s.mu.Lock()
			go func() { defer close(done); h.ServeHTTP(w, r) }()
			select {
			case <-entered:
			case <-time.After(time.Second):
				s.mu.Unlock()
				t.Fatal("middleware wait")
			}
			if mode == "revoke" {
				s.auth.RevokeManagement()
			} else {
				cancel()
			}
			s.mu.Unlock()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("index wait")
			}
			if w.Code != 401 || strings.Contains(w.Body.String(), "tasks") {
				t.Fatal("late authority leak", w.Code)
			}
		})
	}
}

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/store"
)

func projectIndex(t *testing.T, h http.Handler) []struct {
	ID       string `json:"id"`
	Revision int64  `json:"configuration_revision"`
} {
	t.Helper()
	w := request(h, "GET", "/control/v1/projects", "", "", "fixture-management")
	if w.Code != 200 {
		t.Fatal("project index", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("unsafe inventory cache")
	}
	var out struct {
		Projects []struct {
			ID       string `json:"id"`
			Revision int64  `json:"configuration_revision"`
		} `json:"projects"`
	}
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Projects == nil {
		t.Fatal("project inventory must be an array")
	}
	return out.Projects
}

func TestProjectIndexIsSortedMinimalAndDoesNotAdmitDrafts(t *testing.T) {
	s, st, h := setup(t)
	c := configuration()
	c.Routes[0].Admitted = false
	c.Routes[0].CredentialIdentity = "PRIVATE_INDEX_CREDENTIAL"
	c.Routes[0].Workspace = "/PRIVATE_INDEX_PATH"
	for _, id := range []string{"z-project", "a-project"} {
		if err := s.SetProject(id, c); err != nil {
			t.Fatal(err)
		}
	}
	projects := projectIndex(t, h)
	if len(projects) != 3 || projects[0].ID != "a-project" || projects[1].ID != "fixture-project" || projects[2].ID != "z-project" {
		t.Fatal("unstable inventory ordering", projects)
	}
	w := request(h, "GET", "/control/v1/projects", "", "", "fixture-management")
	var raw map[string][]map[string]any
	if json.Unmarshal(w.Body.Bytes(), &raw) != nil || len(raw) != 1 {
		t.Fatal("inventory shape")
	}
	for _, p := range raw["projects"] {
		if len(p) != 2 {
			t.Fatal("inventory leaked configuration")
		}
	}
	if strings.Contains(w.Body.String(), "PRIVATE_INDEX") || strings.Contains(w.Body.String(), "fixture-management") {
		t.Fatal("inventory leaked private metadata")
	}
	w = request(h, "POST", "/control/v1/tasks/preview", `{"project_id":"a-project","goal":"fixture","required_roles":["design"]}`, "", "fixture-management")
	if w.Code != 422 {
		t.Fatal("listing admitted a draft", w.Code)
	}
	if _, err := st.Task("a-project"); err != store.ErrNotFound {
		t.Fatal("listing created task", err)
	}
}

func TestProjectIndexEmptyCapacityAndCurrentDefaults(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		s, st, _ := setup(t)
		empty, err := New(st, s.auth)
		if err != nil {
			t.Fatal(err)
		}
		if len(projectIndex(t, empty.Handler())) != 0 {
			t.Fatal("invented project")
		}
	})
	t.Run("capacity", func(t *testing.T) {
		s, _, h := setup(t)
		for i := 0; i < 127; i++ {
			id := strings.Repeat("a", i+1)
			if err := s.SetProject(id, configuration()); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.SetProject("overflow", configuration()); err != errCapacity {
			t.Fatal("registry capacity", err)
		}
		if len(projectIndex(t, h)) != 128 {
			t.Fatal("inventory truncated")
		}
	})
	t.Run("defaults", func(t *testing.T) {
		_, st, h := setup(t)
		p := preview(t, h)
		created := submit(t, h, p, "fixture-index-before-defaults")
		if created.Code != 201 {
			t.Fatal(created.Code)
		}
		var task store.Task
		if json.Unmarshal(created.Body.Bytes(), &task) != nil {
			t.Fatal("task decode")
		}
		before := projectIndex(t, h)
		w := revisionRequest(h, "PUT", "/control/v1/projects/fixture-project/defaults", `{"layer":{}}`, `"0"`)
		if w.Code != 201 {
			t.Fatal("defaults", w.Code)
		}
		after := projectIndex(t, h)
		if after[0].Revision != before[0].Revision+1 || projectIndex(t, h)[0].Revision != after[0].Revision {
			t.Fatal("defaults revision omitted or read mutates revision")
		}
		saved, err := st.Plan(task.ID, task.PlanRevision)
		if err != nil || saved.Hash != p.Plan.Hash {
			t.Fatal("listing recompiled task", err)
		}
		config := request(h, "GET", "/control/v1/projects/fixture-project/configuration", "", "", "fixture-management")
		var current struct {
			Revision int64 `json:"revision"`
		}
		if json.Unmarshal(config.Body.Bytes(), &current) != nil || current.Revision != after[0].Revision {
			t.Fatal("inventory differs from selected configuration")
		}
	})
	t.Run("closed-store", func(t *testing.T) {
		_, st, h := setup(t)
		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
		w := request(h, "GET", "/control/v1/projects", "", "", "fixture-management")
		if w.Code != 500 || strings.Contains(w.Body.String(), "fixture-project") {
			t.Fatal("partial inventory on storage failure", w.Code)
		}
	})
}

func TestProjectIndexManagementAndReadOnlyBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, token string
		status                    int
	}{
		{"missing", "GET", "/control/v1/projects", "", 401},
		{"wrong", "GET", "/control/v1/projects", "wrong", 401},
		{"stage", "GET", "/control/v1/projects", "fgs_fixture", 403},
		{"query", "GET", "/control/v1/projects?project_id=other", "fixture-management", 400},
		{"post", "POST", "/control/v1/projects", "fixture-management", 405},
		{"put", "PUT", "/control/v1/projects", "fixture-management", 405},
		{"head", "HEAD", "/control/v1/projects", "fixture-management", 405},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, h := setup(t)
			w := request(h, tc.method, tc.path, "", "", tc.token)
			if w.Code != tc.status {
				t.Fatal("inventory boundary", w.Code, tc.status)
			}
		})
	}
	t.Run("cross-site", func(t *testing.T) {
		_, _, h := setup(t)
		req := httptest.NewRequest("GET", "/control/v1/projects", nil)
		req.Header.Set("Authorization", "Bearer fixture-management")
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 403 {
			t.Fatal("cross-site inventory", w.Code)
		}
	})
	t.Run("revoked", func(t *testing.T) {
		s, _, h := setup(t)
		s.auth.RevokeManagement()
		w := request(h, "GET", "/control/v1/projects", "", "", "fixture-management")
		if w.Code != 401 {
			t.Fatal("revoked inventory", w.Code)
		}
	})
}

func TestProjectIndexConcurrentRegistration(t *testing.T) {
	s, _, h := setup(t)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 16; i++ {
			if err := s.SetProject("fixture-project", configuration()); err != nil {
				t.Error(err)
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 16; i++ {
			w := request(h, "GET", "/control/v1/projects", "", "", "fixture-management")
			if w.Code != 200 {
				t.Error("concurrent inventory", w.Code)
			}
		}
	}()
	wg.Wait()
	if got := projectIndex(t, h); len(got) != 1 || got[0].Revision != 17 {
		t.Fatal("lost registration", got)
	}
}

// Hold the actual registry lock after middleware authentication, then revoke
// while the read is waiting. The response must never contain the inventory.
func TestProjectIndexRechecksAuthorityAfterBlockedRead(t *testing.T) {
	for _, mode := range []string{"revoke", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			s, _, _ := setup(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered, done := make(chan struct{}), make(chan struct{})
			h := s.auth.Management(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				s.projectsControl(w, r)
			}))
			req := httptest.NewRequest("GET", "/control/v1/projects", nil).WithContext(ctx)
			req.Header.Set("Authorization", "Bearer fixture-management")
			w := httptest.NewRecorder()
			s.mu.Lock()
			go func() { defer close(done); h.ServeHTTP(w, req) }()
			select {
			case <-entered:
			case <-time.After(time.Second):
				s.mu.Unlock()
				t.Fatal("middleware not entered")
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
				t.Fatal("inventory wait did not end")
			}
			if w.Code != 401 || strings.Contains(w.Body.String(), "fixture-project") {
				t.Fatal("late authority failure leaked inventory", w.Code)
			}
		})
	}
}

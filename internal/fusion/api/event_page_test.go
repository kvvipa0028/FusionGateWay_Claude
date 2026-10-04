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

	"github.com/yetone/magpie/internal/fusion/store"
)

func TestEventPageReadsContiguousDurableSuffixWithoutWork(t *testing.T) {
	_, st, h := setup(t)
	task := submittedTask(t, h)
	for revision := int64(1); revision <= 35; revision++ {
		revise(t, st, task.ID, revision)
	}
	before, e := st.Task(task.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		after, next int64
		count       int
		more        bool
	}{{0, 32, 32, true}, {32, 36, 4, false}, {36, 36, 0, false}} {
		path := fmt.Sprintf("/agent/v1/tasks/%s/events/page/%d", task.ID, tc.after)
		w := request(h, "GET", path, "", "", "fixture-management")
		var out struct {
			TaskID    string        `json:"task_id"`
			After     int64         `json:"after"`
			NextAfter int64         `json:"next_after"`
			HasMore   bool          `json:"has_more"`
			Events    []store.Event `json:"events"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatal("event page unavailable", w.Code)
		}
		if out.TaskID != task.ID || out.After != tc.after || out.NextAfter != tc.next || out.HasMore != tc.more || out.Events == nil || len(out.Events) != tc.count {
			t.Fatal("event page lost sequence")
		}
		for n, v := range out.Events {
			if v.TaskID != task.ID || v.Seq != tc.after+int64(n)+1 {
				t.Fatal("event page not contiguous")
			}
		}
	}
	after, e := st.Task(task.ID)
	if e != nil || after != before {
		t.Fatal("reading events changed Task")
	}
	all, e := st.Events(task.ID, 0)
	if e != nil || len(all) != 36 {
		t.Fatal("reading events created work")
	}
}

func TestEventPageRejectsInvalidCursorScopeMethodAndCredentials(t *testing.T) {
	s, _, h := setup(t)
	task := submittedTask(t, h)
	base := "/agent/v1/tasks/" + task.ID + "/events/page/"
	for _, tc := range []struct {
		path, method, credential string
		code                     int
	}{
		{base + "2", "GET", "fixture-management", 409}, {base + "-1", "GET", "fixture-management", 400},
		{base + "01", "GET", "fixture-management", 400}, {base + "+1", "GET", "fixture-management", 400}, {base + "9223372036854775808", "GET", "fixture-management", 400},
		{base + "0/extra", "GET", "fixture-management", 400}, {base, "GET", "fixture-management", 400},
		{base + "0", "POST", "fixture-management", 405}, {base + "0", "GET", "", 401}, {base + "0", "GET", "fgs_fixture", 403},
		{"/agent/v1/tasks/missing/events/page/0", "GET", "fixture-management", 404},
	} {
		w := request(h, tc.method, tc.path, "", "", tc.credential)
		if w.Code != tc.code {
			t.Fatal(tc.path, w.Code, tc.code)
		}
		if strings.Contains(w.Body.String(), task.ID) {
			t.Fatal("error leaked history")
		}
	}
	r := httptest.NewRequest("GET", base+"0", nil)
	r.Header.Set("Authorization", "Bearer fixture-management")
	r.Header.Set("Last-Event-ID", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("conflicting cursor header accepted", w.Code)
	}
	s.mu.Lock()
	delete(s.projects, task.ProjectID)
	s.mu.Unlock()
	w = request(h, "GET", base+"0", "", "", "fixture-management")
	if w.Code != 404 {
		t.Fatal("unregistered project history disclosed", w.Code)
	}
}

func TestEventPageRechecksAuthorityAfterBlockedLookup(t *testing.T) {
	for _, mode := range []string{"revoke", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			s, _, h := setup(t)
			task := submittedTask(t, h)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered, done := make(chan struct{}), make(chan struct{})
			handler := s.auth.Management(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); s.eventsPage(w, r) }))
			r := httptest.NewRequest("GET", "/agent/v1/tasks/"+task.ID+"/events/page/0", nil).WithContext(ctx)
			r.Header.Set("Authorization", "Bearer fixture-management")
			w := httptest.NewRecorder()
			s.mu.Lock()
			go func() { defer close(done); handler.ServeHTTP(w, r) }()
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
				t.Fatal("event page wait")
			}
			if w.Code != 401 || strings.Contains(w.Body.String(), task.ID) || strings.Contains(w.Body.String(), "created") {
				t.Fatal("late authority leak", w.Code)
			}
		})
	}
}

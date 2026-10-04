package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/store"
)

type submissionReadSignal struct {
	io.Reader
	once    sync.Once
	entered chan struct{}
}

func (b *submissionReadSignal) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.entered) })
	return b.Reader.Read(p)
}

func TestSubmissionReceiptDoesNotDiscloseAfterAuthorityRevokedDuringRead(t *testing.T) {
	for _, mode := range []string{"revoke", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			s, _, h := setup(t)
			p := preview(t, h)
			first := submit(t, h, p, "fixture-revoke-key")
			var task store.Task
			if first.Code != 201 || json.Unmarshal(first.Body.Bytes(), &task) != nil {
				t.Fatal(first.Code)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			body, _ := json.Marshal(SubmitRequest{PreviewID: p.ID, PlanHash: p.Plan.Hash})
			entered, done := make(chan struct{}), make(chan struct{})
			r := httptest.NewRequest("POST", "/agent/v1/tasks", &submissionReadSignal{Reader: strings.NewReader(string(body)), entered: entered}).WithContext(ctx)
			r.Header.Set("Authorization", "Bearer fixture-management")
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Idempotency-Key", "fixture-revoke-key")
			w := httptest.NewRecorder()
			s.mu.Lock()
			go func() { defer close(done); h.ServeHTTP(w, r) }()
			select {
			case <-entered:
			case <-time.After(time.Second):
				s.mu.Unlock()
				t.Fatal("authenticated body not read")
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
				t.Fatal("receipt read did not finish")
			}
			if w.Code != 401 || strings.Contains(w.Body.String(), task.ID) || strings.Contains(w.Body.String(), task.Goal) {
				t.Fatal("late authority disclosed task", w.Code)
			}
		})
	}
}

func TestSubmissionReceiptSurvivesPreviewEviction(t *testing.T) {
	s, st, h := setup(t)
	p := preview(t, h)
	w := submit(t, h, p, "fixture-durable-key")
	var original store.Task
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &original) != nil {
		t.Fatal("initial submit", w.Code)
	}
	before, e := st.Events(original.ID, 0)
	if e != nil {
		t.Fatal(e)
	}
	s.now = func() time.Time { return p.ExpiresAt.Add(time.Second) }
	preview(t, h) // Exercise the real expiry pruning path, not a manually deleted map.
	if _, exists := s.previews[p.ID]; exists {
		t.Fatal("expired fixture was not pruned")
	}
	w = submit(t, h, p, "fixture-durable-key")
	var recovered store.Task
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &recovered) != nil || recovered != original {
		t.Fatal("evicted committed preview lost its receipt", w.Code)
	}
	after, e := st.Events(original.ID, 0)
	if e != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("recovery changed events", e)
	}
	page, e := st.ProjectTasks(context.Background(), original.ProjectID, "")
	if e != nil || len(page.Tasks) != 1 {
		t.Fatal("recovery created a second task", e)
	}
}

func TestSubmissionReceiptSurvivesStoreReopenAndRequiresRegisteredProject(t *testing.T) {
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(root, 0700); e != nil {
		t.Fatal(e)
	}
	st, e := store.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { st.Close() })
	s, e := New(st, policy.NewManager("fixture-management", nil, nil))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.SetProject("fixture-project", configuration()); e != nil {
		t.Fatal(e)
	}
	p := preview(t, s.Handler())
	uncommitted := preview(t, s.Handler())
	w := submit(t, s.Handler(), p, "fixture-reopen-key")
	var original store.Task
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &original) != nil {
		t.Fatal(w.Code)
	}
	if e = st.Close(); e != nil {
		t.Fatal(e)
	}
	st, e = store.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	s, e = New(st, policy.NewManager("fixture-management", nil, nil))
	if e != nil {
		t.Fatal(e)
	}
	if w = submit(t, s.Handler(), p, "fixture-reopen-key"); w.Code != 404 {
		t.Fatal("unregistered project recovered", w.Code)
	}
	if e = s.SetProject("fixture-project", configuration()); e != nil {
		t.Fatal(e)
	}
	if w = submit(t, s.Handler(), p, "fixture-reopen-key"); w.Code != 201 {
		t.Fatal("reopened receipt unavailable", w.Code)
	}
	var recovered store.Task
	if json.Unmarshal(w.Body.Bytes(), &recovered) != nil || recovered != original {
		t.Fatal("reopened task changed")
	}
	for _, tc := range []struct {
		name    string
		preview Preview
		key     string
	}{
		{"wrong-key", p, "fixture-other-key"},
		{"missing-preview", uncommitted, "fixture-reopen-key"},
		{"wrong-hash", func() Preview { q := p; q.Plan.Hash = "fixture-tampered"; return q }(), "fixture-reopen-key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if w := submit(t, s.Handler(), tc.preview, tc.key); w.Code != 409 {
				t.Fatal("identity mismatch accepted", w.Code)
			}
		})
	}
	raw, _ := json.Marshal(SubmitRequest{PreviewID: p.ID, PlanHash: p.Plan.Hash})
	if w = request(s.Handler(), http.MethodPost, "/agent/v1/tasks", string(raw), "fixture-reopen-key", ""); w.Code != 401 {
		t.Fatal("unauthenticated recovery", w.Code)
	}
	if w = submit(t, s.Handler(), uncommitted, "fixture-uncommitted-key"); w.Code != 409 {
		t.Fatal("restart recreated uncommitted preview", w.Code)
	}
}

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/store"
)

const journalPath = "/control/v1/projects/fixture-project/submission"

type journalFixtureView struct {
	ProjectID string  `json:"project_id"`
	Goal      string  `json:"goal"`
	Key       string  `json:"key"`
	State     string  `json:"state"`
	TaskID    string  `json:"task_id"`
	Preview   Preview `json:"preview"`
}

func journalBody(p Preview) string {
	b, _ := json.Marshal(SubmitRequest{PreviewID: p.ID, PlanHash: p.Plan.Hash})
	return string(b)
}
func readJournalFixture(t *testing.T, w *httptest.ResponseRecorder) *journalFixtureView {
	t.Helper()
	var out struct {
		Submission *journalFixtureView `json:"submission"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatal("submission journal response", w.Code)
	}
	return out.Submission
}
func journalCall(h http.Handler, method, path string, p Preview, key string) *httptest.ResponseRecorder {
	return request(h, method, path, journalBody(p), key, "fixture-management")
}

func TestSubmissionJournalAPIOriginalLifecycleAndRestart(t *testing.T) {
	s, st, h := setup(t)
	if got := readJournalFixture(t, request(h, "GET", journalPath, "", "", "fixture-management")); got != nil {
		t.Fatal("invented pending request")
	}
	p := preview(t, h)
	p.ExpiresAt = p.ExpiresAt.UTC() // Store canonicalizes the same instant to UTC.
	key := "fixture-journal-key"
	prepared := readJournalFixture(t, journalCall(h, "POST", journalPath, p, key))
	if prepared == nil || prepared.State != "prepared" || prepared.TaskID != "" || prepared.ProjectID != "fixture-project" || prepared.Goal != "fixture-goal" || prepared.Key != key || !reflect.DeepEqual(prepared.Preview, p) {
		t.Fatal("trusted original preview not frozen")
	}
	page, e := st.ProjectTasks(context.Background(), "fixture-project", "")
	if e != nil || len(page.Tasks) != 0 {
		t.Fatal("prepare created task", e)
	}
	// Expiry and a changed trusted configuration cannot reinterpret a saved request.
	s.now = func() time.Time { return p.ExpiresAt.Add(time.Second) }
	preview(t, h)
	if e = s.SetProject("fixture-project", configuration()); e != nil {
		t.Fatal(e)
	}
	if got := readJournalFixture(t, journalCall(h, "POST", journalPath, p, key)); !reflect.DeepEqual(got, prepared) {
		t.Fatal("prepare retry changed original")
	}
	if w := journalCall(h, "POST", journalPath+"/acknowledge", p, key); w.Code != 409 {
		t.Fatal("uncommitted request acknowledged", w.Code)
	}
	// A fresh Server lacks the preview. Reading a journal does not resurrect it.
	restarted, e := New(st, policy.NewManager("fixture-management", nil, nil))
	if e != nil {
		t.Fatal(e)
	}
	if w := journalCall(restarted.Handler(), "GET", journalPath, p, key); w.Code != 404 {
		t.Fatal("unregistered journal disclosed", w.Code)
	}
	if e = restarted.SetProject("fixture-project", configuration()); e != nil {
		t.Fatal(e)
	}
	h = restarted.Handler()
	if got := readJournalFixture(t, journalCall(h, "POST", journalPath, p, key)); !reflect.DeepEqual(got, prepared) {
		t.Fatal("restart lost prepare acknowledgement")
	}
	if w := submit(t, h, p, key); w.Code != 409 {
		t.Fatal("journal resurrected missing preview", w.Code)
	}
	sealed := readJournalFixture(t, journalCall(h, "POST", journalPath+"/abandon", p, key))
	if sealed.State != "abandoned" || sealed.TaskID != "" {
		t.Fatal("original not sealed")
	}
	if got := readJournalFixture(t, journalCall(h, "POST", journalPath, p, key)); !reflect.DeepEqual(got, sealed) {
		t.Fatal("terminal prepare retry reopened journal")
	}
	if got := readJournalFixture(t, journalCall(h, "POST", journalPath+"/abandon", p, key)); !reflect.DeepEqual(got, sealed) {
		t.Fatal("abandon retry changed original")
	}
	if got := readJournalFixture(t, request(h, "GET", journalPath, "", "", "fixture-management")); got != nil {
		t.Fatal("terminal remained pending")
	}
	// Even the earlier Server's preview cannot commit after the durable seal.
	if w := submit(t, s.Handler(), p, key); w.Code != 409 {
		t.Fatal("late original committed", w.Code)
	}
	p = preview(t, h)
	committed := readJournalFixture(t, journalCall(h, "POST", journalPath, p, key))
	w := submit(t, h, p, key)
	var task store.Task
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &task) != nil {
		t.Fatal("original submit", w.Code)
	}
	committed = readJournalFixture(t, request(h, "GET", journalPath, "", "", "fixture-management"))
	if committed.State != "committed" || committed.TaskID != task.ID {
		t.Fatal("committed journal not synchronized")
	}
	if w := journalCall(h, "POST", journalPath+"/abandon", p, key); w.Code != 409 {
		t.Fatal("committed request abandoned", w.Code)
	}
	ack := readJournalFixture(t, journalCall(h, "POST", journalPath+"/acknowledge", p, key))
	if ack.State != "acknowledged" || ack.TaskID != task.ID {
		t.Fatal("task receipt not acknowledged")
	}
	if got := readJournalFixture(t, journalCall(h, "POST", journalPath+"/acknowledge", p, key)); !reflect.DeepEqual(got, ack) {
		t.Fatal("ack retry changed original")
	}
	restarted, e = New(st, policy.NewManager("fixture-management", nil, nil))
	if e != nil {
		t.Fatal(e)
	}
	if e = restarted.SetProject("fixture-project", configuration()); e != nil {
		t.Fatal(e)
	}
	if got := readJournalFixture(t, journalCall(restarted.Handler(), "POST", journalPath, p, key)); !reflect.DeepEqual(got, ack) {
		t.Fatal("terminal prepare lost after restart")
	}
	events, e := st.Events(task.ID, 0)
	if e != nil || len(events) != 1 {
		t.Fatal("journal rewrote task history", e)
	}
}

func TestSubmissionJournalAPIRejectsWrongIdentityAndUntrustedDraft(t *testing.T) {
	_, _, h := setup(t)
	p := preview(t, h)
	readJournalFixture(t, journalCall(h, "POST", journalPath, p, "original-key"))
	other := preview(t, h)
	for _, tc := range []struct {
		path   string
		p      Preview
		key    string
		status int
	}{
		{journalPath, p, "different-key", 409}, {journalPath, other, "original-key", 409},
		{journalPath + "/abandon", p, "different-key", 409},
		{"/control/v1/projects/not-registered/submission", p, "original-key", 404},
	} {
		if w := journalCall(h, "POST", tc.path, tc.p, tc.key); w.Code != tc.status {
			t.Fatal("identity accepted", tc.status, w.Code)
		}
	}
	for _, body := range []string{
		`null`, `{}`, `{"preview_id":"missing","plan_hash":"x","goal":"replace"}`,
		`{"preview_id":"missing","plan_hash":"x","request":{}}`,
		`{"preview_id":"missing","plan_hash":"x","token":"fixture-forbidden"}`,
		`{"preview_id":"missing","plan_hash":"x","workspace":"/foreign"}`,
		`{"preview_id":"missing","preview_id":"duplicate","plan_hash":"x"}`,
	} {
		if w := request(h, "POST", journalPath, body, "original-key", "fixture-management"); w.Code != 400 || strings.Contains(w.Body.String(), "fixture-forbidden") {
			t.Fatal("untrusted draft accepted", w.Code)
		}
	}
	for _, path := range []string{journalPath, journalPath + "/acknowledge", journalPath + "/abandon"} {
		if w := request(h, "POST", path, journalBody(p), "", "fixture-management"); w.Code != 400 {
			t.Fatal("missing key", w.Code)
		}
		if w := request(h, "POST", path, journalBody(p), "original-key", ""); w.Code != 401 {
			t.Fatal("no authorization", w.Code)
		}
		r := httptest.NewRequest("POST", path, strings.NewReader(journalBody(p)))
		r.Header.Set("Authorization", "Bearer fixture-management")
		r.Header.Add("Idempotency-Key", "original-key")
		r.Header.Add("Idempotency-Key", "other")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal("multiple keys accepted", w.Code)
		}
	}
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"PUT", journalPath, 405}, {"GET", journalPath + "/abandon", 405},
		{"POST", journalPath + "/unknown", 400}, {"GET", journalPath + "/abandon/extra", 400},
		{"GET", journalPath + "?key=original-key", 403},
	} {
		if w := request(h, tc.method, tc.path, "", "", "fixture-management"); w.Code != tc.status {
			t.Fatal("path/method accepted", tc.status, w.Code)
		}
	}
}

func TestSubmissionJournalAPIRequiresLivePreviewForNewPrepare(t *testing.T) {
	for _, mode := range []string{"expired", "configuration", "revision", "foreign", "missing"} {
		t.Run(mode, func(t *testing.T) {
			s, st, h := setup(t)
			p := preview(t, h)
			switch mode {
			case "expired":
				s.now = func() time.Time { return p.ExpiresAt }
			case "configuration":
				if e := s.SetProject("fixture-project", configuration()); e != nil {
					t.Fatal(e)
				}
			case "revision":
				s.previews[p.ID].taskID = "fixture-task"
			case "foreign":
				s.previews[p.ID].request.ProjectID = "different-project"
			case "missing":
				p.ID = "missing-preview"
			}
			if w := journalCall(h, "POST", journalPath, p, "original-key"); w.Code != 409 {
				t.Fatal("ineligible prepare", w.Code)
			}
			if _, e := st.PendingSubmission("fixture-project"); e != store.ErrNotFound {
				t.Fatal("rejected prepare persisted", e)
			}
		})
	}
}

func TestSubmissionJournalAPIRefusesGenericDraftWithoutInventingBudget(t *testing.T) {
	_, st, h := setup(t)
	p := preview(t, h)
	d := store.SubmissionDraft{PreviewID: p.ID, Key: "original-key", Request: store.CreateRequest{ProjectID: "fixture-project", Goal: "fixture-goal", Plan: p.Plan}, ExpiresAt: p.ExpiresAt, ConfigurationRevision: p.ConfigurationRevision}
	if _, e := st.PrepareSubmission(d, store.DefaultStamp{}); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct{ method, path string }{{"GET", journalPath}, {"POST", journalPath}, {"POST", journalPath + "/abandon"}} {
		if w := journalCall(h, tc.method, tc.path, p, d.Key); w.Code != 409 {
			t.Fatal("generic draft reinterpreted", w.Code)
		}
	}
	if got, e := st.PendingSubmission("fixture-project"); e != nil || got.State != "prepared" || got.Draft.Request.Budget != nil {
		t.Fatal("generic draft changed", e)
	}
}

func TestSubmissionJournalAPILateAuthorityDoesNotResolveOrDisclose(t *testing.T) {
	for _, mode := range []string{"revoke", "cancel"} {
		for _, action := range []string{"prepare", "abandon", "acknowledge"} {
			t.Run(mode+"/"+action, func(t *testing.T) {
				s, st, h := setup(t)
				p := preview(t, h)
				key := "fixture-journal-key"
				path := journalPath
				if action != "prepare" {
					readJournalFixture(t, journalCall(h, "POST", path, p, key))
					path += "/" + action
				}
				if action == "acknowledge" {
					if w := submit(t, h, p, key); w.Code != 201 {
						t.Fatal(w.Code)
					}
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				entered, done := make(chan struct{}), make(chan struct{})
				r := httptest.NewRequest("POST", path, &submissionReadSignal{Reader: strings.NewReader(journalBody(p)), entered: entered}).WithContext(ctx)
				r.Header.Set("Authorization", "Bearer fixture-management")
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Idempotency-Key", key)
				w := httptest.NewRecorder()
				s.mu.Lock()
				go func() { defer close(done); h.ServeHTTP(w, r) }()
				select {
				case <-entered:
				case <-time.After(time.Second):
					s.mu.Unlock()
					t.Fatal("body not read")
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
					t.Fatal("request blocked")
				}
				if w.Code != 401 || strings.Contains(w.Body.String(), key) || strings.Contains(w.Body.String(), "fixture-goal") {
					t.Fatal("late authority response", w.Code)
				}
				got, e := st.PendingSubmission("fixture-project")
				if action == "prepare" {
					if e != store.ErrNotFound {
						t.Fatal("revoked prepare mutated", e)
					}
				} else if e != nil || got.State != map[string]string{"abandon": "prepared", "acknowledge": "committed"}[action] {
					t.Fatal("revoked resolution mutated", e)
				}
			})
		}
	}
}

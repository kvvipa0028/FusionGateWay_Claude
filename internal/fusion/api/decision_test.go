package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/fusion/store"
)

func TestHumanDecisionManagementResourceStrictWithoutGrantOrRuntime(t *testing.T) {
	_, st, h := setup(t)
	p := preview(t, h)
	created := submit(t, h, p, "human-decision-fixture")
	var task store.Task
	if json.Unmarshal(created.Body.Bytes(), &task) != nil || task.ID == "" {
		t.Fatal(created.Body.String())
	}
	path := "/control/v1/tasks/" + task.ID + "/workflow/decision"
	for _, tc := range []struct {
		method, path, body, tag string
		want                    int
	}{
		{"GET", path, "", "", 200}, {"GET", path, "{}", "", 400}, {"DELETE", path, "", "", 405},
		{"POST", path, "{}", "", 428}, {"POST", path, "{}", taskETag(task), 400},
		{"POST", path, `{"action":"accept","reason":"yes","passed":true}`, taskETag(task), 400},
		{"POST", path, `{"Action":"accept","reason":"yes"}`, taskETag(task), 400},
		{"POST", path + "/extra", "{}", taskETag(task), 400},
	} {
		t.Run(tc.method+tc.path+tc.body, func(t *testing.T) {
			before, _ := st.Events(task.ID, 0)
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			r.Header.Set("Authorization", "Bearer fixture-management")
			r.Header.Set("Content-Type", "application/json")
			if tc.tag != "" {
				r.Header.Set("If-Match", tc.tag)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatal(w.Code, w.Body.String())
			}
			after, _ := st.Events(task.ID, 0)
			if len(after) != len(before) {
				t.Fatal("invalid/draft read wrote event")
			}
		})
	}
}

func humanRequestFixture() string {
	raw, _ := json.Marshal(HumanDecisionRequest{RunID: "fixture-final", TextHash: strings.Repeat("a", 64), TreeHash: strings.Repeat("b", 64), SpecHash: strings.Repeat("c", 64), DesignHash: strings.Repeat("d", 64), AcceptanceHash: strings.Repeat("e", 64), Action: "accept", Reason: "explicit human intent"})
	return string(raw)
}
func TestHumanDecisionStrictIdentityAndAuthority(t *testing.T) {
	s, st, h := setup(t)
	created := submit(t, h, preview(t, h), "strict-human")
	var task store.Task
	_ = json.Unmarshal(created.Body.Bytes(), &task)
	path := "/control/v1/tasks/" + task.ID + "/workflow/decision"
	valid := humanRequestFixture()
	for _, tc := range []struct {
		method, path, body, credential, key string
		want                                int
	}{
		{"GET", path, "", "", "", 401}, {"GET", path, "", "fgs_fixture", "", 403},
		{"GET", "/control/v1/tasks/missing/workflow/decision", "", "fgs_fixture", "", 403},
		{"GET", path + "?passed=true", "", "fixture-management", "", 400},
		{"GET", path, "", "fixture-management", "forbidden", 400},
		{"POST", path, valid, "fixture-management", "", 409},
		{"POST", path, strings.Replace(valid, `"reason":"explicit human intent"`, `"reason":null`, 1), "fixture-management", "", 400},
		{"POST", path, strings.Replace(valid, `"reason":"explicit human intent"`, `"reason":" "`, 1), "fixture-management", "", 400},
		{"POST", path, strings.Replace(valid, `"action":`, `"Action":`, 1), "fixture-management", "", 400},
		{"POST", path, strings.Replace(valid, `"reason":"explicit human intent"`, `"reason":"one","reason":"two"`, 1), "fixture-management", "", 400},
		{"POST", path, strings.TrimSuffix(valid, "}") + `,"spec":{}}`, "fixture-management", "", 400},
		{"POST", path, strings.Replace(valid, `"run_id":"fixture-final"`, `"run_id":null`, 1), "fixture-management", "", 400},
	} {
		t.Run(tc.method+tc.body+tc.credential+tc.path+tc.key, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("If-Match", taskETag(task))
			if tc.credential != "" {
				r.Header.Set("Authorization", "Bearer "+tc.credential)
			}
			if tc.key != "" {
				r.Header.Set("Idempotency-Key", tc.key)
			}
			before, _ := st.Events(task.ID, 0)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatal(w.Code, w.Body.String())
			}
			after, _ := st.Events(task.ID, 0)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("request granted authority or wrote events")
			}
		})
	}
	if s.SetFinalEvidenceReader(nil) == nil {
		t.Fatal("nil trusted reader admitted")
	}
	reader := func(context.Context, store.Task, store.StageRun) (store.FinalEvidence, func() bool, error) {
		t.Fatal("draft task invoked evidence reader")
		return store.FinalEvidence{}, nil, store.ErrInvalid
	}
	if err := s.SetFinalEvidenceReader(reader); err != nil {
		t.Fatal(err)
	}
	if s.SetFinalEvidenceReader(reader) != store.ErrConflict {
		t.Fatal("trusted reader replaced")
	}
	w := request(h, "GET", path, "", "", "fixture-management")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
}
func TestHumanDecisionRechecksManagementAfterWaiting(t *testing.T) {
	for _, method := range []string{"GET", "POST"} {
		for _, loss := range []string{"cancel", "revoke"} {
			t.Run(method+loss, func(t *testing.T) {
				s, st, h := setup(t)
				created := submit(t, h, preview(t, h), "late-human")
				var task store.Task
				_ = json.Unmarshal(created.Body.Bytes(), &task)
				path := "/control/v1/tasks/" + task.ID + "/workflow/decision"
				body := ""
				if method == "POST" {
					body = humanRequestFixture()
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				entered := make(chan struct{})
				wrapped := s.auth.Management(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); s.decisionControl(w, r) }))
				r := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx)
				r.Header.Set("Authorization", "Bearer fixture-management")
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("If-Match", taskETag(task))
				before, _ := st.Events(task.ID, 0)
				s.mu.Lock()
				done := make(chan *httptest.ResponseRecorder, 1)
				go func() { w := httptest.NewRecorder(); wrapped.ServeHTTP(w, r); done <- w }()
				<-entered
				if loss == "cancel" {
					cancel()
				} else {
					s.auth.RevokeManagement()
				}
				s.mu.Unlock()
				w := <-done
				if w.Code != 401 || strings.Contains(w.Body.String(), task.Goal) {
					t.Fatal("late revoked control disclosed or wrote", w.Code)
				}
				after, _ := st.Events(task.ID, 0)
				if !reflect.DeepEqual(before, after) {
					t.Fatal("late revoked control changed events")
				}
			})
		}
	}
}

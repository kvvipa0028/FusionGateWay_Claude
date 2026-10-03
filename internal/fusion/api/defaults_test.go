package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/quota"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

const globalDefaultsPath = "/control/v1/defaults/global"
const projectDefaultsPath = "/control/v1/projects/fixture-project/defaults"
const defaultsA = `{"layer":{"roles":{"design":{"mode":"locked","route":{"id":"fixture-route","revision":1},"model":"fixture-model-a","effort":{"mode":"default"}}}}}`
const defaultsB = `{"layer":{"roles":{"design":{"mode":"locked","route":{"id":"fixture-route-b","revision":1},"model":"fixture-model-b","effort":{"mode":"none"}}}}}`

func TestDefaultsAPIPeerCommittedReceiptReadsOriginalBeforeCurrentDefaults(t *testing.T) {
	s, st, h := presetSetup(t)
	old := preview(t, h)
	peer, e := New(st, policy.NewManager("fixture-management", nil, nil))
	if e != nil {
		t.Fatal(e)
	}
	s.mu.Lock()
	c := s.projects["fixture-project"].configuration
	s.mu.Unlock()
	if e = peer.SetProject("fixture-project", c); e != nil {
		t.Fatal(e)
	}
	peerPreview := preview(t, peer.Handler())
	w := submit(t, peer.Handler(), peerPreview, "fixture-peer-committed")
	var task store.Task
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &task) != nil {
		t.Fatal(w.Code)
	}
	saveDefaults(t, peer.Handler(), projectDefaultsPath, defaultsB, `"0"`)
	w = submit(t, h, old, "fixture-peer-committed")
	var replay store.Task
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &replay) != nil || replay.ID != task.ID {
		t.Fatal("existing durable receipt blocked by new defaults", w.Code)
	}
	if w = submit(t, h, old, "fixture-other-key"); w.Code != 409 {
		t.Fatal("receipt rebound key", w.Code)
	}
	events, e := st.Events(task.ID, 0)
	if e != nil || len(events) != 1 {
		t.Fatal("replay recreated task/event", e)
	}
}

func saveDefaults(t *testing.T, h http.Handler, path, body, tag string) store.DefaultLayerReceipt {
	t.Helper()
	w := revisionRequest(h, "PUT", path, body, tag)
	var v store.DefaultLayerReceipt
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &v) != nil || !v.Created {
		t.Fatal("default save failed", w.Code, w.Body.String())
	}
	return v
}
func TestDefaultsAPIChangesNewPreviewsWithoutRewritingCommittedTasks(t *testing.T) {
	_, st, h := presetSetup(t)
	first := preview(t, h)
	w := submit(t, h, first, "fixture-defaults-task")
	var task store.Task
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &task) != nil {
		t.Fatal(w.Code)
	}
	saveDefaults(t, h, projectDefaultsPath, defaultsB, `"0"`)
	p := preview(t, h)
	b := p.Plan.Bindings[stageplan.Design]
	if b.Source != "project" || b.Target.ResolvedModel != "fixture-model-b" || b.Target.Effort.Value != nil {
		t.Fatal("project binding mixed fields or not applied")
	}
	if w = submit(t, h, first, "different-key"); w.Code != 409 {
		t.Fatal("old preview reinterpreted", w.Code)
	}
	if w = submit(t, h, first, "fixture-defaults-task"); w.Code != 201 {
		t.Fatal("committed receipt changed", w.Code)
	}
	old, e := st.Plan(task.ID, 1)
	if e != nil || old.Hash != first.Plan.Hash || old.Bindings[stageplan.Design].Target.ResolvedModel != "fixture-model-a" {
		t.Fatal("history rewritten", e)
	}
	saveDefaults(t, h, projectDefaultsPath, `{"layer":{}}`, `"1"`)
	inherited := preview(t, h)
	if inherited.Plan.Bindings[stageplan.Design].Source != "global" || inherited.Plan.Bindings[stageplan.Design].Target.ResolvedModel != "fixture-model-a" {
		t.Fatal("project inherit failed")
	}
	saveDefaults(t, h, globalDefaultsPath, defaultsB, `"0"`)
	if w = submit(t, h, inherited, "fixture-stale"); w.Code != 409 {
		t.Fatal("global change accepted stale preview", w.Code)
	}
	if next := preview(t, h); next.Plan.Bindings[stageplan.Design].Target.ResolvedModel != "fixture-model-b" {
		t.Fatal("global defaults not applied")
	}
}
func TestDefaultsAPIPeerWritesAndServerRestartLoadPersistentLayers(t *testing.T) {
	s, st, h := presetSetup(t)
	old := preview(t, h)
	peer, e := New(st, policy.NewManager("fixture-management", nil, nil))
	if e != nil {
		t.Fatal(e)
	}
	s.mu.Lock()
	c := s.projects["fixture-project"].configuration
	s.mu.Unlock()
	if e = peer.SetProject("fixture-project", c); e != nil {
		t.Fatal(e)
	}
	saveDefaults(t, peer.Handler(), projectDefaultsPath, defaultsB, `"0"`)
	if w := submit(t, h, old, "fixture-peer-stale"); w.Code != 409 {
		t.Fatal("peer write accepted stale preview", w.Code)
	}
	if p := preview(t, h); p.Plan.Bindings[stageplan.Design].Target.ResolvedModel != "fixture-model-b" {
		t.Fatal("live projection ignored Store change")
	}
	restarted, e := New(st, policy.NewManager("fixture-management", nil, nil))
	if e != nil {
		t.Fatal(e)
	}
	c.Project = stageplan.Layer{}
	if e = restarted.SetProject("fixture-project", c); e != nil {
		t.Fatal(e)
	}
	if p := preview(t, restarted.Handler()); p.Plan.Bindings[stageplan.Design].Target.ResolvedModel != "fixture-model-b" {
		t.Fatal("bootstrap overwrote persisted default")
	}
}
func TestDefaultsAPIRejectsUnsafeWritesAndKeepsAbsenceExplicit(t *testing.T) {
	_, _, h := presetSetup(t)
	w := request(h, "GET", globalDefaultsPath, "", "", "fixture-management")
	var absent DefaultLayerView
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &absent) != nil || absent.Revision != 0 || absent.Layer != nil || absent.Configured {
		t.Fatal("fake saved default", w.Code)
	}
	for _, body := range []string{`{}`, `{"layer":null}`, `{"layer":{},"routes":[]}`, `{"layer":{},"token":"fixture-secret"}`, `{"layer":{},"account":"fixture-account"}`, `{"layer":{},"budget":{"max_calls":1000}}`, strings.Replace(defaultsA, "fixture-model-a", "unknown-model", 1), strings.Replace(defaultsA, "fixture-route", "unknown-route", 1)} {
		if w = revisionRequest(h, "PUT", globalDefaultsPath, body, `"0"`); w.Code != 400 {
			t.Fatal("unsafe default input", w.Code)
		}
	}
	if w = revisionRequest(h, "PUT", globalDefaultsPath, defaultsA); w.Code != 428 {
		t.Fatal("missing strong condition", w.Code)
	}
	for _, tag := range []string{`W/"0"`, `"00"`, `*`, `"-1"`, `"9223372036854775807"`} {
		if w = revisionRequest(h, "PUT", globalDefaultsPath, defaultsA, tag); w.Code != 400 {
			t.Fatal("bad version", w.Code)
		}
	}
	if w = revisionRequest(h, "PUT", globalDefaultsPath, defaultsA, `"0"`, `"0"`); w.Code != 400 {
		t.Fatal("duplicate version", w.Code)
	}
	for _, v := range []struct {
		auth string
		code int
	}{{"", 401}, {"bad", 401}, {"fgs_fixture", 403}} {
		if w = executionRequest(h, "PUT", globalDefaultsPath, defaultsA, "", `"0"`, v.auth); w.Code != v.code {
			t.Fatal("management boundary", w.Code)
		}
	}
	if w = revisionRequest(h, "PUT", strings.Replace(projectDefaultsPath, "fixture-project", "unknown-project", 1), defaultsA, `"0"`); w.Code != 404 {
		t.Fatal("unknown project save", w.Code)
	}
}
func TestDefaultsAPIGlobalAmbiguityAndHistoricalRetryCannotReplaceHead(t *testing.T) {
	s, st, h := presetSetup(t)
	s.mu.Lock()
	c := s.projects["fixture-project"].configuration
	s.mu.Unlock()
	raw, _ := json.Marshal(c)
	var other ProjectConfiguration
	json.Unmarshal(raw, &other)
	other.Routes[0].Account = "other-account"
	if e := s.SetProject("other-project", other); e != nil {
		t.Fatal(e)
	}
	if w := revisionRequest(h, "PUT", globalDefaultsPath, defaultsA, `"0"`); w.Code != 400 {
		t.Fatal("global route ref aliased accounts", w.Code)
	}
	first := saveDefaults(t, h, globalDefaultsPath, defaultsB, `"0"`)
	saveDefaults(t, h, globalDefaultsPath, `{"layer":{}}`, `"1"`)
	c.Routes = c.Routes[:1]
	other.Routes = other.Routes[:1]
	if e := s.SetProject("fixture-project", c); e != nil {
		t.Fatal(e)
	}
	if e := s.SetProject("other-project", other); e != nil {
		t.Fatal(e)
	}
	w := revisionRequest(h, "PUT", globalDefaultsPath, defaultsB, `"0"`)
	var replay store.DefaultLayerReceipt
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &replay) != nil || replay.Created || replay.DefaultLayer.Hash != first.DefaultLayer.Hash {
		t.Fatal("history retry required current route or rewrote head", w.Code)
	}
	latest, e := st.DefaultLayer("global", "global", 0)
	if e != nil || latest.Revision != 2 || latest.Layer.Roles[stageplan.Design].Mode != stageplan.Inherit {
		t.Fatal("old retry rolled back", e)
	}
	if w = request(h, "GET", globalDefaultsPath+"/versions/1", "", "", "fixture-management"); w.Code != 200 || w.Header().Get("ETag") != `"1"` {
		t.Fatal("history unreadable", w.Code)
	}
	if w = revisionRequest(h, "PUT", globalDefaultsPath, defaultsB, `"2"`); w.Code != 400 {
		t.Fatal("new save used removed route", w.Code)
	}
	body := `{"project_id":"fixture-project","goal":"fixture","required_roles":["design"]}`
	if w = request(h, "POST", "/control/v1/tasks/preview", body, "", "fixture-management"); w.Code != 422 {
		t.Fatal("explicit inherit global silently used bootstrap", w.Code)
	}
}
func TestDefaultsAPIRegisteredDraftDoesNotGrantAdmission(t *testing.T) {
	s, _, h := presetSetup(t)
	s.mu.Lock()
	c := s.projects["fixture-project"].configuration
	s.mu.Unlock()
	c.Routes[1].Admitted = false
	if e := s.SetProject("fixture-project", c); e != nil {
		t.Fatal(e)
	}
	saveDefaults(t, h, projectDefaultsPath, defaultsB, `"0"`)
	if w := request(h, "POST", "/control/v1/tasks/preview", `{"project_id":"fixture-project","goal":"fixture","required_roles":["design"]}`, "", "fixture-management"); w.Code != 422 {
		t.Fatal("draft became admitted", w.Code)
	}
}
func TestDefaultsAPIQuotaRegistrationRetainsSourceAgeUntilTrustedRouteChanges(t *testing.T) {
	f := setupQuotaAPI(t)
	w := request(f.h, "POST", quotaRefreshPath, `{}`, "", "fixture-management")
	before := quotaDecode(t, w.Body.Bytes())
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	saveDefaults(t, f.h, projectDefaultsPath, `{"layer":{}}`, `"0"`)
	if e := f.s.SetQuotaSources("fixture-project", []QuotaSource{f.source}); !errors.Is(e, store.ErrConflict) {
		t.Fatal("default change allowed source hot replacement", e)
	}
	w = request(f.h, "GET", quotaPath, "", "", "fixture-management")
	after := quotaDecode(t, w.Body.Bytes())
	if w.Code != 200 || after.ConfigurationRevision <= before.ConfigurationRevision || after.Routes[0].Status != quota.Available || !after.Routes[0].Snapshot.ObservedAt.Equal(*before.Routes[0].Snapshot.ObservedAt) || f.calls.Load() != 1 {
		t.Fatal("default selection invalidated/refreshed source", w.Code)
	}
	if e := f.s.SetProject("fixture-project", configuration()); e != nil {
		t.Fatal(e)
	}
	w = request(f.h, "GET", quotaPath, "", "", "fixture-management")
	after = quotaDecode(t, w.Body.Bytes())
	if after.Routes[0].Status != quota.Unverified || f.calls.Load() != 1 {
		t.Fatal("trusted route change kept old authority")
	}
}
func TestDefaultsAPIRevisionReceiptsCheckCurrentDefaultsAndCommittedApplyStaysFrozen(t *testing.T) {
	s, st, h, task := revisionSetup(t)
	p := revisionPreview(t, h, task.ID)
	saveDefaults(t, h, projectDefaultsPath, `{"layer":{}}`, `"0"`)
	if w := applyRevision(h, task.ID, p, `"1"`); w.Code != 409 {
		t.Fatal("stale revision preview applied", w.Code)
	}
	p = revisionPreview(t, h, task.ID)
	if w := applyRevision(h, task.ID, p, `"1"`); w.Code != 200 {
		t.Fatal(w.Code)
	}
	saveDefaults(t, h, projectDefaultsPath, `{"layer":{}}`, `"1"`)
	if w := applyRevision(h, task.ID, p, `"1"`); w.Code != 200 {
		t.Fatal("committed revision not readable", w.Code)
	}
	got, e := st.Plan(task.ID, 2)
	if e != nil || got.Hash != p.Plan.Hash {
		t.Fatal("committed revision followed defaults", e)
	}
	// Use a real HTTP connection for saved revision and its escaped Location.
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	req, _ := http.NewRequest("GET", ts.URL+projectDefaultsPath+"/versions/1", nil)
	req.Header.Set("Authorization", "Bearer fixture-management")
	resp, e := ts.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("ETag") != `"1"` {
		t.Fatal("history HTTP mismatch", resp.StatusCode)
	}
}

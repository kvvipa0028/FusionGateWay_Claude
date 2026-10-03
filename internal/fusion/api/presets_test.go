package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/fusion/quota"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

const presetsPath = "/control/v1/projects/fixture-project/presets"
const presetPath = presetsPath + "/fixture-preset"
const firstPresetBody = `{"name":"工程预设","layer":{"groups":{"design_planning":{"mode":"locked","route":{"id":"fixture-route","revision":1},"model":"fixture-model-a","effort":{"mode":"default"}}}}}`
const secondPresetBody = `{"name":"修订预设","layer":{"groups":{"design_planning":{"mode":"locked","route":{"id":"fixture-route-b","revision":1},"model":"fixture-model-b","effort":{"mode":"none"}}}}}`

func presetSetup(t *testing.T) (*Server, *store.Store, http.Handler) {
	t.Helper()
	s, st, h := setup(t)
	c := configuration()
	b := c.Routes[0]
	b.ID = "fixture-route-b"
	b.Model = "fixture-model-b"
	b.NoEffort = true
	b.Efforts = nil
	b.DefaultEffort = nil
	c.Routes = append(c.Routes, b)
	if e := s.SetProject("fixture-project", c); e != nil {
		t.Fatal(e)
	}
	return s, st, h
}
func saveAPIPreset(t *testing.T, h http.Handler, body, tag string) store.PresetReceipt {
	t.Helper()
	w := revisionRequest(h, "PUT", presetPath, body, tag)
	var v store.PresetReceipt
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &v) != nil || !v.Created {
		t.Fatal("preset save failed", w.Code, w.Body.String())
	}
	return v
}
func previewPreset(t *testing.T, h http.Handler, revision int) Preview {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"project_id": "fixture-project", "goal": "fixture-goal", "required_roles": []string{"design"}, "preset": map[string]any{"id": "fixture-preset", "revision": revision}})
	w := request(h, "POST", "/control/v1/tasks/preview", string(body), "", "fixture-management")
	var p Preview
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil {
		t.Fatal("preset preview failed", w.Code, w.Body.String())
	}
	return p
}
func TestPresetAPIExplicitVersionIsFrozenDespitePresetAndGlobalChanges(t *testing.T) {
	s, st, h := presetSetup(t)
	first := saveAPIPreset(t, h, firstPresetBody, `"0"`)
	p := previewPreset(t, h, 1)
	second := saveAPIPreset(t, h, secondPresetBody, `"1"`)
	if first.Preset.Revision != 1 || second.Preset.Revision != 2 || p.Preset == nil || p.Preset.Hash != first.Preset.Hash {
		t.Fatal("preview omitted exact preset identity")
	}
	w := submit(t, h, p, "fixture-preset-task")
	var task store.Task
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &task) != nil {
		t.Fatal("saving same-name preset invalidated frozen preview", w.Code)
	}
	plan, e := st.Plan(task.ID, 1)
	if e != nil || plan.Hash != p.Plan.Hash || plan.Bindings[stageplan.Design].Target.ResolvedModel != "fixture-model-a" {
		t.Fatal("task silently adopted latest", e)
	}
	ref, e := st.TaskPreset(task.ID)
	if e != nil || ref == nil || ref.Hash != first.Preset.Hash || ref.Revision != 1 {
		t.Fatal("task provenance followed latest", e)
	}
	next := previewPreset(t, h, 2)
	if next.Plan.Bindings[stageplan.Design].Target.ResolvedModel != "fixture-model-b" || next.Plan.Bindings[stageplan.Design].Target.Effort.Value != nil {
		t.Fatal("version2 mixed model/effort")
	}
	if e = s.SetProject("fixture-project", configuration()); e != nil {
		t.Fatal(e)
	}
	if w = submit(t, h, next, "fixture-stale-preset-task"); w.Code != 409 {
		t.Fatal("configuration change accepted stale preview", w.Code)
	}
	if w = submit(t, h, p, "fixture-preset-task"); w.Code != 201 {
		t.Fatal("existing receipt adopted current defaults", w.Code)
	}
	plan, _ = st.Plan(task.ID, 1)
	if plan.Hash != p.Plan.Hash {
		t.Fatal("global default changed submitted snapshot")
	}
}
func TestPresetAPIBrowsingLatestAndOldPutReplayNeverChangeApplicationVersion(t *testing.T) {
	_, _, h := presetSetup(t)
	first := saveAPIPreset(t, h, firstPresetBody, `"0"`)
	saveAPIPreset(t, h, secondPresetBody, `"1"`)
	w := revisionRequest(h, "PUT", presetPath, firstPresetBody, `"0"`)
	var receipt store.PresetReceipt
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &receipt) != nil || receipt.Created || receipt.Preset.Hash != first.Preset.Hash || w.Header().Get("ETag") != `"1"` {
		t.Fatal("old retry wrote or followed latest", w.Code)
	}
	w = request(h, "GET", presetPath, "", "", "fixture-management")
	var latest store.Preset
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &latest) != nil || latest.Revision != 2 || w.Header().Get("ETag") != `"2"` {
		t.Fatal("latest browsing unavailable", w.Code)
	}
	for _, rev := range []any{0, "latest", -1} {
		body, _ := json.Marshal(map[string]any{"project_id": "fixture-project", "goal": "fixture", "required_roles": []string{"design"}, "preset": map[string]any{"id": "fixture-preset", "revision": rev}})
		if w = request(h, "POST", "/control/v1/tasks/preview", string(body), "", "fixture-management"); w.Code != 400 {
			t.Fatal("task used ambiguous preset version", w.Code)
		}
	}
	if w = revisionRequest(h, "PUT", presetPath, secondPresetBody, `"0"`); w.Code != 412 {
		t.Fatal("stale version replaced original", w.Code)
	}
}
func TestPresetAPIRejectsUnsafeWritesAndMalformedPreconditions(t *testing.T) {
	_, st, h := presetSetup(t)
	for _, tag := range []string{`W/"0"`, `"00"`, `"-1"`, `*`, `"0","1"`, `"9223372036854775807"`} {
		if w := revisionRequest(h, "PUT", presetPath, firstPresetBody, tag); w.Code != 400 {
			t.Fatal("unsafe If-Match accepted", tag, w.Code)
		}
	}
	if w := revisionRequest(h, "PUT", presetPath, firstPresetBody); w.Code != 428 {
		t.Fatal("missing precondition accepted", w.Code)
	}
	if w := revisionRequest(h, "PUT", presetPath, firstPresetBody, `"0"`, `"0"`); w.Code != 400 {
		t.Fatal("duplicate condition accepted", w.Code)
	}
	for _, body := range []string{`{"name":"x","layer":{},"token":"fixture-secret"}`, `{"name":"x","layer":{},"workspace":"/tmp"}`, `{"name":"x","layer":{"roles":{"design":{"mode":"inherit","admitted":true}}}}`, `{"name":"x","layer":{"roles":{"bad":{"mode":"inherit"}}}}`, `{"name":"x","layer":{"roles":{"design":{"mode":"inherit","model":"fixture-model-a"}}}}`, `{"name":"","layer":{}}`, `{"name":"x","name":"y","layer":{}}`} {
		if w := revisionRequest(h, "PUT", presetPath, body, `"0"`); w.Code != 400 {
			t.Fatal("unsafe preset DTO accepted", w.Code)
		}
	}
	list, e := st.Presets("fixture-project")
	if e != nil || len(list) != 0 {
		t.Fatal("invalid write left preset history", e)
	}
}
func TestPresetAPIScopeAndAuthCannotReadOrCreateAnotherProject(t *testing.T) {
	s, _, h := presetSetup(t)
	saveAPIPreset(t, h, firstPresetBody, `"0"`)
	if e := s.SetProject("other-project", configuration()); e != nil {
		t.Fatal(e)
	}
	for _, x := range []struct {
		credential string
		code       int
	}{{"", 401}, {"bad", 401}, {"fgs_fixture", 403}} {
		w := executionRequest(h, "PUT", presetPath, firstPresetBody, "", `"0"`, x.credential)
		if w.Code != x.code {
			t.Fatal("scope boundary changed", w.Code)
		}
	}
	for _, path := range []string{strings.Replace(presetPath, "fixture-project", "other-project", 1) + "/versions/1", strings.Replace(presetsPath, "fixture-project", "missing", 1)} {
		if w := request(h, "GET", path, "", "", "fixture-management"); w.Code != 404 {
			t.Fatal("cross-project preset read", w.Code)
		}
	}
	body := `{"project_id":"other-project","goal":"fixture","required_roles":["design"],"preset":{"id":"fixture-preset","revision":1}}`
	if w := request(h, "POST", "/control/v1/tasks/preview", body, "", "fixture-management"); w.Code != 404 {
		t.Fatal("cross-project preset applied", w.Code)
	}
}
func TestPresetAPIIndependentFiveRolesAndTaskWholeBindingOverrides(t *testing.T) {
	s, _, h := presetSetup(t)
	c := configuration()
	b := c.Routes[0]
	b.ID = "fixture-route-b"
	b.Model = "fixture-model-b"
	b.NoEffort = true
	b.Efforts = nil
	b.DefaultEffort = nil
	c.Routes = append(c.Routes, b)
	a := c.Global.Roles[stageplan.Design]
	c.Global.Groups = map[stageplan.Group]stageplan.Binding{stageplan.ImplementationTesting: a, stageplan.ReviewAcceptance: a}
	binding := stageplan.Binding{Mode: stageplan.Locked, Route: &stageplan.RouteRef{ID: b.ID, Revision: 1}, Model: b.Model, Effort: &stageplan.EffortSelection{Mode: stageplan.EffortNone}}
	c.Project.Roles = map[stageplan.Role]stageplan.Binding{stageplan.Testing: binding}
	if e := s.SetProject("fixture-project", c); e != nil {
		t.Fatal(e)
	}
	body, _ := json.Marshal(store.PresetInput{Name: "five roles", Layer: stageplan.Layer{Groups: map[stageplan.Group]stageplan.Binding{stageplan.DesignPlanning: a, stageplan.ImplementationTesting: a, stageplan.ReviewAcceptance: a}, Roles: map[stageplan.Role]stageplan.Binding{stageplan.Acceptance: binding}}})
	saved := saveAPIPreset(t, h, string(body), `"0"`)
	if saved.Preset.Layer.Roles[stageplan.Testing].Model != a.Model || saved.Preset.Layer.Roles[stageplan.Acceptance].Model != b.Model {
		t.Fatal("saving collapsed independent roles")
	}
	in := PreviewRequest{ProjectID: "fixture-project", Goal: "fixture", RequiredRoles: stageplan.AllRoles(), Preset: &PresetSelection{ID: "fixture-preset", Revision: 1}, Task: stageplan.Layer{Groups: map[stageplan.Group]stageplan.Binding{stageplan.ImplementationTesting: binding}, Roles: map[stageplan.Role]stageplan.Binding{stageplan.Testing: {Mode: stageplan.Inherit}}}}
	raw, _ := json.Marshal(in)
	w := request(h, "POST", "/control/v1/tasks/preview", string(raw), "", "fixture-management")
	var p Preview
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil {
		t.Fatal("five-role preview failed", w.Code)
	}
	if p.Plan.Bindings[stageplan.Testing].Source != "project" || p.Plan.Bindings[stageplan.Implementation].Source != "task" || p.Plan.Bindings[stageplan.Implementation].Target.Effort.Value != nil || p.Plan.Bindings[stageplan.Acceptance].Target.ResolvedModel != b.Model {
		t.Fatal("preset/task merge mixed fields or ignored inherit")
	}
}
func TestPresetAPIUnavailableRouteCanBeSavedButNeverAdmittedByPreset(t *testing.T) {
	s, _, h := presetSetup(t)
	c := configuration()
	c.Routes[0].Admitted = false
	if e := s.SetProject("fixture-project", c); e != nil {
		t.Fatal(e)
	}
	saveAPIPreset(t, h, firstPresetBody, `"0"`)
	if w := request(h, "POST", "/control/v1/tasks/preview", `{"project_id":"fixture-project","goal":"fixture","required_roles":["design"],"preset":{"id":"fixture-preset","revision":1}}`, "", "fixture-management"); w.Code != 422 {
		t.Fatal("preset promoted an unverified route", w.Code)
	}
}
func TestPresetAPIActualHTTPWriteAndVersionRead(t *testing.T) {
	_, _, h := presetSetup(t)
	server := httptest.NewServer(h)
	defer server.Close()
	r, _ := http.NewRequest("PUT", server.URL+presetPath, strings.NewReader(firstPresetBody))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer fixture-management")
	r.Header.Set("If-Match", `"0"`)
	res, e := server.Client().Do(r)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 201 || res.Header.Get("Location") != presetPath+"/versions/1" {
		t.Fatal("actual HTTP save failed", res.StatusCode)
	}
	r, _ = http.NewRequest("GET", server.URL+res.Header.Get("Location"), nil)
	r.Header.Set("Authorization", "Bearer fixture-management")
	res, e = server.Client().Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	var p store.Preset
	if e = json.NewDecoder(res.Body).Decode(&p); e != nil || res.StatusCode != 200 || p.Revision != 1 || len(p.Layer.Roles) != 5 || res.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("actual version read failed", e)
	}
}

func TestPresetAPIConfigurationIsReadOnlyAndTaskProvenanceIsExact(t *testing.T) {
	_, _, h := presetSetup(t)
	w := request(h, "GET", "/control/v1/projects/fixture-project/configuration", "", "", "fixture-management")
	var config struct {
		Revision      int64                `json:"revision"`
		Configuration ProjectConfiguration `json:"configuration"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &config) != nil || config.Revision != 2 || len(config.Configuration.Routes) != 2 || w.Header().Get("ETag") != `"2"` {
		t.Fatal("trusted metadata unavailable", w.Code)
	}
	if w = request(h, "PUT", "/control/v1/projects/fixture-project/configuration", `{"routes":[]}`, "", "fixture-management"); w.Code != 405 {
		t.Fatal("caller rewrote route registry", w.Code)
	}
	saved := saveAPIPreset(t, h, firstPresetBody, `"0"`)
	p := previewPreset(t, h, 1)
	w = submit(t, h, p, "fixture-provenance")
	var task store.Task
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &task) != nil {
		t.Fatal(w.Code)
	}
	w = request(h, "GET", "/control/v1/tasks/"+task.ID+"/preset", "", "", "fixture-management")
	var got struct {
		Preset *store.PresetRef `json:"preset"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.Preset == nil || got.Preset.Hash != saved.Preset.Hash {
		t.Fatal("persisted provenance not readable", w.Code)
	}
}

func TestPresetAPILocationEscapesOpaqueIdentifiers(t *testing.T) {
	_, _, h := presetSetup(t)
	path := presetsPath + "/" + url.PathEscape("设计?label#one")
	w := revisionRequest(h, "PUT", path, firstPresetBody, `"0"`)
	if w.Code != 201 || w.Header().Get("Location") != path+"/versions/1" {
		t.Fatal("Location does not round-trip opaque ID", w.Code, w.Header().Get("Location"))
	}
	w = request(h, "GET", w.Header().Get("Location"), "", "", "fixture-management")
	if w.Code != 200 {
		t.Fatal("escaped version Location unreadable", w.Code)
	}
}

func TestPresetAPIProjectNameDoesNotCaptureQuotaResource(t *testing.T) {
	f := setupQuotaAPI(t)
	if e := f.s.SetProject("presets", configuration()); e != nil {
		t.Fatal(e)
	}
	if e := f.s.SetQuotaSources("presets", []QuotaSource{f.source}); e != nil {
		t.Fatal(e)
	}
	w := request(f.h, "GET", "/control/v1/projects/presets/quota", "", "", "fixture-management")
	v := quotaDecode(t, w.Body.Bytes())
	if w.Code != 200 || len(v.Routes) != 1 || v.Routes[0].Status != quota.Unknown || f.calls.Load() != 0 {
		t.Fatal("project identifier captured a different resource", w.Code)
	}
}

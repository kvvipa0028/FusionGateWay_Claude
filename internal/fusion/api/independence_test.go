package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

func TestProjectIndependenceTrustedPreviewFreezeAndNoTaskOverride(t *testing.T) {
	s, st, h := setup(t)
	c := configuration()
	c.Global.Roles[stageplan.Acceptance] = c.Global.Roles[stageplan.Design]
	c.Independence = []stageplan.RolePair{{First: stageplan.Acceptance, Second: stageplan.Design}}
	if e := s.SetProject("fixture-project", c); e != nil {
		t.Fatal(e)
	}
	body := `{"project_id":"fixture-project","goal":"fixture","required_roles":["design","acceptance"]}`
	w := request(h, "POST", "/control/v1/tasks/preview", body, "", "fixture-management")
	if w.Code != 422 || !strings.Contains(w.Body.String(), "independence_conflict") {
		t.Fatal("locked conflict not explicit", w.Code, w.Body.String())
	}
	for _, extra := range []string{`,"independence":[]`, `,"task":{"independence":[]}`} {
		w = request(h, "POST", "/control/v1/tasks/preview", strings.TrimSuffix(body, "}")+extra+"}", "", "fixture-management")
		if w.Code != 400 {
			t.Fatal("task override accepted", w.Code)
		}
	}
	b := c.Routes[0]
	b.ID = "other-route"
	b.Model = "other-model"
	c.Routes = append(c.Routes, b)
	bind := c.Global.Roles[stageplan.Acceptance]
	bind.Route = &stageplan.RouteRef{ID: b.ID, Revision: 1}
	bind.Model = b.Model
	c.Global.Roles[stageplan.Acceptance] = bind
	if e := s.SetProject("fixture-project", c); e != nil {
		t.Fatal(e)
	}
	c.Independence[0].First = stageplan.Review
	w = request(h, "POST", "/control/v1/tasks/preview", body, "", "fixture-management")
	var preview Preview
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &preview) != nil || len(preview.Plan.Independence) != 1 || preview.Plan.Independence[0].First != stageplan.Design {
		t.Fatal("trusted policy not copied/frozen", w.Code)
	}
	w = submit(t, h, preview, "independence-submit")
	var task store.Task
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &task) != nil {
		t.Fatal("submit", w.Code)
	}
	p, e := st.Plan(task.ID, 1)
	if e != nil || p.Hash != preview.Plan.Hash || len(p.Independence) != 1 {
		t.Fatal("submission lost hard rule", e)
	}
	status, result := controlErrorValue(fmt.Errorf("private details: %w", store.ErrIndependence))
	raw, _ := json.Marshal(result)
	if status != 409 || !strings.Contains(string(raw), "project_independence_conflict") || strings.Contains(string(raw), "private details") {
		t.Fatal("runtime conflict response", status)
	}
}

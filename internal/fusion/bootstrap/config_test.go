//go:build darwin || linux

package bootstrap

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/fusion/api"
	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
	"golang.org/x/sys/unix"
)

func sourceFixture(t *testing.T) (string, Document) {
	t.Helper()
	r, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	for _, n := range []string{"private", "private/config", "workspace"} {
		if e = os.Mkdir(filepath.Join(r, n), 0700); e != nil {
			t.Fatal(e)
		}
	}
	path := filepath.Join(r, "private/config/projects.json")
	d := Document{SchemaVersion: 1, Revision: 1, Routes: []RouteDeclaration{{ID: "fixture-glm", Revision: 1, NativeRoute: "glm-cn-claude", Model: "fixture-model", Account: "fixture-account", Workspace: "fixture-provider-workspace", CredentialIdentity: "fixture-identity", RuntimeVersion: "fixture-version", NoEffort: true}}, Projects: []ProjectDeclaration{{ID: "fixture-project", Name: "测试项目", Path: filepath.Join(r, "workspace"), Read: true, Write: false, Routes: []stageplan.RouteRef{{ID: "fixture-glm", Revision: 1}}, Layer: stageplan.Layer{Groups: map[stageplan.Group]stageplan.Binding{stageplan.DesignPlanning: {Mode: stageplan.Locked, Route: &stageplan.RouteRef{ID: "fixture-glm", Revision: 1}, Model: "fixture-model", Effort: &stageplan.EffortSelection{Mode: stageplan.EffortNone}}}}}}}
	writeSource(t, path, d)
	return path, d
}
func writeSource(t *testing.T, path string, d Document) {
	t.Helper()
	raw, e := json.Marshal(d)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
}

func TestProjectSourceDerivesOnlyUnadmittedMetadata(t *testing.T) {
	path, d := sourceFixture(t)
	source, e := Load(path)
	if e != nil {
		t.Fatal(e)
	}
	p, e := source.Project("fixture-project")
	if e != nil || p.Path != d.Projects[0].Path || !p.Read || p.Write || p.Configuration.DefaultBudget.MaxCalls != 50 || p.Configuration.DefaultBudget.MaxReworks != 1 {
		t.Fatal("project registration", e)
	}
	r := p.Configuration.Routes[0]
	if r.Admitted || r.BillingKnown || r.LockEnforcement != stageplan.Unverified || len(r.Capabilities) != 0 || r.BillingPath != "coding_plan" || r.Workspace == p.Path {
		t.Fatal("metadata granted authority or confused workspace")
	}
	if len(p.Configuration.Project.Roles) != 5 || p.Configuration.Project.Roles[stageplan.Design].Model != "fixture-model" {
		t.Fatal("group expansion")
	}
	if _, e = stageplan.Compile(1, []stageplan.Role{stageplan.Design}, p.Configuration.Global, p.Configuration.Project, stageplan.Layer{}, p.Configuration.Routes); !errors.Is(e, stageplan.ErrInvalidPlan) {
		t.Fatal("unverified registration admitted execution", e)
	}
	if !source.Current(p.ID) {
		t.Fatal("fresh source not current")
	}
}
func TestProjectSourceRejectsAuthorityAndAmbiguousJSON(t *testing.T) {
	path, _ := sourceFixture(t)
	valid, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{`null`, `[]`, `{}`, `{"schema_version":1,"Schema_Version":1}`, `{"schema_version":1,"ſchema_version":1}`, string(valid) + `{}`, strings.Replace(string(valid), `"revision":1`, `"revision":null`, 1), strings.Replace(string(valid), `"schema_version":1`, `"schema_version":2`, 1), strings.Replace(string(valid), `"model":"fixture-model"`, `"model":"fixture-model","admitted":true`, 1), strings.Replace(string(valid), `"model":"fixture-model"`, `"model":"fixture-model","api_key":"fixture-sensitive"`, 1), strings.Replace(string(valid), `"read":true`, `"read":true,"stopped_verified":true`, 1), strings.Replace(string(valid), `"write":false`, `"write":false,"argv":["fixture"]`, 1), strings.Replace(string(valid), `"native_route":"glm-cn-claude"`, `"native_route":"glm-cn-claude","url":"https://fixture.invalid"`, 1)} {
		if e = os.WriteFile(path, []byte(raw), 0600); e != nil {
			t.Fatal(e)
		}
		if _, e = Load(path); !errors.Is(e, ErrRegistration) {
			t.Fatal("unsafe JSON accepted", e)
		}
		if strings.Contains(fmt.Sprint(e), "fixture-sensitive") {
			t.Fatal("source echoed input")
		}
	}
	if e = os.WriteFile(path, append(valid, 0xff), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = Load(path); !errors.Is(e, ErrRegistration) {
		t.Fatal("invalid UTF8", e)
	}
}
func TestProjectSourceRejectsInvalidScopeReferencesAndBudget(t *testing.T) {
	for _, kind := range []string{"duplicate-project", "duplicate-route", "unknown-native", "unknown-ref", "model-mismatch", "no-read", "write-no-read", "budget", "budget-zero", "route-limit", "placeholder-model", "same-folder", "overlap", "source-overlap", "blank-name", "invalid-layer"} {
		t.Run(kind, func(t *testing.T) {
			path, d := sourceFixture(t)
			switch kind {
			case "duplicate-project":
				d.Projects = append(d.Projects, d.Projects[0])
			case "duplicate-route":
				d.Routes = append(d.Routes, d.Routes[0])
			case "unknown-native":
				d.Routes[0].NativeRoute = "fixture-unknown"
			case "unknown-ref":
				d.Projects[0].Routes[0].Revision++
			case "model-mismatch":
				b := d.Projects[0].Layer.Groups[stageplan.DesignPlanning]
				b.Model = "fixture-other"
				d.Projects[0].Layer.Groups[stageplan.DesignPlanning] = b
			case "no-read":
				d.Projects[0].Read = false
			case "write-no-read":
				d.Projects[0].Read = false
				d.Projects[0].Write = true
			case "budget":
				v := 1001
				d.Projects[0].MaxCalls = &v
			case "budget-zero":
				v := 0
				d.Projects[0].MaxCalls = &v
			case "placeholder-model":
				d.Routes[0].Model = "unknown"
			case "route-limit":
				for i := 0; i < MaxRoutes+1; i++ {
					r := d.Routes[0]
					r.ID = fmt.Sprint("fixture-route-", i)
					d.Routes = append(d.Routes, r)
				}
			case "same-folder":
				other := d.Projects[0]
				other.ID = "fixture-other"
				d.Projects = append(d.Projects, other)
			case "overlap":
				other := d.Projects[0]
				other.ID = "fixture-other"
				other.Path = filepath.Join(other.Path, "child")
				if e := os.Mkdir(other.Path, 0700); e != nil {
					t.Fatal(e)
				}
				d.Projects = append(d.Projects, other)
			case "source-overlap":
				d.Projects[0].Path = filepath.Dir(filepath.Dir(path))
			case "blank-name":
				d.Projects[0].Name = " "
			case "invalid-layer":
				d.Projects[0].Layer.Roles = map[stageplan.Role]stageplan.Binding{"fixture-role": {Mode: stageplan.Inherit}}
			}
			writeSource(t, path, d)
			if _, e := Load(path); !errors.Is(e, ErrRegistration) {
				t.Fatal("invalid registration", kind, e)
			}
		})
	}
}
func TestProjectSourcePrivateFileNoLinksNoGitNoBlockingSpecialFiles(t *testing.T) {
	for _, kind := range []string{"public-file", "public-parent", "linked-file", "linked-parent", "hardlink", "git-ancestor", "too-big", "wrong-name", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			path, _ := sourceFixture(t)
			switch kind {
			case "public-file":
				if e := os.Chmod(path, 0644); e != nil {
					t.Fatal(e)
				}
			case "public-parent":
				if e := os.Chmod(filepath.Dir(path), 0755); e != nil {
					t.Fatal(e)
				}
			case "linked-file":
				if e := os.Rename(path, path+".old"); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink(path+".old", path); e != nil {
					t.Fatal(e)
				}
			case "linked-parent":
				parent := filepath.Dir(path)
				if e := os.Rename(parent, parent+".old"); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink(parent+".old", parent); e != nil {
					t.Fatal(e)
				}
			case "hardlink":
				if e := os.Link(path, path+".copy"); e != nil {
					t.Fatal(e)
				}
			case "git-ancestor":
				if e := os.Mkdir(filepath.Join(filepath.Dir(filepath.Dir(path)), ".git"), 0700); e != nil {
					t.Fatal(e)
				}
			case "too-big":
				if e := os.WriteFile(path, []byte(strings.Repeat(" ", MaxSourceBytes+1)), 0600); e != nil {
					t.Fatal(e)
				}
			case "wrong-name":
				path = filepath.Join(filepath.Dir(path), "glm-coding-plan.key")
			case "fifo":
				if e := os.Remove(path); e != nil {
					t.Fatal(e)
				}
				if e := unix.Mkfifo(path, 0600); e != nil {
					t.Fatal(e)
				}
			}
			if _, e := Load(path); !errors.Is(e, ErrRegistration) {
				t.Fatal("unsafe private source", kind, e)
			}
		})
	}
}
func TestProjectSourceCurrentFencesRotationAndFolderReplacement(t *testing.T) {
	for _, kind := range []string{"source-rotation", "source-content", "folder-rotation", "folder-symlink", "folder-public-write"} {
		t.Run(kind, func(t *testing.T) {
			path, d := sourceFixture(t)
			source, e := Load(path)
			if e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "source-rotation":
				if e = os.Rename(path, path+".old"); e != nil {
					t.Fatal(e)
				}
				writeSource(t, path, d)
			case "source-content":
				d.Revision++
				d.Projects[0].Write = true
				writeSource(t, path, d)
			case "folder-rotation":
				p := d.Projects[0].Path
				if e = os.Rename(p, p+".old"); e != nil {
					t.Fatal(e)
				}
				if e = os.Mkdir(p, 0700); e != nil {
					t.Fatal(e)
				}
			case "folder-symlink":
				p := d.Projects[0].Path
				if e = os.Rename(p, p+".old"); e != nil {
					t.Fatal(e)
				}
				if e = os.Symlink(p+".old", p); e != nil {
					t.Fatal(e)
				}
			case "folder-public-write":
				if e = os.Chmod(d.Projects[0].Path, 0777); e != nil {
					t.Fatal(e)
				}
			}
			if source.Current("fixture-project") {
				t.Fatal("changed registration stayed authorized", kind)
			}
		})
	}
}
func TestProjectSourceCopiesAndRedactsPrivateRegistration(t *testing.T) {
	path, _ := sourceFixture(t)
	source, e := Load(path)
	if e != nil {
		t.Fatal(e)
	}
	p, e := source.Project("fixture-project")
	if e != nil {
		t.Fatal(e)
	}
	p.Read = false
	p.Write = true
	p.Configuration.Routes[0].Admitted = true
	p.Configuration.Project.Roles[stageplan.Design].Route.ID = "fixture-mutated"
	p.Configuration.DefaultBudget.MaxCalls = 999
	got, e := source.Project("fixture-project")
	if e != nil || !got.Read || got.Write || got.Configuration.Routes[0].Admitted || got.Configuration.Project.Roles[stageplan.Design].Route.ID != "fixture-glm" || got.Configuration.DefaultBudget.MaxCalls != 50 {
		t.Fatal("mutable registration leaked", e)
	}
	if _, e = source.Project("fixture-missing"); !errors.Is(e, ErrRegistration) {
		t.Fatal("missing project", e)
	}
	if source.Current("fixture-missing") {
		t.Fatal("missing grant current")
	}
	for _, s := range []string{fmt.Sprint(source), fmt.Sprintf("%#v", source)} {
		if strings.Contains(s, "fixture-account") || strings.Contains(s, path) || strings.Contains(s, "workspace") {
			t.Fatal("source formatter leaked private metadata")
		}
	}
}
func TestProjectSourceEmptyModelSelectionAndBoundedLists(t *testing.T) {
	path, d := sourceFixture(t)
	d.Routes = nil
	d.Projects[0].Routes = nil
	d.Projects[0].Layer = stageplan.Layer{}
	d.Projects[0].Write = true
	writeSource(t, path, d)
	s, e := Load(path)
	if e != nil {
		t.Fatal(e)
	}
	p, e := s.Project("fixture-project")
	if e != nil || !p.Write || len(p.Configuration.Routes) != 0 || p.Configuration.Project.Roles[stageplan.Design].Mode != stageplan.Inherit {
		t.Fatal("empty selection invented model", e)
	}
	for i := 0; i < MaxProjects+1; i++ {
		x := d.Projects[0]
		x.ID = fmt.Sprint("fixture-", i)
		d.Projects = append(d.Projects, x)
	}
	writeSource(t, path, d)
	if _, e = Load(path); !errors.Is(e, ErrRegistration) {
		t.Fatal("project limit", e)
	}
}

func TestProjectSourceRegistersDraftWithExistingAuthenticatedAPI(t *testing.T) {
	path, _ := sourceFixture(t)
	source, e := Load(path)
	if e != nil {
		t.Fatal(e)
	}
	p, e := source.Project("fixture-project")
	if e != nil {
		t.Fatal(e)
	}
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
	defer st.Close()
	s, e := api.New(st, policy.NewManager("fixture-management", policy.StoreValidator(st), nil))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.SetProject(p.ID, p.Configuration); e != nil {
		t.Fatal(e)
	}
	h := s.Handler()
	req := httptest.NewRequest("GET", "/control/v1/projects/"+p.ID+"/configuration", nil)
	req.Header.Set("Authorization", "Bearer fixture-management")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"admitted":false`) || strings.Contains(w.Body.String(), p.Path) {
		t.Fatal("source draft failed API registration or exposed folder", w.Code)
	}
	req = httptest.NewRequest("POST", "/control/v1/tasks/preview", strings.NewReader(`{"project_id":"fixture-project","goal":"fixture preview","required_roles":["design"]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer fixture-management")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 422 || !strings.Contains(w.Body.String(), `"code":"route_revision_not_admitted"`) {
		t.Fatal("draft source did not fail route admission", w.Code, w.Body.String())
	}
}

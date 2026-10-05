//go:build darwin || linux

package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/control"
	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/routes"
	"github.com/yetone/magpie/internal/fusion/runtime/glm"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

// The registry and inspector below are explicitly synthetic trusted services,
// never a real account, billing, quota or publisher admission report.
func glmFactoryFixture(t *testing.T, writable bool) (string, Document, GLMRuntimeConfig, stageplan.Route, *atomic.Int64) {
	t.Helper()
	path, d := sourceFixture(t)
	high := "high"
	d.Routes[0].Model, d.Routes[0].RuntimeVersion = "glm-5.3", glm.CLIVersion
	d.Routes[0].NoEffort, d.Routes[0].Efforts, d.Routes[0].DefaultEffort = false, []string{"high"}, &high
	d.Projects[0].Write = writable
	d.Projects[0].Layer = stageplan.Layer{Roles: map[stageplan.Role]stageplan.Binding{}}
	for _, role := range stageplan.AllRoles() {
		d.Projects[0].Layer.Roles[role] = stageplan.Binding{Mode: stageplan.Locked, Route: &stageplan.RouteRef{ID: "fixture-glm", Revision: 1}, Model: "glm-5.3", Effort: &stageplan.EffortSelection{Mode: stageplan.EffortExplicit, Value: "high"}}
	}
	writeSource(t, path, d)
	source, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := source.Project("fixture-project")
	if err != nil {
		t.Fatal(err)
	}
	route := p.Configuration.Routes[0]
	route.Admitted, route.BillingKnown, route.LockEnforcement, route.Capabilities = true, true, stageplan.ControlledCalls, []string{"text"}
	registry := routes.NewRegistry(func(e routes.Evidence) bool { return e.ReportHash == strings.Repeat("a", 64) })
	candidate := routes.Builtins()[2]
	candidate.ID = route.ID
	if err := registry.Register(candidate); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(route)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	evidence := routes.Evidence{RouteHash: hex.EncodeToString(hash[:]), Purpose: "generation", Route: stageplan.RouteRef{ID: route.ID, Revision: route.Revision}, RuntimeVersion: route.RuntimeVersion, Model: route.Model, Account: route.Account, Workspace: route.Workspace, CredentialIdentity: route.CredentialIdentity, Region: "CN", Billing: "coding_plan", TransportID: "fixture-cn-transport", ReportHash: strings.Repeat("a", 64), PublisherVerified: true, IdentityVerified: true, BillingVerified: true, ModelVerified: true, EffortVerified: true, AllCallsControlled: true, WorkspaceEnforced: true}
	if err := registry.Admit(route, evidence); err != nil {
		t.Fatal(err)
	}
	keyRoot := privateExecutionDir(t)
	keyDir := filepath.Join(keyRoot, "credentials")
	if err := os.Mkdir(keyDir, 0700); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(keyDir, "glm-coding-plan.key")
	if err := os.WriteFile(key, []byte("fixture-controller-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var inspections atomic.Int64
	c := GLMRuntimeConfig{ProjectID: "fixture-project", Route: stageplan.RouteRef{ID: route.ID, Revision: route.Revision}, Registry: registry, Executable: filepath.Join(privateExecutionDir(t), "fixture-executable"), CredentialPath: key, ExecutionRoot: privateExecutionDir(t), Timeout: 30 * time.Second, Inspect: func(ctx context.Context, task store.Task, role stageplan.Role, target stageplan.ExecutionTarget) (policy.Inspection, error) {
		inspections.Add(1)
		in := fixtureHostInspection(route)
		in.Provider, in.Quota.Identity.Provider, in.Quota.Pool.Provider = "bigmodel", "bigmodel", "bigmodel"
		if writable && (role == stageplan.Implementation || role == stageplan.Testing) {
			in.WriteKey = "fixture-original-project-write"
		}
		return in, nil
	}}
	// Writable testing now requires an independently registered test subtree.
	c.TestingWritePaths = []string{"tests"}
	return path, d, c, route, &inspections
}

func TestGLMFactoryRequiresIndependentAdmissionAndPrivateRegistration(t *testing.T) {
	for _, mode := range []string{"nil_registry", "unadmitted", "nil_inspector", "route", "revision", "relative_executable", "relative_root", "shared_root", "project_root", "credential_root", "key_in_project", "key_permissions", "no_read", "model", "account", "runtime", "no_effort"} {
		t.Run(mode, func(t *testing.T) {
			path, d, c, _, calls := glmFactoryFixture(t, false)
			switch mode {
			case "nil_registry":
				c.Registry = nil
			case "unadmitted":
				c.Registry = routes.NewRegistry(nil)
			case "nil_inspector":
				c.Inspect = nil
			case "route":
				c.Route.ID = "missing"
			case "revision":
				c.Route.Revision++
			case "relative_executable":
				c.Executable = "claude"
			case "relative_root":
				c.ExecutionRoot = "runtime"
			case "shared_root":
				if err := os.Chmod(c.ExecutionRoot, 0755); err != nil {
					t.Fatal(err)
				}
			case "project_root":
				c.ExecutionRoot = d.Projects[0].Path
			case "credential_root":
				c.ExecutionRoot = filepath.Dir(c.CredentialPath)
			case "key_in_project":
				c.CredentialPath = filepath.Join(d.Projects[0].Path, "glm-coding-plan.key")
				if err := os.WriteFile(c.CredentialPath, []byte("fixture-key"), 0600); err != nil {
					t.Fatal(err)
				}
			case "key_permissions":
				if err := os.Chmod(c.CredentialPath, 0644); err != nil {
					t.Fatal(err)
				}
			case "no_read":
				d.Projects[0].Read = false
				writeSource(t, path, d)
			case "model":
				d.Routes[0].Model = "glm-other"
				for role, b := range d.Projects[0].Layer.Roles {
					b.Model = "glm-other"
					d.Projects[0].Layer.Roles[role] = b
				}
				writeSource(t, path, d)
			case "account":
				d.Routes[0].Account = "other-account"
				writeSource(t, path, d)
			case "runtime":
				d.Routes[0].RuntimeVersion = "different-version"
				writeSource(t, path, d)
			case "no_effort":
				d.Routes[0].NoEffort = true
				d.Routes[0].DefaultEffort = nil
				d.Routes[0].Efforts = nil
				for role, b := range d.Projects[0].Layer.Roles {
					b.Effort = &stageplan.EffortSelection{Mode: stageplan.EffortNone}
					d.Projects[0].Layer.Roles[role] = b
				}
				writeSource(t, path, d)
			}
			factory, err := NewGLMRuntimeFactory(c)
			if err == nil {
				h, openErr := OpenExecutionControl(context.Background(), path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0", factory)
				if h != nil {
					h.Close()
				}
				err = openErr
			}
			if !errors.Is(err, ErrControlHost) || calls.Load() != 0 {
				t.Fatal("unsafe startup accepted or inspector invoked during registration", err, calls.Load())
			}
		})
	}
}

func TestGLMFactoryResolvesOwnedCopyAndPreservesFrozenPrompt(t *testing.T) {
	for _, writable := range []bool{false, true} {
		path, d, c, route, calls := glmFactoryFixture(t, writable)
		if err := os.WriteFile(filepath.Join(d.Projects[0].Path, "file.txt"), []byte("original\n"), 0600); err != nil {
			t.Fatal(err)
		}
		factory, err := NewGLMRuntimeFactory(c)
		if err != nil {
			t.Fatal(err)
		}
		h, err := OpenExecutionControl(context.Background(), path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0", factory)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { h.Close() })
		if calls.Load() != 0 {
			t.Fatal("factory scheduled or inspected during startup")
		}
		for _, role := range stageplan.AllRoles() {
			plan, err := stageplan.Compile(1, []stageplan.Role{role}, stageplan.Layer{}, d.Projects[0].Layer, stageplan.Layer{}, []stageplan.Route{route})
			if err != nil {
				t.Fatal(err)
			}
			task, err := h.store.Create("fixture-"+string(role), store.CreateRequest{ProjectID: c.ProjectID, Goal: "literal task goal <untrusted text>", Plan: plan})
			if err != nil {
				t.Fatal(err)
			}
			target := *plan.Bindings[role].Target
			launch, err := h.resolveExecution(context.Background(), task, role, target)
			if err != nil {
				t.Fatal("real GLM resolver", role, err)
			}
			wantWrite := writable && (role == stageplan.Implementation || role == stageplan.Testing)
			if launch.Spec.Writable != wantWrite || launch.Spec.Workspace == d.Projects[0].Path || !launch.Spec.Source.BoundToSource(d.Projects[0].Path) || !launch.Spec.Source.ValidFor(launch.Spec.Workspace) || launch.Spec.Root == launch.Spec.Workspace || launch.Backend.Probe == nil || launch.Backend.Start == nil || launch.Backend.Release == nil {
				t.Fatal("launch scope/backend drift", role)
			}
			var input struct {
				Role stageplan.Role `json:"role"`
				Goal string         `json:"goal"`
			}
			if json.Unmarshal(launch.Spec.Input, &input) != nil || input.Role != role || input.Goal != task.Goal {
				t.Fatal("frozen role/goal lost")
			}
			b, err := os.ReadFile(filepath.Join(launch.Spec.Workspace, "file.txt"))
			if err != nil || string(b) != "original\n" {
				t.Fatal("copy content lost", err)
			}
			if err := os.WriteFile(filepath.Join(launch.Spec.Workspace, "file.txt"), []byte("copy only\n"), 0600); err != nil {
				t.Fatal(err)
			}
			b, err = os.ReadFile(filepath.Join(d.Projects[0].Path, "file.txt"))
			if err != nil || string(b) != "original\n" {
				t.Fatal("original changed")
			}
		}
	}
}

func TestGLMFactoryRechecksRegistryCredentialSourceAndRoot(t *testing.T) {
	for _, mode := range []string{"registry", "credential", "source", "root", "target", "other_project", "inspector_route", "inspector_provider"} {
		t.Run(mode, func(t *testing.T) {
			path, d, c, route, _ := glmFactoryFixture(t, false)
			if mode == "inspector_route" || mode == "inspector_provider" {
				original := c.Inspect
				c.Inspect = func(ctx context.Context, task store.Task, role stageplan.Role, target stageplan.ExecutionTarget) (policy.Inspection, error) {
					in, err := original(ctx, task, role, target)
					if mode == "inspector_route" {
						in.Route.Account = "other"
					} else {
						in.Provider = "other"
					}
					return in, err
				}
			}
			factory, err := NewGLMRuntimeFactory(c)
			if err != nil {
				t.Fatal(err)
			}
			h, err := OpenExecutionControl(context.Background(), path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0", factory)
			if err != nil {
				t.Fatal(err)
			}
			defer h.Close()
			plan, err := stageplan.Compile(1, []stageplan.Role{stageplan.Design}, stageplan.Layer{}, d.Projects[0].Layer, stageplan.Layer{}, []stageplan.Route{route})
			if err != nil {
				t.Fatal(err)
			}
			task, err := h.store.Create("fixture", store.CreateRequest{ProjectID: c.ProjectID, Goal: "fixture", Plan: plan})
			if err != nil {
				t.Fatal(err)
			}
			target := *plan.Bindings[stageplan.Design].Target
			switch mode {
			case "registry":
				c.Registry.Revoke(c.Route)
			case "credential":
				if err := os.WriteFile(c.CredentialPath, []byte("fixture-rotated-key\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "source":
				d.Revision++
				writeSource(t, path, d)
			case "root":
				if err := os.Chmod(c.ExecutionRoot, 0755); err != nil {
					t.Fatal(err)
				}
			case "target":
				target.ResolvedModel = "other"
			case "other_project":
				task.ProjectID = "other"
			}
			before, err := os.ReadDir(c.ExecutionRoot)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = h.inspectExecution(context.Background(), task, stageplan.Design, target); err == nil {
				t.Fatal("changed current identity inspected")
			}
			if mode != "inspector_route" && mode != "inspector_provider" {
				if _, err = h.resolveExecution(context.Background(), task, stageplan.Design, target); err == nil {
					t.Fatal("changed current identity resolved")
				}
			}
			after, err := os.ReadDir(c.ExecutionRoot)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("rejected resolve copied project", err)
			}
		})
	}
}

func TestGLMFactoryPreservesDefaultEffortWithResolvedValue(t *testing.T) {
	path, d, c, route, _ := glmFactoryFixture(t, false)
	b := d.Projects[0].Layer.Roles[stageplan.Design]
	b.Effort = &stageplan.EffortSelection{Mode: stageplan.EffortDefault}
	d.Projects[0].Layer.Roles[stageplan.Design] = b
	writeSource(t, path, d)
	factory, err := NewGLMRuntimeFactory(c)
	if err != nil {
		t.Fatal(err)
	}
	h, err := OpenExecutionControl(context.Background(), path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0", factory)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	plan, err := stageplan.Compile(1, []stageplan.Role{stageplan.Design}, stageplan.Layer{}, d.Projects[0].Layer, stageplan.Layer{}, []stageplan.Route{route})
	if err != nil {
		t.Fatal(err)
	}
	task, err := h.store.Create("fixture", store.CreateRequest{ProjectID: c.ProjectID, Goal: "fixture", Plan: plan})
	if err != nil {
		t.Fatal(err)
	}
	target := *plan.Bindings[stageplan.Design].Target
	if target.Effort.RequestedMode != stageplan.EffortDefault || target.Effort.Value == nil || *target.Effort.Value != "high" {
		t.Fatal("test oracle has wrong default")
	}
	if _, err = h.resolveExecution(context.Background(), task, stageplan.Design, target); err != nil {
		t.Fatal("valid resolved default refused", err)
	}
}

func TestGLMFactoryRejectsReplacedRootBeforeRegistration(t *testing.T) {
	path, _, c, _, _ := glmFactoryFixture(t, false)
	factory, err := NewGLMRuntimeFactory(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(c.ExecutionRoot, c.ExecutionRoot+"-old"); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(c.ExecutionRoot + "-old")
	if err := os.Mkdir(c.ExecutionRoot, 0700); err != nil {
		t.Fatal(err)
	}
	h, err := OpenExecutionControl(context.Background(), path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0", factory)
	if h != nil {
		h.Close()
	}
	if !errors.Is(err, ErrControlHost) {
		t.Fatal("replaced root registered", err)
	}
}

func TestGLMFactoryDoesNotDiscardChangesThroughFreshMultistageCopies(t *testing.T) {
	path, d, c, route, _ := glmFactoryFixture(t, true)
	factory, err := NewGLMRuntimeFactory(c)
	if err != nil {
		t.Fatal(err)
	}
	h, err := OpenExecutionControl(context.Background(), path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0", factory)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	plan, err := stageplan.Compile(1, stageplan.AllRoles(), stageplan.Layer{}, d.Projects[0].Layer, stageplan.Layer{}, []stageplan.Route{route})
	if err != nil {
		t.Fatal(err)
	}
	task, err := h.store.Create("fixture", store.CreateRequest{ProjectID: c.ProjectID, Goal: "fixture multistage", Plan: plan})
	if err != nil {
		t.Fatal(err)
	}
	target := *plan.Bindings[stageplan.Implementation].Target
	if _, err := h.resolveExecution(context.Background(), task, stageplan.Implementation, target); err == nil {
		t.Fatal("multistage fresh copy would discard previous implementation")
	}
	entries, err := os.ReadDir(c.ExecutionRoot)
	if err != nil || len(entries) != 0 {
		t.Fatal("unsupported multistage copied source", err)
	}
	// The trusted resolver exposes the actionable cause before the host masks
	// private execution errors; this is not a temporary proof of Handoff support.
	if _, err := h.runtime.Resolve(context.Background(), task, stageplan.Implementation, target); !errors.Is(err, control.ErrUnsupported) {
		t.Fatal("wrong multistage cause", err)
	}
}

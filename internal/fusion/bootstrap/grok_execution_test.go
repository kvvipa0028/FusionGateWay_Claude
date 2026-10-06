//go:build darwin

package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/routes"
	"github.com/yetone/magpie/internal/fusion/runtime/grok"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

// Only synthetic independent admission, cache and quota.
func grokRuntimeFixture(t *testing.T, writable bool) (string, Document, GrokRuntimeConfig, stageplan.Route) {
	t.Helper()
	path, d := sourceFixture(t)
	d.Routes[0].NativeRoute = "grok-subscription"
	d.Routes[0].Model, d.Routes[0].RuntimeVersion = "fixture-model", grok.CLIVersion
	// The managed Grok adapter currently supports only the no-effort surface.
	d.Routes[0].NoEffort = true
	d.Projects[0].Write = writable
	d.Projects[0].Layer = stageplan.Layer{Roles: map[stageplan.Role]stageplan.Binding{}}
	for _, role := range stageplan.AllRoles() {
		d.Projects[0].Layer.Roles[role] = stageplan.Binding{Mode: stageplan.Locked, Route: &stageplan.RouteRef{ID: d.Routes[0].ID, Revision: 1}, Model: "fixture-model", Effort: &stageplan.EffortSelection{Mode: stageplan.EffortNone}}
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
	var candidate routes.Candidate
	for _, c := range routes.Builtins() {
		if c.ID == "grok-subscription" {
			candidate = c
		}
	}
	candidate.ID = route.ID
	candidate.Region = "US"
	if err := registry.Register(candidate); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(route)
	hash := sha256.Sum256(raw)
	proof := routes.Evidence{RouteHash: hex.EncodeToString(hash[:]), Purpose: "generation", Route: stageplan.RouteRef{ID: route.ID, Revision: route.Revision}, RuntimeVersion: route.RuntimeVersion, Model: route.Model, Account: route.Account, Workspace: route.Workspace, CredentialIdentity: route.CredentialIdentity, Region: "US", Billing: "subscription", TransportID: "synthetic-subscription", ReportHash: strings.Repeat("a", 64), PublisherVerified: true, IdentityVerified: true, BillingVerified: true, ModelVerified: true, EffortVerified: true, AllCallsControlled: true, WorkspaceEnforced: true}
	if err := registry.Admit(route, proof); err != nil {
		t.Fatal(err)
	}
	auth := filepath.Join(privateExecutionDir(t), "grok", "home", ".grok")
	if err := os.MkdirAll(auth, 0700); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(auth, "auth.json")
	if err := os.WriteFile(cache, []byte(`{"https://accounts.x.ai/sign-in":{"key":"opaque-fixture-grok-bearer","expires_at":"2026-10-12T10:00:00Z"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	config := GrokRuntimeConfig{ProjectID: p.ID, Route: proof.Route, Registry: registry, Executable: filepath.Join(privateExecutionDir(t), "not-executed"), CredentialPath: cache, ExecutionRoot: privateExecutionDir(t), Issuer: "https://accounts.x.ai/sign-in", TestingWritePaths: []string{"tests"}, Timeout: 30 * time.Second,
		Inspect: func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (policy.Inspection, error) {
			in := fixtureHostInspection(route)
			in.Provider, in.Quota.Identity.Provider, in.Quota.Pool.Provider = "xai", "xai", "xai"
			in.Quota.Identity.Region, in.Quota.Pool.Region = "US", "US"
			return in, nil
		}}
	if writable {
		base := config.Inspect
		config.Inspect = func(ctx context.Context, task store.Task, role stageplan.Role, target stageplan.ExecutionTarget) (policy.Inspection, error) {
			in, e := base(ctx, task, role, target)
			if role == stageplan.Implementation || role == stageplan.Testing {
				in.WriteKey = "fixture-project-write"
			}
			return in, e
		}
	}
	return path, d, config, route
}

func TestGrokFactoryPrivateRegistration(t *testing.T) {
	for _, mode := range []string{"registry", "inspector", "route", "revision", "relative_executable", "root_permissions", "credential_root", "no_read", "unadmitted", "wrong_account", "runtime", "native_route", "nil_context"} {
		t.Run(mode, func(t *testing.T) {
			path, d, c, _ := grokRuntimeFixture(t, false)
			switch mode {
			case "native_route":
				d.Routes[0].NativeRoute = "codex-chatgpt"
				writeSource(t, path, d)
			case "registry":
				c.Registry = nil
			case "inspector":
				c.Inspect = nil
			case "route":
				c.Route.ID = "other"
			case "revision":
				c.Route.Revision++
			case "relative_executable":
				c.Executable = "relative"
			case "root_permissions":
				if e := os.Chmod(c.ExecutionRoot, 0755); e != nil {
					t.Fatal(e)
				}
			case "credential_root":
				c.ExecutionRoot = filepath.Dir(c.CredentialPath)
			case "no_read":
				d.Projects[0].Read = false
				writeSource(t, path, d)
			case "unadmitted":
				c.Registry = routes.NewRegistry(nil)
			case "wrong_account":
				d.Routes[0].Account = "other"
				writeSource(t, path, d)
			case "runtime":
				d.Routes[0].RuntimeVersion = "wrong"
				writeSource(t, path, d)
			}
			f, err := NewGrokRuntimeFactory(c)
			if err != nil {
				return
			}
			if mode == "nil_context" {
				if _, err := f(nil, RuntimeEnvironment{}); err == nil {
					t.Fatal("nil context accepted")
				}
				return
			}
			h, err := OpenExecutionControl(context.Background(), path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0", f)
			if h != nil {
				defer h.Close()
			}
			if err == nil {
				t.Fatal("invalid registration accepted")
			}
		})
	}
}

func grokFactoryTask(t *testing.T, h *ControlHost, d Document, r stageplan.Route, role stageplan.Role) (store.Task, stageplan.ExecutionTarget) {
	t.Helper()
	plan, err := stageplan.Compile(1, []stageplan.Role{role}, stageplan.Layer{}, d.Projects[0].Layer, stageplan.Layer{}, []stageplan.Route{r})
	if err != nil {
		t.Fatal(err)
	}
	task, err := h.store.Create("fixture-"+string(role), store.CreateRequest{ProjectID: d.Projects[0].ID, Goal: "Frozen grok factory goal", Plan: plan})
	if err != nil {
		t.Fatal(err)
	}
	return task, *plan.Bindings[role].Target
}

func TestGrokFactoryExactCopiesAndRoles(t *testing.T) {
	for _, writable := range []bool{false, true} {
		t.Run(map[bool]string{false: "readonly", true: "writers"}[writable], func(t *testing.T) {
			path, d, c, r := grokRuntimeFixture(t, writable)
			if e := os.WriteFile(filepath.Join(d.Projects[0].Path, "source.txt"), []byte("original"), 0600); e != nil {
				t.Fatal(e)
			}
			f, err := NewGrokRuntimeFactory(c)
			if err != nil {
				t.Fatal(err)
			}
			h, err := OpenExecutionControl(context.Background(), path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0", f)
			if err != nil {
				t.Fatal(err)
			}
			defer h.Close()
			for _, role := range stageplan.AllRoles() {
				task, target := grokFactoryTask(t, h, d, r, role)
				launch, err := h.resolveExecution(context.Background(), task, role, target)
				if err != nil {
					t.Fatal(role, err)
				}
				wantWrite := writable && (role == stageplan.Implementation || role == stageplan.Testing)
				if launch.Spec.Writable != wantWrite || launch.Spec.Workspace == d.Projects[0].Path || !launch.Spec.Source.BoundToSource(d.Projects[0].Path) || !launch.Spec.Source.ValidFor(launch.Spec.Workspace) || launch.Spec.Root == launch.Spec.Workspace {
					t.Fatal("source scope lost")
				}
				var prompt struct {
					Role stageplan.Role `json:"role"`
					Goal string         `json:"goal"`
				}
				if json.Unmarshal(launch.Spec.Input, &prompt) != nil || prompt.Role != role || prompt.Goal != task.Goal {
					t.Fatal("frozen prompt lost")
				}
				if launch.Spec.Timeout != c.Timeout || target.Effort.RequestedMode != stageplan.EffortNone || target.Effort.Value != nil {
					t.Fatal("timeout or default effort lost")
				}
				if wantWrite && launch.Backend.ValidateLaunch(context.Background(), role, target, launch.Spec) != nil {
					t.Fatal("bounded writer launch refused")
				}
				if e := os.WriteFile(filepath.Join(launch.Spec.Workspace, "source.txt"), []byte("copy"), 0600); e != nil {
					t.Fatal(e)
				}
				raw, e := os.ReadFile(filepath.Join(d.Projects[0].Path, "source.txt"))
				if e != nil || string(raw) != "original" {
					t.Fatal("original modified")
				}
			}
		})
	}
}

func TestGrokFactoryCurrentBeforeCopy(t *testing.T) {
	for _, mode := range []string{"registry", "credential", "root", "source", "target", "task", "provider", "region", "pool_provider", "pool_region", "inspection_target", "inspection_mutates_target", "closed"} {
		t.Run(mode, func(t *testing.T) {
			path, d, c, r := grokRuntimeFixture(t, false)
			base := c.Inspect
			c.Inspect = func(ctx context.Context, task store.Task, role stageplan.Role, target stageplan.ExecutionTarget) (policy.Inspection, error) {
				in, e := base(ctx, task, role, target)
				switch mode {
				case "provider":
					in.Provider = "openai"
				case "region":
					in.Quota.Identity.Region = "CN"
				case "pool_provider":
					in.Quota.Pool.Provider = "openai"
				case "pool_region":
					in.Quota.Pool.Region = "CN"
				case "inspection_target":
					in.Route.Account = "other"
				case "inspection_mutates_target":
					mode := stageplan.EffortExplicit
					target.Effort = stageplan.FrozenEffort{RequestedMode: mode, Value: ptrGrok("low")}
				}
				return in, e
			}
			f, e := NewGrokRuntimeFactory(c)
			if e != nil {
				t.Fatal(e)
			}
			h, e := OpenExecutionControl(context.Background(), path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0", f)
			if e != nil {
				t.Fatal(e)
			}
			defer h.Close()
			task, target := grokFactoryTask(t, h, d, r, stageplan.Design)
			switch mode {
			case "registry":
				c.Registry.Revoke(c.Route)
			case "credential":
				if e := os.WriteFile(c.CredentialPath, []byte(`{}`), 0600); e != nil {
					t.Fatal(e)
				}
			case "root":
				if e := os.Chmod(c.ExecutionRoot, 0755); e != nil {
					t.Fatal(e)
				}
			case "source":
				d.Revision++
				writeSource(t, path, d)
			case "target":
				target.ResolvedModel = "other"
			case "task":
				task.Goal = "other"
			case "closed":
				if e := h.runtime.Close(context.Background()); e != nil {
					t.Fatal(e)
				}
			}
			before, e := os.ReadDir(c.ExecutionRoot)
			if e != nil {
				t.Fatal(e)
			}
			_, err := h.resolveExecution(context.Background(), task, stageplan.Design, target)
			if mode == "inspection_mutates_target" {
				if err != nil || target.Effort.RequestedMode != stageplan.EffortNone {
					t.Fatal("inspector mutated target")
				}
				return
			}
			if err == nil {
				t.Fatal("drift allowed copy")
			}
			after, e := os.ReadDir(c.ExecutionRoot)
			if e != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("rejected resolve created launch")
			}
		})
	}
}

func ptrGrok(s string) *string { return &s }

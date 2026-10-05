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
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/routes"
	"github.com/yetone/magpie/internal/fusion/runtime/codex"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

// Only synthetic independent admission, identity, OAuth cache and quota.
func codexFactoryFixture(t *testing.T, writable bool) (string, Document, CodexRuntimeConfig, stageplan.Route, *atomic.Int64) {
	t.Helper()
	path, d := sourceFixture(t)
	medium := "medium"
	d.Routes[0].NativeRoute = "codex-chatgpt"
	d.Routes[0].Model, d.Routes[0].RuntimeVersion = "fixture-model", codex.CLIVersion
	d.Routes[0].NoEffort, d.Routes[0].Efforts, d.Routes[0].DefaultEffort = false, []string{"medium"}, &medium
	d.Projects[0].Write = writable
	d.Projects[0].Layer = stageplan.Layer{Roles: map[stageplan.Role]stageplan.Binding{}}
	for _, role := range stageplan.AllRoles() {
		d.Projects[0].Layer.Roles[role] = stageplan.Binding{Mode: stageplan.Locked, Route: &stageplan.RouteRef{ID: d.Routes[0].ID, Revision: 1}, Model: "fixture-model", Effort: &stageplan.EffortSelection{Mode: stageplan.EffortDefault}}
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
	candidate := routes.Builtins()[0]
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
	auth := filepath.Join(privateExecutionDir(t), "codex", "home", ".codex")
	if err := os.MkdirAll(auth, 0700); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(auth, "auth.json")
	if err := os.WriteFile(cache, []byte(`{"auth_mode":"chatgpt","tokens":{"id_token":"fixture-id","access_token":"fixture-access","refresh_token":"fixture-refresh","account_id":"fixture-upstream"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	epoch := &atomic.Int64{}
	epoch.Store(7)
	config := CodexRuntimeConfig{ProjectID: p.ID, Route: proof.Route, Registry: registry, Executable: filepath.Join(privateExecutionDir(t), "not-executed"), CredentialPath: cache, UpstreamAccount: "fixture-upstream", ExecutionRoot: privateExecutionDir(t), TestingWritePaths: []string{"tests"}, Timeout: 30 * time.Second,
		Identity: func(context.Context) (codex.Identity, error) {
			return codex.Identity{Account: route.Account, Workspace: route.Workspace, CredentialIdentity: route.CredentialIdentity, Generation: epoch.Load()}, nil
		},
		Inspect: func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (policy.Inspection, error) {
			in := fixtureHostInspection(route)
			in.Provider, in.Quota.Identity.Provider, in.Quota.Pool.Provider = "openai", "openai", "openai"
			in.Quota.Identity.Region, in.Quota.Pool.Region = "US", "US"
			in.Quota.Identity.Generation = epoch.Load()
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
	return path, d, config, route, epoch
}

func TestCodexFactoryPrivateRegistration(t *testing.T) {
	for _, mode := range []string{"registry", "identity", "inspector", "route", "revision", "relative_executable", "root_permissions", "credential_root", "no_read", "unadmitted", "wrong_account", "runtime", "upstream_account", "cache_permissions", "zero_epoch", "identity_account", "nil_context", "native_route"} {
		t.Run(mode, func(t *testing.T) {
			path, d, c, _, epoch := codexFactoryFixture(t, false)
			switch mode {
			case "native_route":
				d.Routes[0].NativeRoute = "grok-subscription"
				writeSource(t, path, d)
			case "registry":
				c.Registry = nil
			case "identity":
				c.Identity = nil
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
			case "upstream_account":
				c.UpstreamAccount = "other"
			case "cache_permissions":
				if e := os.Chmod(c.CredentialPath, 0644); e != nil {
					t.Fatal(e)
				}
			case "zero_epoch":
				epoch.Store(0)
			case "identity_account":
				c.Identity = func(context.Context) (codex.Identity, error) {
					return codex.Identity{Account: "other", Workspace: "other", CredentialIdentity: "other", Generation: 7}, nil
				}
			}
			f, err := NewCodexRuntimeFactory(c)
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

func codexFactoryTask(t *testing.T, h *ControlHost, d Document, r stageplan.Route, role stageplan.Role) (store.Task, stageplan.ExecutionTarget) {
	t.Helper()
	plan, err := stageplan.Compile(1, []stageplan.Role{role}, stageplan.Layer{}, d.Projects[0].Layer, stageplan.Layer{}, []stageplan.Route{r})
	if err != nil {
		t.Fatal(err)
	}
	task, err := h.store.Create("fixture-"+string(role), store.CreateRequest{ProjectID: d.Projects[0].ID, Goal: "Frozen factory goal", Plan: plan})
	if err != nil {
		t.Fatal(err)
	}
	return task, *plan.Bindings[role].Target
}

func TestCodexFactoryExactCopiesAndRoles(t *testing.T) {
	for _, writable := range []bool{false, true} {
		t.Run(map[bool]string{false: "readonly", true: "writers"}[writable], func(t *testing.T) {
			path, d, c, r, _ := codexFactoryFixture(t, writable)
			if e := os.WriteFile(filepath.Join(d.Projects[0].Path, "source.txt"), []byte("original"), 0600); e != nil {
				t.Fatal(e)
			}
			var inspected atomic.Int64
			base := c.Inspect
			c.Inspect = func(ctx context.Context, task store.Task, role stageplan.Role, target stageplan.ExecutionTarget) (policy.Inspection, error) {
				inspected.Add(1)
				return base(ctx, task, role, target)
			}
			f, err := NewCodexRuntimeFactory(c)
			if err != nil {
				t.Fatal(err)
			}
			h, err := OpenExecutionControl(context.Background(), path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0", f)
			if err != nil {
				t.Fatal(err)
			}
			defer h.Close()
			if inspected.Load() != 0 {
				t.Fatal("startup inspection")
			}
			for _, role := range stageplan.AllRoles() {
				task, target := codexFactoryTask(t, h, d, r, role)
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
				if launch.Spec.Timeout != c.Timeout || target.Effort.Value == nil || *target.Effort.Value != "medium" || target.Effort.RequestedMode != stageplan.EffortDefault {
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

// The official 0.160.0 effort catalogue is an open enumeration; the factory
// must honor the admitted route's declared tiers instead of GLM's own.
func TestCodexFactoryAdmittedEfforts(t *testing.T) {
	for _, mode := range []string{"xhigh", "minimal", "undeclared"} {
		t.Run(mode, func(t *testing.T) {
			path, d, c, r, _ := codexFactoryFixture(t, false)
			efforts, want := []string{"medium", "xhigh"}, "xhigh"
			if mode == "minimal" {
				efforts, want = append(efforts, "minimal"), "minimal"
			}
			if mode == "undeclared" {
				efforts, want = []string{"medium"}, "medium"
			}
			// A revoked route is blocked forever; widen the catalogue under a
			// new immutable revision instead.
			r.Revision++
			c.Route.Revision = r.Revision
			d.Routes[0].Revision = r.Revision
			d.Projects[0].Routes[0].Revision = r.Revision
			candidate := routes.Builtins()[0]
			candidate.ID, candidate.Revision, candidate.Region = r.ID, r.Revision, "US"
			if err := c.Registry.Register(candidate); err != nil {
				t.Fatal(err)
			}
			r.Efforts = efforts
			raw, _ := json.Marshal(r)
			digest := sha256.Sum256(raw)
			proof := routes.Evidence{RouteHash: hex.EncodeToString(digest[:]), Purpose: "generation", Route: stageplan.RouteRef{ID: r.ID, Revision: r.Revision}, RuntimeVersion: r.RuntimeVersion, Model: r.Model, Account: r.Account, Workspace: r.Workspace, CredentialIdentity: r.CredentialIdentity, Region: "US", Billing: "subscription", TransportID: "synthetic-subscription", ReportHash: strings.Repeat("a", 64), PublisherVerified: true, IdentityVerified: true, BillingVerified: true, ModelVerified: true, EffortVerified: true, AllCallsControlled: true, WorkspaceEnforced: true}
			if err := c.Registry.Admit(r, proof); err != nil {
				t.Fatal(err)
			}
			d.Routes[0].Efforts = efforts
			for _, role := range stageplan.AllRoles() {
				binding := d.Projects[0].Layer.Roles[role]
				binding.Route = &stageplan.RouteRef{ID: r.ID, Revision: r.Revision}
				binding.Effort = &stageplan.EffortSelection{Mode: stageplan.EffortExplicit, Value: want}
				d.Projects[0].Layer.Roles[role] = binding
			}
			writeSource(t, path, d)
			// The fixture inspector still reports the original revision; point
			// the synthetic inspection at the newly admitted route.
			base := c.Inspect
			c.Inspect = func(ctx context.Context, task store.Task, role stageplan.Role, target stageplan.ExecutionTarget) (policy.Inspection, error) {
				in, e := base(ctx, task, role, target)
				in.Route = r
				return in, e
			}
			f, err := NewCodexRuntimeFactory(c)
			if err != nil {
				t.Fatal(err)
			}
			h, err := OpenExecutionControl(context.Background(), path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0", f)
			if err != nil {
				t.Fatal(err)
			}
			defer h.Close()
			task, target := codexFactoryTask(t, h, d, r, stageplan.Design)
			if mode == "undeclared" {
				*target.Effort.Value = "ultra"
			}
			launch, err := h.resolveExecution(context.Background(), task, stageplan.Design, target)
			if mode == "undeclared" {
				if err == nil {
					t.Fatal("undeclared effort accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(mode, err)
			}
			if target.Effort.Value == nil || *target.Effort.Value != want {
				t.Fatal("admitted effort lost")
			}
			if launch.Spec.Timeout != c.Timeout {
				t.Fatal("effort tier changed frozen timeout")
			}
		})
	}
}

func TestCodexFactoryCurrentBeforeCopy(t *testing.T) {
	for _, mode := range []string{"registry", "credential", "epoch", "root", "source", "target", "task", "provider", "region", "pool_provider", "pool_region", "quota_epoch", "inspection_target", "inspection_mutates_target", "closed"} {
		t.Run(mode, func(t *testing.T) {
			path, d, c, r, epoch := codexFactoryFixture(t, false)
			base := c.Inspect
			c.Inspect = func(ctx context.Context, task store.Task, role stageplan.Role, target stageplan.ExecutionTarget) (policy.Inspection, error) {
				in, e := base(ctx, task, role, target)
				switch mode {
				case "provider":
					in.Provider = "xai"
				case "region":
					in.Quota.Identity.Region = "CN"
				case "pool_provider":
					in.Quota.Pool.Provider = "xai"
				case "pool_region":
					in.Quota.Pool.Region = "CN"
				case "quota_epoch":
					in.Quota.Identity.Generation++
				case "inspection_target":
					in.Route.Account = "other"
				case "inspection_mutates_target":
					*target.Effort.Value = "high"
				}
				return in, e
			}
			f, e := NewCodexRuntimeFactory(c)
			if e != nil {
				t.Fatal(e)
			}
			h, e := OpenExecutionControl(context.Background(), path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0", f)
			if e != nil {
				t.Fatal(e)
			}
			defer h.Close()
			task, target := codexFactoryTask(t, h, d, r, stageplan.Design)
			switch mode {
			case "registry":
				c.Registry.Revoke(c.Route)
			case "credential":
				if e := os.WriteFile(c.CredentialPath, []byte(`{}`), 0600); e != nil {
					t.Fatal(e)
				}
			case "epoch":
				epoch.Add(1)
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
				if err != nil || *target.Effort.Value != "medium" {
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

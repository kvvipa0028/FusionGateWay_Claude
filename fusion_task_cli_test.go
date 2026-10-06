//go:build fusion

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/bootstrap"
	"github.com/yetone/magpie/internal/fusion/control"
	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/quota"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

// openAdmittedTaskHost mirrors the synthetic browser fixture: declared routes
// are locally admitted and inspected, while Resolve stays unsupported so no
// runtime can actually launch from the CLI test.
func openAdmittedTaskHost(t *testing.T, ctx context.Context, source, root string) *bootstrap.ControlHost {
	t.Helper()
	h, e := bootstrap.OpenExecutionControl(ctx, source, root, "127.0.0.1:0", func(_ context.Context, env bootstrap.RuntimeEnvironment) (bootstrap.RuntimeRegistration, error) {
		reg := bootstrap.RuntimeRegistration{Routes: map[string][]stageplan.Route{}, Inspect: func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (policy.Inspection, error) {
			return policy.Inspection{}, control.ErrUnsupported
		}, Resolve: func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (control.Launch, error) {
			return control.Launch{}, control.ErrUnsupported
		}}
		for _, p := range env.Source.Projects() {
			for _, route := range p.Configuration.Routes {
				route.Admitted, route.BillingKnown = true, true
				route.LockEnforcement = stageplan.ControlledCalls
				route.Capabilities = []string{"text"}
				reg.Routes[p.ID] = append(reg.Routes[p.ID], route)
				reg.Inspect = func(_ context.Context, task store.Task, _ stageplan.Role, target stageplan.ExecutionTarget) (policy.Inspection, error) {
					for _, r := range reg.Routes[task.ProjectID] {
						if r.ID == target.Route.ID && r.Revision == target.Route.Revision {
							now := time.Now()
							used := float64(20)
							return policy.Inspection{Route: r, Provider: "fixture-provider", QueryAdmitted: true, DataAllowed: true, SandboxVerified: true, VerificationAvailable: true, AllCallsCounted: true, ManagedExecutions: 1, ProofHash: strings.Repeat("a", 64), Quota: quota.Snapshot{Identity: quota.Identity{Provider: "fixture-provider", Account: r.Account, Workspace: r.Workspace, Region: "CN", Generation: 1}, Source: "fixture", ObservedAt: &now, ReceivedAt: now, Complete: true, Status: quota.Available, Pool: quota.Pool{ID: "fixture-pool", Provider: "fixture-provider", Region: "CN", Scope: "account", Owner: r.Account, Verified: true}, Windows: []quota.Window{{Kind: "subscription", Unit: "percent", UsedPercent: &used}}}}, nil
						}
					}
					return policy.Inspection{}, control.ErrUnsupported
				}
			}
		}
		return reg, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	return h
}

func fusionTaskFixture(t *testing.T) string {
	t.Helper()
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil || os.Chmod(root, 0700) != nil {
		t.Fatal("private root")
	}
	for _, name := range []string{"private/config", "workspace", "data/fusion-gateway/control"} {
		if e = os.MkdirAll(filepath.Join(root, name), 0700); e != nil {
			t.Fatal(e)
		}
	}
	doc := bootstrap.Document{SchemaVersion: 1, Revision: 1, Routes: []bootstrap.RouteDeclaration{{ID: "fixture-glm", Revision: 1, NativeRoute: "glm-cn-claude", Model: "fixture-model", Account: "fixture-account", Workspace: "fixture-provider-workspace", CredentialIdentity: "fixture-identity", RuntimeVersion: "fixture-version", NoEffort: true}}, Projects: []bootstrap.ProjectDeclaration{{ID: "fixture-project", Name: "CLI 验证项目", Path: filepath.Join(root, "workspace"), Read: true, Write: false, Routes: []stageplan.RouteRef{{ID: "fixture-glm", Revision: 1}}, Layer: stageplan.Layer{Groups: map[stageplan.Group]stageplan.Binding{stageplan.DesignPlanning: {Mode: stageplan.Locked, Route: &stageplan.RouteRef{ID: "fixture-glm", Revision: 1}, Model: "fixture-model", Effort: &stageplan.EffortSelection{Mode: stageplan.EffortNone}}}}}}}
	raw, e := json.Marshal(doc)
	if e != nil || os.WriteFile(filepath.Join(root, "private/config/projects.json"), raw, 0600) != nil {
		t.Fatal("source")
	}
	t.Setenv("FUSION_STATE_ROOT", root)
	return root
}

func TestFusionTaskClientRoundTrip(t *testing.T) {
	root := fusionTaskFixture(t)
	controlRoot := filepath.Join(root, "data", "fusion-gateway", "control")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	host := openAdmittedTaskHost(t, ctx, filepath.Join(root, "private/config/projects.json"), controlRoot)
	served := make(chan error, 1)
	go func() { served <- host.Serve(ctx) }()
	for i := 0; i < 40; i++ {
		if _, e := os.Stat(filepath.Join(controlRoot, "control.address")); e == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, e := os.Stat(filepath.Join(controlRoot, "control.address")); e != nil {
		t.Fatal("serving host did not publish its address")
	}
	run := func(args ...string) (string, error) {
		t.Helper()
		var out strings.Builder
		e := runFusionTask(args, &out)
		return strings.TrimSpace(out.String()), e
	}
	// No host state: refuse before any request once the address is gone.
	if out, e := run("preview", "-project", "fixture-project", "-goal", "goal"); e != nil {
		t.Fatal("preview failed", e, out)
	} else {
		var preview struct {
			PreviewID string `json:"preview_id"`
			Plan      struct {
				Hash string `json:"hash"`
			} `json:"plan"`
		}
		if json.Unmarshal([]byte(out), &preview) != nil || preview.PreviewID == "" || len(preview.Plan.Hash) != 64 {
			t.Fatal("preview reply unusable", out)
		}
		var submitted string
		if out2, e := run("submit", "-project", "fixture-project", "-preview", preview.PreviewID, "-hash", preview.Plan.Hash, "-key", "cli-roundtrip"); e != nil {
			t.Fatal("submit failed", e, out2)
		} else {
			var task struct {
				ID string `json:"id"`
			}
			if json.Unmarshal([]byte(out2), &task) != nil || task.ID == "" {
				t.Fatal("submit reply unusable", out2)
			}
			submitted = task.ID
		}
		// The explicit idempotency key replays the same task.
		if out2, e := run("submit", "-project", "fixture-project", "-preview", preview.PreviewID, "-hash", preview.Plan.Hash, "-key", "cli-roundtrip"); e != nil {
			t.Fatal("submit replay failed", e, out2)
		} else if !strings.Contains(out2, submitted) {
			t.Fatal("submit replay changed the task", out2)
		}
		var listed struct {
			Tasks []struct {
				ID string `json:"id"`
			} `json:"tasks"`
		}
		if out2, e := run("list", "-project", "fixture-project"); e != nil || json.Unmarshal([]byte(out2), &listed) != nil || len(listed.Tasks) != 1 || listed.Tasks[0].ID != submitted {
			t.Fatal("list unusable", e, out2)
		}
		if out2, e := run("show", "-task", submitted); e != nil || !strings.Contains(out2, `"ready"`) {
			t.Fatal("show unusable", e, out2)
		}
		// A draft host has no runtime resolver: start must fail, the task stays ready.
		if _, e := run("start", "-task", submitted, "-role", "design"); e == nil {
			t.Fatal("draft start accepted")
		}
		if out2, e := run("cancel", "-task", submitted); e != nil || !strings.Contains(out2, "cancelled") {
			t.Fatal("cancel failed", e, out2)
		}
	}
	if e := host.Close(); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(controlRoot, "control.address")); !os.IsNotExist(e) {
		t.Fatal("closed host kept its address file")
	}
	if _, e := run("show", "-task", "any"); e == nil {
		t.Fatal("client used a closed host")
	}
	<-served
}

func TestFusionTaskClientRejectsInvalidInput(t *testing.T) {
	root := fusionTaskFixture(t)
	controlRoot := filepath.Join(root, "data", "fusion-gateway", "control")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	host := openAdmittedTaskHost(t, ctx, filepath.Join(root, "private/config/projects.json"), controlRoot)
	go func() { host.Serve(ctx) }()
	for i := 0; i < 40; i++ {
		if _, e := os.Stat(filepath.Join(controlRoot, "control.address")); e == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	// The published address is authority-shaped but must be loopback.
	if e := os.WriteFile(filepath.Join(controlRoot, "control.address"), []byte("10.0.0.1:8080\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := runFusionTask([]string{"show", "-task", "t"}, &strings.Builder{}); e == nil {
		t.Fatal("non-loopback address accepted")
	}
	if e := os.WriteFile(filepath.Join(controlRoot, "control.address"), []byte("127.0.0.1:1\n"), 0600); e != nil {
		t.Fatal(e)
	}
	for _, args := range [][]string{{"preview", "-project", "../evil", "-goal", "g"}, {"show", "-task", "a/b"}, {"submit", "-project", "p", "-preview", "x", "-hash", "zz"}, {"bogus"}} {
		if e := runFusionTask(args, &strings.Builder{}); e == nil {
			t.Fatal("invalid input accepted", args)
		}
	}
	if e := host.Close(); e != nil {
		t.Fatal(e)
	}
}

//go:build darwin || linux

package bootstrap

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/workspace"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/api"
	"github.com/yetone/magpie/internal/fusion/control"
	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/quota"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

var uiFixtureSource = flag.String("fusion-ui-fixture-source", "", "explicit synthetic UI fixture source; test binary only")
var uiFixtureExecution = flag.String("fusion-ui-fixture-execution", "", "synthetic hold/success execution; test binary only")
var uiFixtureRoot = flag.String("fusion-ui-fixture-root", "", "explicit private UI fixture state; test binary only")
var uiFixtureQuota = flag.Bool("fusion-ui-fixture-quota", false, "synthetic quota only; no supplier calls")

// This is a browser test process, not a product entry point. Its admitted
// metadata is explicitly synthetic; default execution/inspection stays closed.
// Opt-in modes simulate Runtime/StopProof without a real process or provider.
func TestTaskUIFixtureProcess(t *testing.T) {
	if *uiFixtureSource == "" && *uiFixtureRoot == "" {
		t.Skip("explicit synthetic browser fixture only")
	}
	if *uiFixtureExecution != "" && *uiFixtureExecution != "hold" && *uiFixtureExecution != "success" {
		t.Fatal("invalid synthetic execution mode")
	}
	source, err := Load(*uiFixtureSource)
	if err != nil || *uiFixtureRoot == "" {
		t.Fatal("invalid private UI fixture")
	}
	for _, p := range source.Projects() {
		if !strings.HasPrefix(p.ID, "synthetic-ui") || !p.Read || p.Write {
			t.Fatal("non-synthetic UI fixture scope")
		}
		for _, r := range p.Configuration.Routes {
			if !strings.HasPrefix(r.Account, "fixture-") || !strings.HasPrefix(r.CredentialIdentity, "fixture-") || !strings.HasPrefix(r.RuntimeVersion, "fixture-") || !strings.HasPrefix(r.Model, "fixture-") {
				t.Fatal("non-synthetic UI fixture identity")
			}
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	h, err := OpenExecutionControl(ctx, *uiFixtureSource, *uiFixtureRoot, "127.0.0.1:0", func(_ context.Context, env RuntimeEnvironment) (RuntimeRegistration, error) {
		reg := RuntimeRegistration{Routes: map[string][]stageplan.Route{}, Inspect: func(context.Context, store.Task, stageplan.Role, stageplan.ExecutionTarget) (policy.Inspection, error) {
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
			}
			if *uiFixtureQuota {
				if reg.QuotaSources == nil {
					reg.QuotaSources = map[string][]api.QuotaSource{}
				}
				for _, route := range p.Configuration.Routes {
					identity := quota.Identity{Provider: "synthetic", Account: route.Account, Workspace: route.Workspace, Region: "fixture", Generation: 1}
					projectID := p.ID
					reg.QuotaSources[p.ID] = append(reg.QuotaSources[p.ID], api.QuotaSource{Route: stageplan.RouteRef{ID: route.ID, Revision: route.Revision}, Identity: identity,
						Current: func(ctx context.Context, i quota.Identity) bool {
							return ctx.Err() == nil && i == identity && env.Current(projectID)
						},
						Fetch: func(_ context.Context, i quota.Identity) (quota.Snapshot, error) {
							now := time.Now().UTC()
							used, remaining := float64(0), float64(100)
							return quota.Snapshot{Identity: i, Source: "synthetic-browser-quota", ObservedAt: &now, ReceivedAt: now, Status: quota.Unverified, Pool: quota.Pool{Provider: i.Provider, Region: i.Region}, Windows: []quota.Window{{Name: "synthetic window", Kind: "coding_plan", Unit: "percent", UsedPercent: &used, RemainingPercent: &remaining}}, Resources: []quota.Resource{{Kind: "credit", Unit: "CNY", Display: "2.50"}}}, nil
						}})
				}
			}
		}
		if *uiFixtureExecution != "" {
			reg.Inspect = func(_ context.Context, task store.Task, _ stageplan.Role, target stageplan.ExecutionTarget) (policy.Inspection, error) {
				for _, r := range reg.Routes[task.ProjectID] {
					if r.ID == target.Route.ID && r.Revision == target.Route.Revision {
						return fixtureHostInspection(r), nil
					}
				}
				return policy.Inspection{}, control.ErrUnsupported
			}
			reg.Resolve = func(_ context.Context, task store.Task, _ stageplan.Role, _ stageplan.ExecutionTarget) (control.Launch, error) {
				p, err := env.Source.Project(task.ProjectID)
				if err != nil {
					return control.Launch{}, err
				}
				snapshot, err := workspace.Copy(p.Path, privateExecutionDir(t), "copy")
				if err != nil {
					return control.Launch{}, err
				}
				guard, err := snapshot.Guard()
				if err != nil {
					return control.Launch{}, err
				}
				return control.Launch{Spec: managed.Spec{Root: privateExecutionDir(t), Workspace: snapshot.Path, Source: guard, Input: []byte("synthetic browser execution"), Timeout: 5 * time.Second}, Backend: control.Backend{
					Probe: func(context.Context) (managed.Capabilities, error) {
						return managed.Capabilities{Probe: true, Start: true, Events: true, Cancel: true}, nil
					},
					Start: func(ctx context.Context, run store.StageRun, _ managed.Spec) (control.Execution, error) {
						if err := env.Store.ConfirmStarted(run.ID, run.Generation, run.Owner, "synthetic-ui-session"); err != nil {
							return nil, err
						}
						x := &hostExecution{done: make(chan struct{}), cancelled: make(chan struct{})}
						go func() {
							var success <-chan time.Time
							var timer *time.Timer
							if *uiFixtureExecution == "success" {
								timer = time.NewTimer(100 * time.Millisecond)
								success = timer.C
								defer timer.Stop()
							}
							outcome := "cancelled"
							select {
							case <-success:
								outcome = "succeeded"
							case <-x.cancelled:
							case <-ctx.Done():
								x.Cancel()
							}
							if err := env.Store.Finish(run.ID, run.Generation, run.Owner, outcome); err != nil {
								t.Error(err)
							}
							x.result = managed.Result{State: outcome, StoppedVerified: true, Proof: policy.StopProof{RunID: run.ID, Generation: run.Generation, NativeSessionID: "synthetic-ui-session", ProcessIdentityHash: strings.Repeat("b", 64), ReportHash: strings.Repeat("c", 64), DescendantsStopped: true}}
							close(x.done)
						}()
						return x, nil
					}, Release: func(proof policy.StopProof) error {
						return env.Store.ReleaseReserved(proof.RunID, proof.Generation, proof.ReportHash, true)
					},
				}}, nil
			}
		}
		return reg, nil
	})
	if err != nil {
		t.Fatal("synthetic UI host initialization")
	}
	defer h.Close()
	announcement, _ := json.Marshal(map[string]any{"control_address": "http://" + h.Addr(), "synthetic_fixture": true, "execution_supported": *uiFixtureExecution != "", "synthetic_execution_mode": *uiFixtureExecution, "synthetic_quota": *uiFixtureQuota, "jev": "off"})
	fmt.Println(string(announcement))
	if err := h.Serve(ctx); err != nil {
		t.Fatal("synthetic UI host service")
	}
}

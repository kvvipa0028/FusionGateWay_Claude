//go:build darwin || linux

package bootstrap

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"

	"github.com/yetone/magpie/internal/fusion/control"
	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

var uiFixtureSource = flag.String("fusion-ui-fixture-source", "", "explicit synthetic UI fixture source; test binary only")
var uiFixtureRoot = flag.String("fusion-ui-fixture-root", "", "explicit private UI fixture state; test binary only")

// This is a browser test process, not a product entry point. Its admitted
// metadata is explicitly synthetic; execution and quota inspection always fail.
func TestTaskUIFixtureProcess(t *testing.T) {
	if *uiFixtureSource == "" && *uiFixtureRoot == "" {
		t.Skip("explicit synthetic browser fixture only")
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
		}
		return reg, nil
	})
	if err != nil {
		t.Fatal("synthetic UI host initialization")
	}
	defer h.Close()
	announcement, _ := json.Marshal(map[string]any{"control_address": "http://" + h.Addr(), "synthetic_fixture": true, "execution_supported": false, "jev": "off"})
	fmt.Println(string(announcement))
	if err := h.Serve(ctx); err != nil {
		t.Fatal("synthetic UI host service")
	}
}

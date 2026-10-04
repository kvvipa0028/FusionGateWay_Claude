package control

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/fusion/policy"
	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/runtime/codex"
	"github.com/yetone/magpie/internal/fusion/runtime/codexadapter"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

type launchValidationForwarder struct{}

func (launchValidationForwarder) Send(context.Context, stageplan.ExecutionTarget, []byte) (codex.ForwardResponse, error) {
	return codex.ForwardResponse{}, codex.ErrUnverified
}

func TestControllerCodexSpecRejectedBeforeIntent(t *testing.T) {
	for _, mode := range []string{"source_missing", "encoded_prompt", "timeout", "write", "target_version", "identity"} {
		t.Run(mode, func(t *testing.T) {
			effort := "medium"
			route := stageplan.Route{ID: "fixture-codex", Revision: 1, Model: "fixture-model", Account: "fixture-account", Workspace: "fixture-workspace", CredentialIdentity: "fixture-credential", RuntimeVersion: codex.CLIVersion, BillingPath: "subscription", BillingKnown: true, Admitted: true, Efforts: []string{effort}, DefaultEffort: &effort, Capabilities: []string{"text"}, LockEnforcement: stageplan.ControlledCalls}
			if mode == "target_version" {
				route.RuntimeVersion = "unverified-version"
			}
			f := controlRouteFixture(t, route, stageplan.EffortSelection{Mode: stageplan.EffortDefault})
			if mode != "source_missing" {
				controlSourceFixture(t, f)
			}
			f.config.RequireSource = false // Adapter must refuse absent provenance too.
			if mode == "encoded_prompt" {
				f.launch.Spec.Input = []byte(strings.Repeat("\n", 20000))
			}
			if mode == "timeout" {
				f.launch.Spec.Timeout *= 300
			}
			if mode == "write" {
				f.launch.Spec.Writable = true
				f.in.Role = stageplan.Design
			}
			a, e := codexadapter.NewAdapter(codexadapter.AdapterConfig{Scheduler: f.config.Scheduler, Manager: policy.NewManager("fixture-management", policy.StoreValidator(f.st), nil), Executable: "/fixture/pinned-codex", Identity: func(target stageplan.ExecutionTarget) codex.Identity {
				id := codex.Identity{Account: target.Account, Workspace: target.Workspace, CredentialIdentity: target.CredentialIdentity, Generation: 1}
				if mode == "identity" {
					id.Generation = 0
				}
				return id
			}, Current: func(codex.Binding) bool { return true }, Forwarder: launchValidationForwarder{}})
			if e != nil {
				t.Fatal(e)
			}
			backend := BindAdapter(a)
			// A successful pin/platform probe isolates the adapter-specific check.
			backend.Probe = f.launch.Backend.Probe
			f.launch.Backend = backend
			c := f.controller(t)
			receipt, e := c.Start(context.Background(), "fixture-reject", f.in)
			if e == nil || receipt.Created || receipt.Run.ID != "" {
				t.Fatal("adapter rejection committed an intent", e, receipt.Created)
			}
			task, e := f.st.Task(f.in.TaskID)
			if e != nil || task.State != "ready" || task.Generation != 0 {
				t.Fatal("adapter rejection changed task", e, task.State, task.Generation)
			}
			if _, e := f.st.LookupStart("fixture-reject", f.in); !errors.Is(e, store.ErrNotFound) {
				t.Fatal("adapter rejection left receipt", e)
			}
		})
	}
}

// A validator cannot rewrite the frozen prompt/target used after preflight.
func TestControllerLaunchValidationReceivesFrozenCopies(t *testing.T) {
	f := newControlFixture(t)
	calls := 0
	f.launch.Backend.ValidateLaunch = func(_ context.Context, role stageplan.Role, target stageplan.ExecutionTarget, spec managed.Spec) error {
		calls++
		if role != stageplan.Design || string(spec.Input) != "fixture prompt" {
			t.Fatal("wrong preflight binding")
		}
		spec.Input[0] = 'X'
		*target.Effort.Value = "low"
		return nil
	}
	oldStart := f.launch.Backend.Start
	f.launch.Backend.Start = func(ctx context.Context, r store.StageRun, s managed.Spec) (Execution, error) {
		if string(s.Input) != "fixture prompt" || *r.Target.Effort.Value != "high" {
			t.Error("validator rewrote launch")
		}
		return oldStart(ctx, r, s)
	}
	c := f.controller(t)
	got, e := c.Start(context.Background(), "fixture-validate-copy", f.in)
	if e != nil {
		t.Fatal(e)
	}
	close(f.finish)
	done := waitControl(t, c, got.Run.ID)
	if done.State != "succeeded" || calls != 1 {
		t.Fatal(done, calls)
	}
}

func TestControllerLaunchValidationFailureAndRevocationStayPreintent(t *testing.T) {
	for _, mode := range []string{"error", "management", "source", "context"} {
		t.Run(mode, func(t *testing.T) {
			f := newControlFixture(t)
			source := controlSourceFixture(t, f)
			live := true
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.launch.Backend.ValidateLaunch = func(context.Context, stageplan.Role, stageplan.ExecutionTarget, managed.Spec) error {
				switch mode {
				case "error":
					return errors.New("private fixture validation failure")
				case "management":
					live = false
				case "source":
					if e := os.WriteFile(source, []byte("changed"), 0600); e != nil {
						t.Fatal(e)
					}
				case "context":
					cancel()
				}
				return nil
			}
			c := f.controller(t)
			got, e := c.StartAuthorized(ctx, "fixture-validator-revoke", f.in, func(context.Context) bool { return live })
			if e == nil || got.Created || f.started.Load() != 0 || strings.Contains(e.Error(), "private fixture") {
				t.Fatal("preflight failure published authority", e)
			}
			task, e := f.st.Task(f.in.TaskID)
			if e != nil || task.State != "ready" || task.Generation != 0 {
				t.Fatal("preflight revoke committed intent", e)
			}
		})
	}
}

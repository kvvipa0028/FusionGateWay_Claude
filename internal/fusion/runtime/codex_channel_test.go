package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

func codexChannelRun() store.StageRun {
	effort := "medium"
	return store.StageRun{ID: "codex-run", TaskID: "codex-task", Owner: "codex-owner", Role: stageplan.Design, Attempt: 1, PlanRevision: 1, Generation: 1, Target: stageplan.ExecutionTarget{RequestedModel: "fixture-model", ResolvedModel: "fixture-model", Account: "fixture-account", Workspace: "fixture-provider-workspace", CredentialIdentity: "fixture-credential", RuntimeVersion: CodexCLIVersion, BillingPath: "subscription", Effort: stageplan.FrozenEffort{Value: &effort}}}
}

func TestCodexChannelFrozenScopeAndNoCallerLaunchAuthority(t *testing.T) {
	r := codexChannelRun()
	root, cwd := "/private/fusion-worker", "/private/fusion-project"
	c, e := NewCodexChannel(r, root, cwd, "launch-session", func(context.Context, io.ReadWriteCloser) error { return nil }, func() bool { return true })
	if e != nil {
		t.Fatal(e)
	}
	spec := Spec{Root: root, Workspace: cwd, Executable: "/private/native", ExecutableHash: CodexExecutableSHA256, Args: []string{"app-server", "--listen", "stdio://", "--strict-config"}, NativeSessionID: "launch-session", CodexChannel: c, Timeout: time.Second}
	if !c.valid(r, spec) {
		t.Fatal("valid scope refused")
	}
	*r.Target.Effort.Value = "high"
	if c.valid(r, spec) {
		t.Fatal("caller mutated frozen target")
	}
	r = codexChannelRun()
	for _, mode := range []string{"run", "generation", "owner", "role", "task", "plan", "attempt", "target", "root", "cwd", "hash", "args", "input", "fixture", "write", "session", "claude", "grok"} {
		t.Run(mode, func(t *testing.T) {
			bad, launch := r, spec
			switch mode {
			case "run":
				bad.ID += "other"
			case "generation":
				bad.Generation++
			case "owner":
				bad.Owner += "other"
			case "role":
				bad.Role = stageplan.Review
			case "task":
				bad.TaskID += "other"
			case "plan":
				bad.PlanRevision++
			case "attempt":
				bad.Attempt++
			case "target":
				bad.Target.Account += "other"
			case "root":
				launch.Root += "other"
			case "cwd":
				launch.Workspace += "other"
			case "hash":
				launch.ExecutableHash = strings.Repeat("0", 64)
			case "args":
				launch.Args = append(append([]string(nil), spec.Args...), "-c", "ignore_host_managed=true")
			case "input":
				launch.Input = []byte("caller input")
			case "fixture":
				launch.FixtureEnvironment = map[string]string{"FUSION_WORKER_FIXTURE": "ok"}
			case "write":
				launch.Writable = true
			case "session":
				launch.NativeSessionID += "other"
			case "claude":
				launch.ClaudeChannel = &ClaudeChannel{}
			case "grok":
				launch.GrokChannel = &GrokChannel{}
			}
			if c.valid(bad, launch) {
				t.Fatal("changed authority accepted")
			}
		})
	}
	if !c.acquire(r, spec) || c.acquire(r, spec) {
		t.Fatal("channel reused")
	}
	c.release()
	if c.acquire(r, spec) {
		t.Fatal("released channel reused")
	}
	for _, v := range []string{fmt.Sprintf("%v", c), fmt.Sprintf("%#v", c)} {
		if strings.Contains(v, root) || strings.Contains(v, cwd) {
			t.Fatal("private channel printed")
		}
	}
	raw, e := json.Marshal(spec)
	if e == nil || strings.Contains(string(raw), "codex-owner") {
		t.Fatal("trusted driver became JSON authority")
	}
	if raw, e := json.Marshal(c); e == nil || len(raw) != 0 {
		t.Fatal("channel serialized")
	}
}

func TestCodexChannelRejectsIncompleteAndStaleConstruction(t *testing.T) {
	driver := func(context.Context, io.ReadWriteCloser) error { return nil }
	for _, mode := range []string{"driver", "current", "stale", "zero", "runtime", "model", "root", "cwd", "session", "role", "root_slash"} {
		t.Run(mode, func(t *testing.T) {
			r, root, cwd, sid, runDriver, current := codexChannelRun(), "/private/worker", "/private/project", "session", driver, func() bool { return true }
			switch mode {
			case "driver":
				runDriver = nil
			case "current":
				current = nil
			case "stale":
				current = func() bool { return false }
			case "zero":
				r = store.StageRun{}
			case "runtime":
				r.Target.RuntimeVersion = "other"
			case "model":
				r.Target.ResolvedModel = "other"
			case "root":
				root = "relative"
			case "cwd":
				cwd = root
			case "session":
				sid = ""
			case "role":
				r.Role = "unknown"
			case "root_slash":
				root = "/"
			}
			if c, e := NewCodexChannel(r, root, cwd, sid, runDriver, current); e == nil || c != nil {
				t.Fatal("invalid channel accepted")
			}
		})
	}
	if (&CodexChannel{}).valid(codexChannelRun(), Spec{}) {
		t.Fatal("empty channel accepted")
	}
}

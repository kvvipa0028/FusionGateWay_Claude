package grok

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/quota"
	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

func adapterFixture(t *testing.T) (AdapterConfig, store.StageRun, managed.Spec, *atomic.Int64, *quota.Status) {
	t.Helper()
	f, _, in, run, pending, scheduler := storedGrokFixture(t, 3, false)
	pending.Cancel() // the adapter must mint its own grant only after current checks
	root, cwd := privateAdapterDir(t), privateAdapterDir(t)
	calls := &atomic.Int64{}
	config := AdapterConfig{Scheduler: *scheduler, Manager: f.manager, Executable: "/fixture/pinned-grok", Current: func(GateBinding) bool { return true }, Forwarder: fakeForwarder(func(context.Context, stageplan.ExecutionTarget, []byte) (ForwardResponse, error) {
		calls.Add(1)
		return ForwardResponse{}, ErrUnverified
	})}
	return config, run, managed.Spec{Root: root, Workspace: cwd, Timeout: 15 * time.Second, Input: []byte("fixture private prompt")}, calls, &in.Quota.Status
}
func privateAdapterDir(t *testing.T) string {
	t.Helper()
	p, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil || os.Chmod(p, 0700) != nil {
		t.Fatal("private dir failed")
	}
	return p
}

func TestGrokAdapterRequiresTrustedServices(t *testing.T) {
	config, _, _, _, _ := adapterFixture(t)
	for _, mode := range []string{"store", "inspect", "manager", "current", "forwarder", "path"} {
		c := config
		switch mode {
		case "store":
			c.Scheduler.Store = nil
		case "inspect":
			c.Scheduler.Inspect = nil
		case "manager":
			c.Manager = nil
		case "current":
			c.Current = nil
		case "forwarder":
			c.Forwarder = nil
		case "path":
			c.Executable = "relative"
		}
		if _, e := NewAdapter(c); e == nil {
			t.Fatal("untrusted service accepted", mode)
		}
	}
}
func TestGrokAdapterRejectsCallerLaunchAuthorityBeforeSpawnOrCalls(t *testing.T) {
	for _, mode := range []string{"args", "executable", "hash", "env", "validator", "session", "claude", "grok", "empty", "utf8", "nul", "oversize", "timeout", "write", "quota", "owner", "identity", "root", "workspace"} {
		t.Run(mode, func(t *testing.T) {
			c, r, in, calls, status := adapterFixture(t)
			switch mode {
			case "args":
				in.Args = []string{"--allow-all"}
			case "executable":
				in.Executable = "/bin/sh"
			case "hash":
				in.ExecutableHash = strings.Repeat("a", 64)
			case "env":
				in.FixtureEnvironment = map[string]string{"GROK_HOME": "/caller/config"}
			case "validator":
				in.ValidateOutcome = func([]byte) bool { return true }
			case "session":
				in.NativeSessionID = "caller-session"
			case "claude":
				in.ClaudeChannel = &managed.ClaudeChannel{}
			case "grok":
				in.GrokChannel = &managed.GrokChannel{}
			case "empty":
				in.Input = nil
			case "utf8":
				in.Input = []byte{0xff}
			case "nul":
				in.Input = []byte("private\x00prompt")
			case "oversize":
				in.Input = make([]byte, 65537)
			case "timeout":
				in.Timeout = 5 * time.Minute
			case "write":
				in.Writable = true
			case "quota":
				*status = quota.Unknown
			case "owner":
				r.Owner = "other"
			case "identity":
				r.Target.CredentialIdentity = "other"
			case "root":
				in.Root = "relative"
			case "workspace":
				in.Workspace = in.Root
			}
			a, e := NewAdapter(c)
			if e != nil {
				t.Fatal(e)
			}
			if h, e := a.Start(context.Background(), r, in); e == nil || h != nil {
				t.Fatal("unsafe launch accepted")
			}
			if calls.Load() != 0 {
				t.Fatal("rejected startup used upstream")
			}
			if _, e := os.Stat(filepath.Join(in.Root, "launch.json")); !os.IsNotExist(e) {
				t.Fatal("rejected startup spawned")
			}
			b, e := c.Scheduler.Store.Budget(r.TaskID)
			if e != nil || b.UsedCalls != 0 {
				t.Fatal("rejected startup spent", e)
			}
		})
	}
}
func TestGrokAdapterProbeResumeAndObservationStayExplicit(t *testing.T) {
	c, r, _, _, _ := adapterFixture(t)
	a, e := NewAdapter(c)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := a.Probe(context.Background()); e == nil {
		t.Fatal("missing Native pin admitted")
	}
	if h, e := a.Resume(context.Background(), r); h != nil || e != ErrUnsupported {
		t.Fatal("unverified Native resume accepted")
	}
	if _, text, e := a.Observation(r.ID, r.Generation); e == nil || text != "" {
		t.Fatal("starting output exposed")
	}
	if a.VerifyStop(policy.StopProof{}) {
		t.Fatal("foreign stop proof trusted")
	}
}

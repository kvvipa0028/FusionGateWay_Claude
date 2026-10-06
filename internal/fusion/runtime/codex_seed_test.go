package runtime

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/stageplan"
)

const seedThread = "12345678-1234-1234-1234-123456789abc"
const seedRollout = "sessions/2026/10/06/rollout-2026-10-06T19-09-43-" + seedThread + ".jsonl"

// codexResumeDirs returns resolved private dirs: CanonicalDirectory rejects
// unresolved /var spellings on macOS.
func codexResumeDirs(t *testing.T) (root, cwd string) {
	t.Helper()
	return pdir(t), pdir(t)
}

func tryResumeSeedChannel(t *testing.T, threadID string, files map[string][]byte) error {
	t.Helper()
	r := codexChannelRun()
	r.Target.Route = stageplan.RouteRef{ID: "fixture-codex-route", Revision: 1}
	r.Target.LockEnforcement = stageplan.ControlledCalls
	r.Target.Effort.RequestedMode = stageplan.EffortExplicit
	claims := policy.Claims{TaskID: r.TaskID, RunID: r.ID, Role: r.Role, Attempt: r.Attempt, PlanRevision: r.PlanRevision, Generation: r.Generation, ProjectID: "fixture-project", Audience: policy.ModelAudience}
	m := policy.NewManager("fixture-management", func(c policy.Claims) bool { return c == claims }, nil)
	grant, e := m.PrepareModel(claims, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	root, cwd := codexResumeDirs(t)
	ch, e := NewCodexResumeChannel(r, root, cwd, "fixture-resume", func(context.Context, io.ReadWriteCloser) error { return nil }, func() bool { return true }, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) }), grant, threadID, files)
	if e == nil {
		ch.Close()
	}
	return e
}

func TestCodexResumeChannelValidatesSeedShape(t *testing.T) {
	cases := map[string]map[string][]byte{
		"not_rollout_name":  {"sessions/2026/10/06/notes.txt": []byte("{}")},
		"foreign_thread":    {"sessions/2026/10/06/rollout-2026-10-06T19-09-43-87654321-4321-4321-4321-210987654321.jsonl": []byte("{}")},
		"traversal":         {"sessions/2026/10/07/../../../rollout-x.jsonl": []byte("{}")},
		"empty_bytes":       {seedRollout: {}},
		"nul_byte":          {seedRollout: []byte("{\"a\":\"\x00\"}")},
		"invalid_utf8":      {seedRollout: []byte("{\"\xff\"}")},
		"too_many_files":    {"sessions/2026/10/06/rollout-a-" + seedThread + ".jsonl": []byte("{}"), "sessions/2026/10/06/rollout-b-" + seedThread + ".jsonl": []byte("{}"), "sessions/2026/10/06/rollout-c-" + seedThread + ".jsonl": []byte("{}"), "sessions/2026/10/06/rollout-d-" + seedThread + ".jsonl": []byte("{}"), "sessions/2026/10/06/rollout-e-" + seedThread + ".jsonl": []byte("{}")},
		"none":              {},
		"unrelated_subtree": {"history/rollout-" + seedThread + ".jsonl": []byte("{}")},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			if e := tryResumeSeedChannel(t, seedThread, files); e == nil {
				t.Fatal("invalid seed accepted")
			}
		})
	}
	if e := tryResumeSeedChannel(t, "fixture-thread", map[string][]byte{seedRollout: []byte("{}")}); e == nil {
		t.Fatal("non-uuid thread accepted")
	}
	if e := tryResumeSeedChannel(t, seedThread, map[string][]byte{seedRollout: []byte("{}")}); e != nil {
		t.Fatal("valid seed refused", e)
	}
}

func TestCodexSeedInstallsOnceIntoFreshHome(t *testing.T) {
	if e := tryResumeSeedChannel(t, seedThread, map[string][]byte{seedRollout: []byte("{}")}); e != nil {
		t.Fatal("valid seed refused", e)
	}
	// A second construction of the same shape gives an installable channel.
	ch, seedCwd := resumeSeedChannelForInstall(t)
	home := t.TempDir()
	if e := os.Chmod(home, 0700); e != nil {
		t.Fatal(e)
	}
	spec := Spec{Root: filepath.Dir(home), Workspace: seedCwd}
	if e := ch.seed.install(home, spec); e != nil {
		t.Fatal(e)
	}
	data, e := os.ReadFile(filepath.Join(home, seedRollout))
	if e != nil || string(data) != "{}" {
		t.Fatal("rollout not installed", e)
	}
	if info, e := os.Stat(filepath.Join(home, seedRollout)); e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("rollout permissions", e)
	}
	if e := ch.seed.install(home, spec); e == nil {
		t.Fatal("seed reused")
	}
	if e := ch.seed.install(home, Spec{Root: filepath.Dir(home), Workspace: t.TempDir()}); e == nil {
		t.Fatal("seed accepted foreign workspace")
	}
}

// resumeSeedChannelForInstall exposes a channel whose private seed is still
// charged; only this in-package install test may consume it.
func resumeSeedChannelForInstall(t *testing.T) (*CodexChannel, string) {
	t.Helper()
	r := codexChannelRun()
	r.Target.Route = stageplan.RouteRef{ID: "fixture-codex-route", Revision: 1}
	r.Target.LockEnforcement = stageplan.ControlledCalls
	r.Target.Effort.RequestedMode = stageplan.EffortExplicit
	claims := policy.Claims{TaskID: r.TaskID, RunID: r.ID, Role: r.Role, Attempt: r.Attempt, PlanRevision: r.PlanRevision, Generation: r.Generation, ProjectID: "fixture-project", Audience: policy.ModelAudience}
	m := policy.NewManager("fixture-management", func(c policy.Claims) bool { return c == claims }, nil)
	grant, e := m.PrepareModel(claims, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	root, cwd := codexResumeDirs(t)
	ch, e := NewCodexResumeChannel(r, root, cwd, "fixture-resume", func(context.Context, io.ReadWriteCloser) error { return nil }, func() bool { return true }, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) }), grant, seedThread, map[string][]byte{seedRollout: []byte("{}")})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { ch.Close() })
	return ch, cwd
}

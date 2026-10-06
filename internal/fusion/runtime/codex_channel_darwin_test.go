//go:build darwin

package runtime

import (
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodexSandboxExactPreferencesAndPrivateEnvironment(t *testing.T) {
	root, cwd := pdir(t), pdir(t)
	c, e := NewCodexChannel(codexChannelRun(), root, cwd, "session", func(context.Context, io.ReadWriteCloser) error { return nil }, func() bool { return true })
	if e != nil {
		t.Fatal(e)
	}
	spec := Spec{Root: root, Workspace: cwd, Executable: "/fixture/native-codex", ExecutableHash: CodexExecutableSHA256, Args: []string{"app-server", "--listen", "stdio://", "--strict-config"}, NativeSessionID: "session", CodexChannel: c}
	profile, env, e := sandbox(spec)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(profile, codexPreferencesRules) || strings.Contains(profile, "(allow network") || strings.Contains(profile, "(allow process-fork") || strings.Contains(profile, "(allow user-preference-write") || strings.Contains(profile, "SecurityServer") {
		t.Fatal("bootstrap authority widened")
	}
	vars := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		vars[k] = v
	}
	if vars["CODEX_HOME"] != filepath.Join(root, "config", "codex") || vars["HOME"] != filepath.Join(root, "home") || vars["CFFIXED_USER_HOME"] != vars["HOME"] || len(vars) != 8 {
		t.Fatal("ambient environment inherited")
	}
	if info, e := os.Stat(vars["CODEX_HOME"]); e != nil || info.Mode().Perm() != 0700 {
		t.Fatal("home not private")
	}
	for _, mode := range []string{"empty", "argv", "fixture", "other_channel", "write-escape"} {
		t.Run(mode, func(t *testing.T) {
			s := spec
			s.Root, s.Workspace = pdir(t), pdir(t)
			s.CodexChannel, _ = NewCodexChannel(codexChannelRun(), s.Root, s.Workspace, "session", func(context.Context, io.ReadWriteCloser) error { return nil }, func() bool { return true })
			switch mode {
			case "empty":
				s.CodexChannel = &CodexChannel{}
			case "argv":
				s.Args = []string{"app-server", "-c", "ignore_host_managed=true"}
			case "fixture":
				s.FixtureEnvironment = map[string]string{"FUSION_WORKER_FIXTURE": "ok"}
			case "other_channel":
				s.GrokChannel = &GrokChannel{}
			case "write-escape":
				// Bounded writers are legal; only an escaping write scope is refused.
				s.Writable = true
				s.WritePaths = []string{"../escape"}
			}
			if _, _, e := sandbox(s); e == nil {
				t.Fatal("caller authority accepted")
			}
		})
	}
	plain, _, e := sandbox(Spec{Root: pdir(t), Workspace: pdir(t), Executable: "/fixture/worker"})
	if e != nil || strings.Contains(plain, "(allow mach-lookup") || strings.Contains(plain, "user-preference-read") || strings.Contains(plain, "ipc-posix-shm-read") {
		t.Fatal("generic worker permissions changed")
	}
}

func TestCodexPreferencesKernelBoundariesWithPositiveControls(t *testing.T) {
	source, e := filepath.Abs("../../../tests/fusion/fixtures/codex-preferences-probe.c")
	if e != nil {
		t.Fatal(e)
	}
	exe := filepath.Join(pdir(t), "preferences-probe")
	args := []string{"-Wall", "-Wextra", "-Werror", source, "-framework", "CoreFoundation", "-o", exe}
	if sdk := os.Getenv("SDKROOT"); sdk != "" {
		args = append([]string{"-isysroot", sdk}, args...)
	}
	if b, e := exec.Command("/usr/bin/clang", args...).CombinedOutput(); e != nil {
		t.Fatalf("compile probe: %v %s", e, b)
	}
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	go func() {
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			c.Close()
		}
	}()
	port := strings.Split(l.Addr().String(), ":")[1]
	forbidden := filepath.Join(pdir(t), "management-synthetic.txt")
	if e := os.WriteFile(forbidden, []byte("synthetic"), 0600); e != nil {
		t.Fatal(e)
	}
	control := exec.Command(exe, "--control", forbidden, port)
	control.Env = []string{}
	if b, e := control.CombinedOutput(); e != nil || string(b) != "fixture-completed\n" {
		t.Fatalf("positive control: %v %s", e, b)
	}
	root, cwd := pdir(t), pdir(t)
	c, _ := NewCodexChannel(codexChannelRun(), root, cwd, "session", func(context.Context, io.ReadWriteCloser) error { return nil }, func() bool { return true })
	spec := Spec{Root: root, Workspace: cwd, Executable: exe, ExecutableHash: CodexExecutableSHA256, Args: []string{"app-server", "--listen", "stdio://", "--strict-config"}, NativeSessionID: "session", CodexChannel: c}
	profile, env, e := sandbox(spec)
	if e != nil {
		t.Fatal(e)
	}
	// Substitute only probe argv after obtaining the exact profile. Supervisor
	// would reject this fake executable hash/argv; this is a kernel profile test.
	spec.Args = []string{"--confined", forbidden, port}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := platformCommand(profile, spec)
	cmd.Dir = cwd
	cmd.Env = env
	stop := context.AfterFunc(ctx, func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	})
	defer stop()
	if b, e := cmd.CombinedOutput(); e != nil || string(b) != "fixture-completed\n" {
		t.Fatalf("kernel boundary: %v %s", e, b)
	}
	if b, e := os.ReadFile(filepath.Join(root, "config", "codex", "private-probe.txt")); e != nil || string(b) != "fixture" {
		t.Fatal("private write positive control missing")
	}
	if _, e := os.Stat(filepath.Join(cwd, "readonly-effect.txt")); !os.IsNotExist(e) {
		t.Fatal("readonly workspace changed")
	}
	if b, e := os.ReadFile(forbidden); e != nil || string(b) != "synthetic" {
		t.Fatal("outside management fixture changed")
	}
}

//go:build darwin

package runtime

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
)

func TestGrokSandboxPrivateConfigAndOnlyOwnedNetwork(t *testing.T) {
	ch, p, _, _ := grokChannelFixture(t)
	secret, e := p.Secret()
	if e != nil {
		t.Fatal(e)
	}
	root, cwd := pdir(t), pdir(t)
	spec := Spec{Root: root, Workspace: cwd, Executable: "/fixture/only-native", GrokChannel: ch}
	profile, env, e := sandbox(spec)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(profile, `localhost:`+ch.port) || strings.Contains(profile, "(allow mach-lookup") || strings.Contains(profile, "(allow process-fork") || strings.Contains(profile, "(allow file-write* (subpath "+`"`+cwd) {
		t.Fatal("sandbox authority widened")
	}
	vars := map[string]string{}
	for _, v := range env {
		k, value, _ := strings.Cut(v, "=")
		vars[k] = value
		if strings.Contains(v, secret) {
			t.Fatal("grant should stay in private config")
		}
	}
	if vars["HOME"] != filepath.Join(root, "home") || vars["GROK_HOME"] != filepath.Join(root, "config", "grok") || vars["ANTHROPIC_API_KEY"] != "" {
		t.Fatal("private family env changed")
	}
	path := filepath.Join(vars["GROK_HOME"], "config.toml")
	info, e := os.Lstat(path)
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("config not private")
	}
	// Decode with actual TOML keys to assert executable configuration, not string formatting.
	var values map[string]any
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if e := toml.Unmarshal(raw, &values); e != nil {
		t.Fatal("private TOML invalid", e)
	}
	models := values["models"].(map[string]any)
	model := values["model"].(map[string]any)["fusion"].(map[string]any)
	if models["default"] != "fusion" || models["session_summary"] != "fusion" || model["model"] != "fixture-model" || model["base_url"] != ch.endpoint || model["api_key"] != secret || model["max_retries"] != int64(2) {
		t.Fatal("private frozen config changed")
	}
	if values["features"].(map[string]any)["turn_summary"] != false || values["cli"].(map[string]any)["auto_update"] != false {
		t.Fatal("extra model/update traffic enabled")
	}
}

func TestGrokSandboxRejectsInjectedFixtureAndAmbiguousChannel(t *testing.T) {
	grok, _, _, _ := grokChannelFixture(t)
	claude, _, _, _ := channelFixture(t)
	for _, mode := range []string{"fixture", "dual"} {
		spec := Spec{Root: pdir(t), Workspace: pdir(t), Executable: "/fixture/native", GrokChannel: grok}
		if mode == "fixture" {
			spec.FixtureEnvironment = map[string]string{"FUSION_WORKER_FIXTURE": "ok"}
		} else {
			spec.ClaudeChannel = claude
		}
		if _, _, e := sandbox(spec); e == nil {
			t.Fatal("invalid launch controls accepted", mode)
		}
	}
}

// Direct native C probes exercise the exact Grok profile with positive controls.
// They are profile tests, not Grok model/identity admission or StopProof tests.
func TestGrokSandboxKernelBoundariesWithPositiveControls(t *testing.T) {
	ch, _, _, _ := grokChannelFixture(t)
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	go func() {
		for {
			c, e := listener.Accept()
			if e != nil {
				return
			}
			c.Close()
		}
	}()
	other := strings.Split(listener.Addr().String(), ":")[1]
	exe := compileProbe(t)
	control := exec.Command(exe, "--channel-control", ch.port, other)
	control.Env = []string{}
	if b, e := control.CombinedOutput(); e != nil || string(b) != "fixture-completed\n" {
		t.Fatal("positive network control failed", e)
	}
	root, cwd, outside := pdir(t), pdir(t), pdir(t)
	forbidden := filepath.Join(outside, "forbidden.txt")
	if os.WriteFile(filepath.Join(cwd, "fixture.txt"), []byte("owned\n"), 0600) != nil || os.WriteFile(forbidden, []byte("forbidden\n"), 0600) != nil {
		t.Fatal("positive file control failed")
	}
	spec := Spec{Root: root, Workspace: cwd, Executable: exe, GrokChannel: ch, Args: []string{"--grok-channel", ch.port, other, forbidden}}
	profile, env, e := sandbox(spec)
	if e != nil {
		t.Fatal(e)
	}
	cmd := platformCommand(profile, spec)
	cmd.Dir = cwd
	cmd.Env = env
	cmd.WaitDelay = time.Second
	if b, e := cmd.CombinedOutput(); e != nil || string(b) != "fixture-completed\n" {
		t.Fatal("Grok kernel boundary failed", e)
	}
	if _, e := os.Stat(filepath.Join(cwd, "readonly-effect.txt")); !os.IsNotExist(e) {
		t.Fatal("readonly workspace wrote")
	}
	if b, e := os.ReadFile(forbidden); e != nil || string(b) != "forbidden\n" {
		t.Fatal("outside file changed")
	}
	if b, e := os.ReadFile(filepath.Join(root, "config", "grok", "probe-private.txt")); e != nil || string(b) != "synthetic private write" {
		t.Fatal("private write positive control missing")
	}
}

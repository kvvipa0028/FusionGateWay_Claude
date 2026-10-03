//go:build darwin

package runtime

import (
	"fmt"

	"github.com/pelletier/go-toml/v2"
	"github.com/yetone/magpie/internal/fusion/workspace"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func platformAvailable() bool {
	i, e := os.Stat("/usr/bin/sandbox-exec")
	return e == nil && i.Mode().IsRegular()
}
func platformCommand(profile string, spec Spec) *exec.Cmd {
	args := append([]string{"-p", profile, spec.Executable}, spec.Args...)
	cmd := exec.Command("/usr/bin/sandbox-exec", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}

func sandbox(spec Spec) (string, []string, error) {
	channel, channelErr := spec.modelChannel()
	if channelErr != nil || channel != nil && len(spec.FixtureEnvironment) != 0 {
		return "", nil, ErrLaunch
	}
	if workspace.PrivateState(spec.Root) != nil || workspace.PrivateState(spec.Workspace) != nil || spec.Root == spec.Workspace || strings.HasPrefix(spec.Workspace, spec.Root+"/") || strings.HasPrefix(spec.Root, spec.Workspace+"/") {
		return "", nil, ErrLaunch
	}
	// Default deny includes network, Mach services (Keychain/launchd), IPC,
	// and process-fork. A managed worker cannot create escaping descendants.
	profile := "(version 1)\n(deny default)\n(allow signal (target self))\n(allow sysctl-read (sysctl-name-prefix \"hw.\") (sysctl-name \"kern.osrelease\") (sysctl-name \"kern.osversion\") (sysctl-name \"kern.ostype\") (sysctl-name \"kern.bootargs\"))\n(allow file-read-metadata)\n(allow file-read* (literal \"/\"))\n"
	for _, p := range []string{"/System/Library", "/usr/lib", "/Library/Apple/System/Library"} {
		profile += fmt.Sprintf("(allow file-read* file-map-executable (subpath %s))\n", strconv.Quote(p))
	}
	// System ICU enumerates timezone IDs from both data directories during
	// Native startup. Permit data reads only, without broader /usr or /var access.
	for _, p := range []string{"/usr/share/icu", "/private/var/db/timezone"} {
		profile += fmt.Sprintf("(allow file-read* (subpath %s))\n", strconv.Quote(p))
	}
	for _, p := range []string{spec.Root, spec.Workspace} {
		profile += fmt.Sprintf("(allow file-read* (subpath %s))\n", strconv.Quote(p))
	}
	profile += fmt.Sprintf("(allow process-exec (literal %s))\n(allow file-read* file-map-executable (literal %s))\n", strconv.Quote(spec.Executable), strconv.Quote(spec.Executable))
	profile += "(allow file-read* file-write* (literal \"/dev/null\"))\n(allow file-read* (literal \"/dev/urandom\"))\n"
	for _, p := range []string{"home", "config", "cache", "data", "tmp"} {
		profile += fmt.Sprintf("(allow file-write* (subpath %s))\n", strconv.Quote(filepath.Join(spec.Root, p)))
	}
	if spec.Writable {
		profile += fmt.Sprintf("(allow file-write* (subpath %s))\n", strconv.Quote(spec.Workspace))
	}
	env := []string{"PATH=/usr/bin:/bin", "HOME=" + filepath.Join(spec.Root, "home"), "XDG_CONFIG_HOME=" + filepath.Join(spec.Root, "config"), "XDG_CACHE_HOME=" + filepath.Join(spec.Root, "cache"), "XDG_DATA_HOME=" + filepath.Join(spec.Root, "data"), "TMPDIR=" + filepath.Join(spec.Root, "tmp")}
	for _, p := range []string{"home", "config", "cache", "data", "tmp"} {
		if e := os.Mkdir(filepath.Join(spec.Root, p), 0700); e != nil {
			return "", nil, ErrLaunch
		}
	}
	for k, v := range spec.FixtureEnvironment {
		switch k {
		case "FUSION_WORKER_FIXTURE", "FUSION_FIXTURE_FORBIDDEN", "FUSION_FIXTURE_OUTSIDE", "FUSION_FIXTURE_NETWORK":
		default:
			return "", nil, ErrLaunch
		}
		if strings.ContainsRune(v, 0) || len(v) > 4096 {
			return "", nil, ErrLaunch
		}
		env = append(env, k+"="+v)
	}
	if c := spec.ClaudeChannel; c != nil {
		secret, e := c.grant.Secret()
		if e != nil {
			return "", nil, ErrLaunch
		}
		env = append(env,
			"USERPROFILE="+filepath.Join(spec.Root, "home"),
			"CLAUDE_CONFIG_DIR="+filepath.Join(spec.Root, "config", "claude"),
			"CLAUDE_CODE_TMPDIR="+filepath.Join(spec.Root, "tmp"),
			"ANTHROPIC_API_KEY="+secret, "ANTHROPIC_AUTH_TOKEN="+secret,
			"ANTHROPIC_BASE_URL="+c.endpoint, "ANTHROPIC_MODEL="+c.target.RequestedModel,
			"DISABLE_UPDATES=1", "DISABLE_TELEMETRY=1", "DISABLE_ERROR_REPORTING=1",
			"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1", "DISABLE_COMPACT=1",
			"DO_NOT_TRACK=1", "API_TIMEOUT_MS=5000")
	}
	if channel != nil {
		// Seatbelt localhost includes both families; the shared lease owns both.
		profile += fmt.Sprintf("(allow network-outbound (remote tcp %s))\n", strconv.Quote("localhost:"+channel.port))
	}
	if c := spec.GrokChannel; c != nil {
		secret, e := c.grant.Secret()
		if e != nil {
			return "", nil, ErrLaunch
		}
		home := filepath.Join(spec.Root, "config", "grok")
		if os.Mkdir(home, 0700) != nil {
			return "", nil, ErrLaunch
		}
		config := map[string]any{
			"model":    map[string]any{"fusion": map[string]any{"model": c.target.RequestedModel, "base_url": c.endpoint, "api_key": secret, "max_retries": 2, "rate_limit_retry_threshold": 2}},
			"models":   map[string]any{"default": "fusion", "session_summary": "fusion"},
			"features": map[string]any{"turn_summary": false}, "cli": map[string]any{"auto_update": false},
		}
		raw, e := toml.Marshal(config)
		if e != nil {
			return "", nil, ErrLaunch
		}
		file, e := os.OpenFile(filepath.Join(home, "config.toml"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return "", nil, ErrLaunch
		}
		_, writeErr := file.Write(raw)
		syncErr := file.Sync()
		closeErr := file.Close()
		if writeErr != nil || syncErr != nil || closeErr != nil {
			return "", nil, ErrLaunch
		}
		if c.seed != nil && c.seed.install(home, spec) != nil {
			return "", nil, ErrLaunch
		}
		env = append(env, "GROK_HOME="+home, "LANG=en_US.UTF-8", "DO_NOT_TRACK=1", "RUST_LOG=error")
	}
	return profile, env, nil
}

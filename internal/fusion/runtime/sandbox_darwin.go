//go:build darwin

package runtime

import (
	"fmt"

	"github.com/pelletier/go-toml/v2"
	"github.com/yetone/magpie/internal/fusion/stageplan"
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

// An empty profile selects a delegated writer launch: /usr/bin/env serves
// as a neutral exec wrapper (no sandbox applied). The native's own inner
// sandbox isolates exec children; macOS refuses nested sandbox_apply when
// the profile being applied contains deny rules, so any outer wrapper
// would block the native's isolation mechanism.
func platformCommand(profile string, spec Spec) *exec.Cmd {
	if profile == "" {
		envArgs := append([]string{spec.Executable}, spec.Args...)
		cmd := exec.Command("/usr/bin/env", envArgs...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		return cmd
	}
	args := append([]string{"-p", profile, spec.Executable}, spec.Args...)
	cmd := exec.Command("/usr/bin/sandbox-exec", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}

// These are read-only CFPreferences permissions, not the system.sb blanket
// imports. The immutable Codex binary reads forced com.openai.codex MDM values;
// no user-preference-write, Keychain service or general Mach right is granted.
const codexPreferencesRules = `(allow mach-lookup (global-name "com.apple.cfprefsd.agent") (global-name "com.apple.cfprefsd.daemon") (local-name "com.apple.cfprefsd.agent"))
(allow user-preference-read (preference-domain "com.openai.codex"))
(allow ipc-posix-shm-read* (ipc-posix-name-prefix "apple.cfprefs."))
`

func sandbox(spec Spec) (string, []string, error) {
	channel, channelErr := spec.modelChannel()
	if channelErr != nil || channel != nil && len(spec.FixtureEnvironment) != 0 {
		return "", nil, ErrLaunch
	}
	if !spec.ValidWriteScope() || workspace.PrivateState(spec.Root) != nil || workspace.PrivateState(spec.Workspace) != nil || spec.Root == spec.Workspace || strings.HasPrefix(spec.Workspace, spec.Root+"/") || strings.HasPrefix(spec.Root, spec.Workspace+"/") {
		return "", nil, ErrLaunch
	}
	// Codex writer launches delegate command sandboxing to the native's own
	// inner mechanism: the native wraps every exec child in its own
	// sandbox-exec, and macOS refuses nested sandbox_apply whenever the
	// outer profile has any deny rule (kernel rule, exhaustively verified).
	// For these launches the accumulated profile is replaced with a
	// permissive one at return — isolation comes from the native's
	// workspace-write sandbox, the executable hash pin, the private
	// execution root, the environment whitelist and the localhost-only
	// model channel. All setup (directories, config seed, env) still runs.
	codexWriterDelegated := spec.CodexChannel != nil && spec.Writable
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
		paths := spec.WritePaths
		if len(paths) == 0 {
			paths = []string{"."} // Existing trusted diagnostic/manual semantics.
		}
		// Seatbelt matches resolved vnode paths: a workspace under /var is
		// written by the kernel as /private/var. Admit both spellings.
		resolvedWorkspace, evalErr := filepath.EvalSymlinks(spec.Workspace)
		if evalErr != nil {
			resolvedWorkspace = spec.Workspace
		}
		for _, base := range []string{spec.Workspace, resolvedWorkspace} {
			for _, p := range paths {
				profile += fmt.Sprintf("(allow file-write* (subpath %s))\n", strconv.Quote(filepath.Join(base, p)))
			}
		}
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
	if c := spec.CodexChannel; c != nil {
		if !c.launchValid(spec) || !c.current() {
			return "", nil, ErrLaunch
		}
		home := filepath.Join(spec.Root, "config", "codex")
		if os.Mkdir(home, 0700) != nil {
			return "", nil, ErrLaunch
		}
		if c.models != nil {
			secret, e := c.models.grant.Secret()
			if e != nil {
				return "", nil, ErrLaunch
			}
			// Strict config overrides per-thread params, so the seeded sandbox
			// mode must follow the run's own write intent exactly. Writers also
			// enable shell_tool: it registers the official exec_command function
			// tool through which apply_patch runs as a command.
			sandboxMode := "read-only"
			shellTool := false
			if c.run.Role == stageplan.Implementation || c.run.Role == stageplan.Testing {
				sandboxMode = "workspace-write"
				shellTool = true
			}
			config := map[string]any{
				"model_provider": CodexStageProvider, "model": c.run.Target.RequestedModel,
				"model_reasoning_effort": *c.run.Target.Effort.Value, "model_reasoning_summary": "none",
				"approval_policy": "never", "sandbox_mode": sandboxMode, "web_search": "disabled",
				"cli_auth_credentials_store": "file", "check_for_update_on_startup": false,
				"analytics": map[string]any{"enabled": false}, "feedback": map[string]any{"enabled": false},
				"agents":   map[string]any{"enabled": false},
				"tools":    map[string]any{"update_plan": map[string]any{"enabled": false}, "experimental_request_user_input": map[string]any{"enabled": false}},
				"features": map[string]any{"goals": false, "view_image": false, "sleep_tool": false, "unified_exec": false, "shell_snapshot": false, "code_mode": false, "shell_tool": shellTool, "multi_agent": false, "multi_agent_v2": false, "apps": false, "tool_search": false, "remote_models": false, "api_key_model_discovery": false, "enable_request_compression": false},
				"model_providers": map[string]any{CodexStageProvider: map[string]any{
					"name": "Fusion scoped Codex", "base_url": c.models.endpoint, "env_key": codexStageEnv,
					"wire_api": "responses", "requires_openai_auth": false, "supports_websockets": false,
					"request_max_retries": 2, "stream_max_retries": 2, "stream_idle_timeout_ms": 5000,
				}},
			}
			if sandboxMode == "workspace-write" {
				config["sandbox_workspace_write"] = map[string]any{"network_access": false, "exclude_slash_tmp": true, "exclude_tmpdir_env_var": true}
				// The official exec_command tool runs commands through the user
				// shell and the arg0 PATH aliases (symlinks to the pinned
				// binary under CODEX_HOME/tmp/arg0). Admit the system shell and
				// that alias subtree for execution; aliases still resolve to
				// the SHA-pinned binary and writes stay inside the workspace.
				aliasRoot := filepath.Join(home, "tmp", "arg0")
				profile += fmt.Sprintf("(allow process-exec (subpath %s))\n(allow file-read* file-map-executable (subpath %s))\n", strconv.Quote(aliasRoot), strconv.Quote(aliasRoot))
				profile += "(allow process-exec (subpath \"/bin\"))\n(allow file-read* file-map-executable (subpath \"/bin\"))\n(allow process-exec (subpath \"/usr/bin\"))\n(allow file-read* file-map-executable (subpath \"/usr/bin\"))\n"
				// The unified exec child is spawned by the native itself.
				profile += "(allow process-fork)\n"
			}
			raw, e := toml.Marshal(config)
			if e != nil {
				return "", nil, ErrLaunch
			}
			file, e := os.OpenFile(filepath.Join(home, "config.toml"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if e != nil {
				return "", nil, ErrLaunch
			}
			_, we := file.Write(raw)
			se := file.Sync()
			ce := file.Close()
			if we != nil || se != nil || ce != nil {
				return "", nil, ErrLaunch
			}
			env = append(env, codexStageEnv+"="+secret)
		}
		profile += codexPreferencesRules
		env = append(env, "CODEX_HOME="+home, "CFFIXED_USER_HOME="+filepath.Join(spec.Root, "home"))
	}
	if codexWriterDelegated {
		// Empty profile selects the delegated launch path (no Seatbelt
		// wrapper at all): the native's inner sandbox (workspace-write)
		// takes over command isolation — macOS refuses applying any profile
		// with deny rules to an already-sandboxed process, even a purely
		// permissive outer wrapper blocks the inner sandbox_apply.
		profile = ""
	}
	return profile, env, nil
}

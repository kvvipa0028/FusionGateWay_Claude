//go:build darwin

package runtime

import (
	"fmt"
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
	if workspace.PrivateState(spec.Root) != nil || workspace.PrivateState(spec.Workspace) != nil || spec.Root == spec.Workspace || strings.HasPrefix(spec.Workspace, spec.Root+"/") || strings.HasPrefix(spec.Root, spec.Workspace+"/") {
		return "", nil, ErrLaunch
	}
	// Default deny includes network, Mach services (Keychain/launchd), IPC,
	// and process-fork. A managed worker cannot create escaping descendants.
	profile := "(version 1)\n(deny default)\n(allow signal (target self))\n(allow sysctl-read (sysctl-name-prefix \"hw.\") (sysctl-name \"kern.osrelease\") (sysctl-name \"kern.osversion\") (sysctl-name \"kern.ostype\") (sysctl-name \"kern.bootargs\"))\n(allow file-read-metadata)\n(allow file-read* (literal \"/\"))\n"
	for _, p := range []string{"/System/Library", "/usr/lib", "/Library/Apple/System/Library"} {
		profile += fmt.Sprintf("(allow file-read* file-map-executable (subpath %s))\n", strconv.Quote(p))
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
	return profile, env, nil
}

// Package isolation distinguishes Fusion development from the upstream build.
package isolation

import (
	"errors"
	"os"
	"path/filepath"
)

var ErrDisabled = errors.New("disabled in isolated Fusion development build")

// Enabled is the engineering workflow master switch. It is off until the
// workflow admission and authenticated API are implemented.
func Enabled() bool { return false }

// ValidateEnvironment is checked before the application starts.
func ValidateEnvironment() error {
	if !Development {
		return nil
	}
	r := os.Getenv("FUSION_STATE_ROOT")
	if !filepath.IsAbs(r) || filepath.Clean(r) != r {
		return errors.New("use the Fusion isolated launcher")
	}
	for parent := r; ; parent = filepath.Dir(parent) {
		if _, err := os.Lstat(filepath.Join(parent, ".git")); err == nil || !os.IsNotExist(err) {
			return errors.New("Fusion private state must be outside a Git workspace")
		}
		if filepath.Dir(parent) == parent {
			break
		}
	}
	for _, key := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL",
		"OPENAI_API_KEY", "GROK_API_KEY", "GLM_API_KEY", "ZHIPUAI_API_KEY", "CODEX_HOME", "CLAUDE_CONFIG_DIR"} {
		if os.Getenv(key) != "" {
			return errors.New("Fusion startup must not inherit provider credentials or client configuration")
		}
	}
	if err := privateDirectory(r); err != nil {
		return err
	}
	for key, folder := range map[string]string{"HOME": "runtime-home", "XDG_CONFIG_HOME": "config", "XDG_CACHE_HOME": "cache", "XDG_DATA_HOME": "data", "TMPDIR": "tmp"} {
		want := filepath.Join(r, folder)
		if os.Getenv(key) != want {
			return errors.New("Fusion requires isolated HOME/XDG/TMPDIR")
		}
		if err := privateDirectory(want); err != nil {
			return err
		}
	}
	return nil
}

func privateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 || !ownedByCurrentUser(info) {
		return errors.New("Fusion state must be private directories owned by the current user")
	}
	return nil
}

//go:build fusion

package isolation

import (
	"os"
	"path/filepath"
	"testing"
)

func isolatedEnvironment(t *testing.T) string {
	t.Helper()
	r := t.TempDir()
	if err := os.Chmod(r, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FUSION_STATE_ROOT", r)
	for key, folder := range map[string]string{"HOME": "runtime-home", "XDG_CONFIG_HOME": "config", "XDG_CACHE_HOME": "cache", "XDG_DATA_HOME": "data", "TMPDIR": "tmp"} {
		p := filepath.Join(r, folder)
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
		t.Setenv(key, p)
	}
	return r
}

func TestFusionRejectsUnisolatedHomeBeforeStarting(t *testing.T) {
	isolatedEnvironment(t)
	t.Setenv("HOME", t.TempDir())
	if err := ValidateEnvironment(); err == nil {
		t.Fatal("unisolated HOME accepted")
	}
}

func TestFusionRejectsMissingStateRoot(t *testing.T) {
	isolatedEnvironment(t)
	t.Setenv("FUSION_STATE_ROOT", "")
	if err := ValidateEnvironment(); err == nil {
		t.Fatal("missing isolation root accepted")
	}
}

func TestFusionRejectsPublicOrLinkedState(t *testing.T) {
	r := isolatedEnvironment(t)
	if err := os.Chmod(filepath.Join(r, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ValidateEnvironment(); err == nil {
		t.Fatal("public config accepted")
	}
	if err := os.Chmod(filepath.Join(r, "config"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(r, "config")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(r, "config")); err != nil {
		t.Fatal(err)
	}
	if err := ValidateEnvironment(); err == nil {
		t.Fatal("linked config accepted")
	}
}

func TestFusionAcceptsPrivateIsolatedState(t *testing.T) {
	isolatedEnvironment(t)
	if err := ValidateEnvironment(); err != nil {
		t.Fatal(err)
	}
}

func TestFusionRejectsStateInsideGitWorkspace(t *testing.T) {
	r := isolatedEnvironment(t)
	if err := os.Mkdir(filepath.Join(r, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ValidateEnvironment(); err == nil {
		t.Fatal("state inside Git workspace accepted")
	}
}

func TestFusionRejectsAmbientProviderCredentials(t *testing.T) {
	isolatedEnvironment(t)
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "fixture-ambient-secret")
	if err := ValidateEnvironment(); err == nil {
		t.Fatal("ambient provider credentials accepted")
	}
}

//go:build fusion

package davsync

import (
	"errors"
	"os"
	"path/filepath"
	"testing"


	"github.com/yetone/magpie/internal/fusion/isolation"
)

func TestFusionCannotConfigureCloudSync(t *testing.T) {
	r := t.TempDir()
	t.Setenv("HOME", r)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(r, "config"))
	err := Configure(Config{URL: "https://fixture.invalid/fusion", User: "fixture", Password: "fixture-password", Passphrase: "separate-fixture-passphrase"})
	if !errors.Is(err, isolation.ErrDisabled) {
		t.Fatalf("configure = %v, want disabled", err)
	}
	if _, err := os.Stat(path("sync.json")); !os.IsNotExist(err) {
		t.Fatal("cloud settings written")
	}
}

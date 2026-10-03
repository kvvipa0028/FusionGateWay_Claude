//go:build fusion && darwin

package autostart

import (
	"errors"
	"os"
	"path/filepath"
	"testing"


	"github.com/yetone/magpie/internal/fusion/isolation"
)

func TestFusionCannotChangeMagpieLaunchAgent(t *testing.T) {
	r := t.TempDir()
	t.Setenv("HOME", r)
	path := filepath.Join(r, "Library", "LaunchAgents", "com.yetone.magpie.plist")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fixture-original-service"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, on := range []bool{false, true} {
		if err := Set(on); !errors.Is(err, isolation.ErrDisabled) {
			t.Errorf("autostart(%v) = %v, want disabled", on, err)
		}
		b, err := os.ReadFile(path)
		if err != nil || string(b) != "fixture-original-service" {
			t.Errorf("original launch agent changed: %v", err)
		}
	}
	if Enabled() {
		t.Error("Fusion inherited original autostart")
	}
}

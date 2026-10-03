//go:build fusion

package plugin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"


	"github.com/yetone/magpie/internal/fusion/isolation"
)

func TestFusionCannotAdoptUnapprovedPlugin(t *testing.T) {
	r := t.TempDir()
	t.Setenv("HOME", r)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(r, "config"))
	file := filepath.Join(r, "fixture-plugin.js")
	if err := os.WriteFile(file, []byte("export default async () => ({})"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(context.Background(), file); !errors.Is(err, isolation.ErrDisabled) {
		t.Errorf("plugin adoption = %v, want disabled", err)
	}
	if len(Load().Plugins) != 0 {
		t.Fatal("unapproved plugin persisted")
	}
}

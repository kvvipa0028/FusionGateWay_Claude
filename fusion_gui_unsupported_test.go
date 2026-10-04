//go:build fusion && (nogui || !darwin)

package main

import (
	"context"
	"io"
	"testing"

	"github.com/yetone/magpie/internal/fusion/bootstrap"
)

func TestFusionUIUnsupportedBuildFailsClosed(t *testing.T) {
	if err := runFusionGUI(context.Background(), []string{"--projects", "/unused/projects.json"}, io.Discard); err != bootstrap.ErrControlHost {
		t.Fatal("unsupported build accepted Native UI", err)
	}
	handled, err := fusionControlCommand([]string{"fusion-ui", "--projects", "/unused/projects.json"})
	if !handled || err != bootstrap.ErrControlHost {
		t.Fatal("Native UI command escaped dispatcher", handled, err)
	}
}

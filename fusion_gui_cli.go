//go:build fusion && !nogui && darwin

package main

import (
	"context"
	"flag"
	"io"
	"os"
	"path/filepath"

	"github.com/yetone/magpie/internal/fusion/bootstrap"
	"github.com/yetone/magpie/internal/gui"
)

func runFusionGUI(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("fusion-ui", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	source := flags.String("projects", "", "private projects.json source")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *source == "" || ctx == nil || out == nil {
		return bootstrap.ErrControlHost
	}
	root := os.Getenv("FUSION_STATE_ROOT")
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return bootstrap.ErrControlHost
	}
	return gui.RunFusion(ctx, *source, filepath.Join(root, "data", "fusion-gateway", "control"), out)
}

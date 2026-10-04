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
	quotaProject := flags.String("glm-quota-project", "", "explicit quota project")
	quotaRoute := flags.String("glm-quota-route", "", "explicit quota route")
	quotaKey := flags.String("glm-quota-key", "", "private quota key file")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *source == "" || ctx == nil || out == nil {
		return bootstrap.ErrControlHost
	}
	query := *quotaProject != "" || *quotaRoute != "" || *quotaKey != ""
	if query && (*quotaProject == "" || *quotaRoute == "" || *quotaKey == "") {
		return bootstrap.ErrControlHost
	}
	root := os.Getenv("FUSION_STATE_ROOT")
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return bootstrap.ErrControlHost
	}
	controlRoot := filepath.Join(root, "data", "fusion-gateway", "control")
	if query {
		return gui.RunFusionWithGLMQuota(ctx, *source, controlRoot, out, gui.FusionGLMQuota{Project: *quotaProject, Route: *quotaRoute, KeyFile: *quotaKey})
	}
	return gui.RunFusion(ctx, *source, controlRoot, out)
}

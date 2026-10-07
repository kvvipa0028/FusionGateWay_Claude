//go:build fusion

package main

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/yetone/magpie/internal/fusion/bootstrap"
)

func fusionControlCommand(args []string) (bool, error) {
	if len(args) == 0 || args[0] != "fusion-control" && args[0] != "fusion-ui" {
		return false, nil
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if args[0] == "fusion-ui" {
		return true, runFusionGUI(ctx, args[1:], os.Stdout)
	}
	return true, runFusionControl(ctx, args[1:], os.Stdout)
}
func runFusionControl(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("fusion-control", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	source := flags.String("projects", "", "private projects.json source")
	addr := flags.String("addr", "127.0.0.1:0", "numeric loopback address")
	quotaProject := flags.String("glm-quota-project", "", "registered project for explicit CN quota queries only")
	quotaRoute := flags.String("glm-quota-route", "", "exact declared GLM route ID for quota queries")
	quotaKey := flags.String("glm-quota-key", "", "private glm-coding-plan.key file; never a literal key")
	execProject := flags.String("glm-execution-project", "", "registered project for GLM stage execution")
	execRoute := flags.String("glm-execution-route", "", "exact declared GLM route ID for execution")
	execKey := flags.String("glm-execution-key", "", "private glm-coding-plan.key file; never a literal key")
	execEvidence := flags.String("glm-execution-evidence", "", "private generation evidence file recorded by record-glm-evidence.py")
	execExe := flags.String("glm-execution-claude", "", "pinned Claude executable path")
	execRoot := flags.String("glm-execution-root", "", "private execution root for managed runs")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *source == "" || ctx == nil || out == nil {
		return bootstrap.ErrControlHost
	}
	query := *quotaProject != "" || *quotaRoute != "" || *quotaKey != ""
	if query && (*quotaProject == "" || *quotaRoute == "" || *quotaKey == "") {
		return bootstrap.ErrControlHost
	}
	execution := *execProject != "" || *execRoute != "" || *execKey != "" || *execEvidence != "" || *execExe != "" || *execRoot != ""
	if execution && (*execProject == "" || *execRoute == "" || *execKey == "" || *execEvidence == "" || *execExe == "" || *execRoot == "") {
		return bootstrap.ErrControlHost
	}
	if execution && query {
		return bootstrap.ErrControlHost
	}
	root := os.Getenv("FUSION_STATE_ROOT")
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return bootstrap.ErrControlHost
	}
	var host *bootstrap.ControlHost
	var e error
	controlRoot := filepath.Join(root, "data", "fusion-gateway", "control")
	switch {
	case execution:
		host, e = bootstrap.OpenGLMExecutionControl(ctx, *source, controlRoot, *addr, *execProject, *execRoute, *execKey, *execEvidence, *execExe, *execRoot)
	case query:
		host, e = bootstrap.OpenGLMQuotaControl(ctx, *source, controlRoot, *addr, *quotaProject, *quotaRoute, *quotaKey)
	default:
		host, e = bootstrap.OpenControl(*source, controlRoot, *addr)
	}
	if e != nil {
		return bootstrap.ErrControlHost
	}
	defer host.Close()
	mode := "draft-control"
	enabled := false
	if execution {
		mode, enabled = "glm-execution", true
	}
	if json.NewEncoder(out).Encode(map[string]any{"product": "fusion-gateway", "control_address": "http://" + host.Addr(), "mode": mode, "execution_enabled": enabled, "quota_query_enabled": query, "jev": "off"}) != nil {
		return bootstrap.ErrControlHost
	}
	return host.Serve(ctx)
}

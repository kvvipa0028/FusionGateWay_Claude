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
	if len(args) == 0 || args[0] != "fusion-control" {
		return false, nil
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return true, runFusionControl(ctx, args[1:], os.Stdout)
}
func runFusionControl(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("fusion-control", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	source := flags.String("projects", "", "private projects.json source")
	addr := flags.String("addr", "127.0.0.1:0", "numeric loopback address")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *source == "" || ctx == nil || out == nil {
		return bootstrap.ErrControlHost
	}
	root := os.Getenv("FUSION_STATE_ROOT")
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return bootstrap.ErrControlHost
	}
	host, e := bootstrap.OpenControl(*source, filepath.Join(root, "data", "fusion-gateway", "control"), *addr)
	if e != nil {
		return bootstrap.ErrControlHost
	}
	defer host.Close()
	if json.NewEncoder(out).Encode(map[string]any{"product": "fusion-gateway", "control_address": "http://" + host.Addr(), "mode": "draft-control", "execution_enabled": false, "jev": "off"}) != nil {
		return bootstrap.ErrControlHost
	}
	return host.Serve(ctx)
}

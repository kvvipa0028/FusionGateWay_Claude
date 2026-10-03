//go:build fusion

package main

import (
	"context"
	"github.com/yetone/magpie/internal/fusion/bootstrap"
	"io"
	"testing"
)

func TestFusionControlCommandRejectsArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"--unknown"}, {"--projects"}, {"extra"}, {"--projects", "relative"}, {"--projects", "/missing/projects.json", "--addr", "0.0.0.0:0"}} {
		if e := runFusionControl(context.Background(), args, io.Discard); e != bootstrap.ErrControlHost {
			t.Fatal("bad arguments accepted", e)
		}
	}
}
func TestFusionControlCommandKeepsOtherCommands(t *testing.T) {
	for _, args := range [][]string{nil, {"fusion-status"}, {"serve"}, {"web"}} {
		handled, e := fusionControlCommand(args)
		if handled || e != nil {
			t.Fatal("intercepted unrelated command")
		}
	}
}

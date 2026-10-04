//go:build fusion && (nogui || !darwin)

package main

import (
	"context"
	"io"

	"github.com/yetone/magpie/internal/fusion/bootstrap"
)

func runFusionGUI(context.Context, []string, io.Writer) error { return bootstrap.ErrControlHost }

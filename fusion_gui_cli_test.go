//go:build fusion && !nogui && darwin

package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/yetone/magpie/internal/fusion/bootstrap"
)

func TestFusionGUIRejectsIncompleteQuotaPurposeBeforeWindow(t *testing.T) {
	t.Setenv("FUSION_STATE_ROOT", t.TempDir())
	for _, args := range [][]string{
		{"--projects", "/missing/projects.json", "--glm-quota-project", "fixture"},
		{"--projects", "/missing/projects.json", "--glm-quota-route", "fixture"},
		{"--projects", "/missing/projects.json", "--glm-quota-key", "fixture-key"},
		{"--projects", "/missing/projects.json", "--glm-quota-project", "fixture", "--glm-quota-route", "fixture", "--glm-quota-key", "relative"},
	} {
		var out bytes.Buffer
		if e := runFusionGUI(context.Background(), args, &out); e != bootstrap.ErrControlHost || out.Len() != 0 {
			t.Fatal("invalid quota purpose opened GUI or disclosed output")
		}
	}
}

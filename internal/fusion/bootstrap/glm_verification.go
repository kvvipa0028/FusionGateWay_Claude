package bootstrap

import (
	"context"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/fusion/evidence"
	"github.com/yetone/magpie/internal/fusion/store"
	"github.com/yetone/magpie/internal/fusion/workspace"
)

// StageVerificationConfig is an independently registered host command/standard,
// not model-generated instructions or a task HTTP configuration.
type StageVerificationConfig struct {
	Spec       evidence.Spec
	Acceptance []string
}

// GLMVerificationConfig keeps existing GLM wiring source-compatible.
type GLMVerificationConfig = StageVerificationConfig

func glmVerificationValid(v *GLMVerificationConfig) bool {
	if v == nil {
		return true
	}
	if evidence.ValidateSpec(v.Spec) != nil || len(v.Acceptance) == 0 || len(v.Acceptance) > 128 {
		return false
	}
	for _, s := range v.Acceptance {
		if !utf8.ValidString(s) || strings.TrimSpace(s) == "" || len(s) > 8192 {
			return false
		}
	}
	return true
}
func glmVerificationMatches(v *GLMVerificationConfig, input store.StageInput) bool {
	return v != nil && input.Workflow != nil && input.Workflow.Design != nil && input.Workflow.Approval != nil && reflect.DeepEqual(v.Acceptance, input.Workflow.Design.Snapshot.Document.Acceptance)
}
func glmRunVerification(ctx context.Context, a workspace.FrozenArtifact, root string, s evidence.Spec, current func() bool) (evidence.Result, error) {
	owned, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	defer close(done)
	go func() {
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-owned.Done():
				return
			case <-tick.C:
				if !current() {
					cancel()
					return
				}
			}
		}
	}()
	return evidence.Run(owned, a, root, s)
}

package runtime

import (
	"context"
	"io"
	"net/http"
	"regexp"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

const CodexStageProvider = "fusion_codex_stage"
const codexStageEnv = "FUSION_CODEX_STAGE_SECRET"

var codexModelID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// NewCodexHTTPChannel extends the private stdio channel with one exclusively
// owned controller endpoint and a prepared stage credential. No long-term
// ChatGPT credential is given to Native. The local provider is stage auth, not
// subscription/account proof; trusted registry/Forwarder admission is separate.
func NewCodexHTTPChannel(r store.StageRun, root, cwd, session string, driver func(context.Context, io.ReadWriteCloser) error, current func() bool, handler http.Handler, grant *policy.PendingModelGrant) (*CodexChannel, error) {
	t := r.Target
	if !codexModelID.MatchString(t.RequestedModel) || t.BillingPath != "subscription" || t.LockEnforcement != stageplan.ControlledCalls || t.Effort.Value == nil || !codexModelID.MatchString(*t.Effort.Value) || (t.Effort.RequestedMode != stageplan.EffortExplicit && t.Effort.RequestedMode != stageplan.EffortDefault) {
		return nil, ErrLaunch
	}
	c, e := NewCodexChannel(r, root, cwd, session, driver, current)
	if e != nil {
		return nil, e
	}
	c.models, e = newModelChannel(handler, grant, t, "")
	if e != nil {
		return nil, e
	}
	return c, nil
}

// Close retains the endpoint and scope until the managed Native is reaped.
func (c *CodexChannel) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active {
		return ErrLaunch
	}
	if c.models != nil {
		if e := c.models.Close(); e != nil {
			return e
		}
	}
	c.closed = true
	return nil
}

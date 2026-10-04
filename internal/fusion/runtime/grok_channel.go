package runtime

import (
	"net/http"
	"regexp"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/stageplan"
)

const GrokCLIVersion = "1.0.48"
const GrokExecutableSHA256 = "1ed292eb62206b1a2ec3d17dc69c9c8406a07f5ff414305f953baee5b72a4a05"

var grokModel = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

// GrokChannel holds only a scoped stage grant, never the X subscription secret.
// Handler and frozen target come from trusted controller wiring. Construction
// does not itself verify upstream identity, quota, billing or route admission.
type GrokChannel struct {
	*modelChannel
	seed *grokSessionSeed
}

func (*GrokChannel) String() string   { return "scoped Grok channel (redacted)" }
func (*GrokChannel) GoString() string { return "GrokChannel(<redacted>)" }

func NewGrokChannel(handler http.Handler, grant *policy.PendingModelGrant, target stageplan.ExecutionTarget) (*GrokChannel, error) {
	if !grokModel.MatchString(target.RequestedModel) || target.RequestedModel != target.ResolvedModel || target.RuntimeVersion != GrokCLIVersion || target.BillingPath != "subscription" {
		return nil, ErrLaunch
	}
	channel, e := newModelChannel(handler, grant, target, "/v1")
	if e != nil {
		return nil, e
	}
	return &GrokChannel{modelChannel: channel}, nil
}

// A launch has exactly one typed channel, or none for an offline worker.
func (s Spec) modelChannel() (*modelChannel, error) {
	if s.CodexChannel != nil && (s.ClaudeChannel != nil || s.GrokChannel != nil || !s.CodexChannel.launchValid(s)) {
		return nil, ErrLaunch
	}
	if s.CodexChannel != nil && s.CodexChannel.models != nil {
		return s.CodexChannel.models, nil
	}
	if s.ClaudeChannel != nil && s.GrokChannel != nil {
		return nil, ErrLaunch
	}
	if s.ClaudeChannel != nil {
		if s.ClaudeChannel.modelChannel == nil {
			return nil, ErrLaunch
		}
		return s.ClaudeChannel.modelChannel, nil
	}
	if s.GrokChannel != nil {
		if s.GrokChannel.modelChannel == nil {
			return nil, ErrLaunch
		}
		return s.GrokChannel.modelChannel, nil
	}
	return nil, nil
}

func (c *GrokChannel) Close() error {
	if c == nil {
		return nil
	}
	return c.modelChannel.Close()
}

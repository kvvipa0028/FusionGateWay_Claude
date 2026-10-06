package codex

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

const StageProvider = "fusion_codex_stage"

// NewGateway selects the controller-owned stage transport, not a Native
// ChatGPT login. Scope/Identity/admitted must come from trusted frozen registry
// and managed execution wiring. A local null account never admits an upstream.
// Gateway threads persist their rollout under the private CODEX_HOME so a
// trusted checkpoint consumer can later resume them; adopting a prior thread
// still requires the adapter's verified checkpoint binding plus ResumeThread.
func NewGateway(p Peer, b Binding, scope func(Scope) bool, identity func() Identity, admitted func() bool) (*Client, error) {
	if e := ValidateGatewayTarget(b.Target); e != nil {
		return nil, e
	}
	c, e := New(p, b, scope, identity, admitted)
	if e != nil {
		return nil, e
	}
	c.gateway = true
	c.persist = true
	return c, nil
}

// ValidateGatewayTarget is a static compatibility check, never admission. It
// lets a trusted adapter reject incompatible routes before durable launch.
func ValidateGatewayTarget(t stageplan.ExecutionTarget) error {
	if t.RuntimeVersion != CLIVersion || t.Account == "" || t.Workspace == "" || t.CredentialIdentity == "" || t.ResolvedModel != t.RequestedModel || t.BillingPath != "subscription" || t.Route.ID == "" || t.Route.Revision < 1 || t.PluginVersion != nil || t.LockEnforcement != stageplan.ControlledCalls || !gateID.MatchString(t.RequestedModel) || t.Effort.Value == nil || !gateID.MatchString(*t.Effort.Value) || (t.Effort.RequestedMode != stageplan.EffortExplicit && t.Effort.RequestedMode != stageplan.EffortDefault) {
		return ErrUnverified
	}
	return nil
}
func (c *Client) readGatewayAccount(ctx context.Context) error {
	var out map[string]json.RawMessage
	c.accountVerified = false
	if e := c.call(ctx, "account/read", map[string]bool{"refreshToken": false}, &out); e != nil {
		return e
	}
	for k := range out {
		if k != "account" && k != "requiresOpenaiAuth" && k != "workspaceRouting" {
			return ErrUnverified
		}
	}
	if routing, exists := out["workspaceRouting"]; exists && !isNull(routing) {
		return ErrUnverified
	}
	var required bool
	if !bytes.Equal(bytes.TrimSpace(out["account"]), []byte("null")) || len(out["requiresOpenaiAuth"]) == 0 || bytes.Equal(bytes.TrimSpace(out["requiresOpenaiAuth"]), []byte("null")) || json.Unmarshal(out["requiresOpenaiAuth"], &required) != nil || required {
		return ErrUnverified
	}
	// This flag records only the expected local transport authentication shape.
	// generation() still requires independent current upstream admission.
	c.accountVerified = true
	return nil
}
func (*Client) String() string               { return "Codex client (redacted)" }
func (*Client) GoString() string             { return "codex.Client(<redacted>)" }
func (*Client) MarshalJSON() ([]byte, error) { return nil, ErrProtocol }

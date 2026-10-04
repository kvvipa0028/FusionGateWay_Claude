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
// Only readonly, tool-free generation is currently characterized; restoring a
// thread needs a separate checkpoint/identity proof and is refused here.
func NewGateway(p Peer, b Binding, scope func(Scope) bool, identity func() Identity, admitted func() bool) (*Client, error) {
	t := b.Target
	if t.BillingPath != "subscription" || t.Route.ID == "" || t.Route.Revision < 1 || t.PluginVersion != nil || t.LockEnforcement != stageplan.ControlledCalls || !gateID.MatchString(t.RequestedModel) || t.Effort.Value == nil || !gateID.MatchString(*t.Effort.Value) || (t.Effort.RequestedMode != stageplan.EffortExplicit && t.Effort.RequestedMode != stageplan.EffortDefault) {
		return nil, ErrUnverified
	}
	c, e := New(p, b, scope, identity, admitted)
	if e != nil {
		return nil, e
	}
	c.gateway = true
	return c, nil
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

// Package codex implements the pinned App Server protocol, without borrowing
// ambient accounts or declaring native generation/hidden calls admitted.
package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

const CLIVersion = "0.160.0"

var (
	ErrProtocol   = errors.New("pinned codex protocol mismatch")
	ErrIdentity   = errors.New("codex execution identity changed")
	ErrUnverified = errors.New("codex generation/all-calls capability unverified")
	ErrNative     = errors.New("native codex request failed")
)

type Scope struct {
	RunID      string
	Generation int64
	Role       stageplan.Role
}
type Identity struct {
	Account, Workspace, CredentialIdentity string
	Generation                             int64
}
type Binding struct {
	Scope     Scope
	Identity  Identity
	Target    stageplan.ExecutionTarget
	Cwd       string
	CodexHome string
}

// Peer is a trusted private, bounded transport for one managed native process.
// It must correlate response IDs and handle server requests separately; no raw
// method/params or returned auth payload is exposed through product APIs.
type Peer interface {
	Call(context.Context, uint64, string, json.RawMessage) (json.RawMessage, error)
	Notify(context.Context, string, json.RawMessage) error
}
type Client struct {
	mu                           sync.Mutex
	peer                         Peer
	binding                      Binding
	validScope                   func(Scope) bool
	currentIdentity              func() Identity
	generationAdmitted           func() bool
	initialized, accountVerified bool
	nextID                       uint64
	threadID, turnID, state      string
}

func New(p Peer, b Binding, scope func(Scope) bool, identity func() Identity, admitted func() bool) (*Client, error) {
	if p == nil || scope == nil || identity == nil || b.Scope.RunID == "" || b.Scope.Generation < 1 || b.Identity.Generation < 1 || b.Identity.Account == "" || b.Identity.Workspace == "" || b.Identity.CredentialIdentity == "" || b.Target.Account != b.Identity.Account || b.Target.Workspace != b.Identity.Workspace || b.Target.CredentialIdentity != b.Identity.CredentialIdentity || b.Target.RequestedModel == "" || b.Target.RequestedModel != b.Target.ResolvedModel || b.Target.RuntimeVersion != CLIVersion || b.Target.Effort.Value == nil || *b.Target.Effort.Value == "" || !filepath.IsAbs(b.Cwd) || filepath.Clean(b.Cwd) != b.Cwd {
		return nil, ErrProtocol
	}
	knownRole := false
	for _, role := range stageplan.AllRoles() {
		if role == b.Scope.Role {
			knownRole = true
		}
	}
	if !knownRole || !filepath.IsAbs(b.CodexHome) || filepath.Clean(b.CodexHome) != b.CodexHome {
		return nil, ErrProtocol
	}
	target := b.Target
	effort := *target.Effort.Value
	target.Effort.Value = &effort
	target.Capabilities = append([]string(nil), target.Capabilities...)
	b.Target = target
	return &Client{peer: p, binding: b, validScope: scope, currentIdentity: identity, generationAdmitted: admitted, state: "new"}, nil
}
func (c *Client) check() error {
	if !c.validScope(c.binding.Scope) || c.currentIdentity() != c.binding.Identity {
		return ErrIdentity
	}
	return nil
}
func (c *Client) generation() error {
	if e := c.check(); e != nil {
		return e
	}
	if !c.accountVerified || c.generationAdmitted == nil || !c.generationAdmitted() || c.binding.Target.LockEnforcement != stageplan.ControlledCalls {
		return ErrUnverified
	}
	return nil
}
func (c *Client) call(ctx context.Context, method string, params any, out any) error {
	if e := c.check(); e != nil {
		return e
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	raw, e := json.Marshal(params)
	if e != nil {
		return ErrProtocol
	}
	c.nextID++
	response, e := c.peer.Call(ctx, c.nextID, method, raw)
	if e != nil {
		return ErrNative
	}
	if e = c.check(); e != nil {
		return e
	}
	return decode(response, out)
}
func (c *Client) Initialize(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.initialized {
		return ErrProtocol
	}
	var out struct {
		UserAgent      string `json:"userAgent"`
		PlatformFamily string `json:"platformFamily"`
		PlatformOs     string `json:"platformOs"`
		CodexHome      string `json:"codexHome"`
	}
	if e := c.call(ctx, "initialize", map[string]any{"clientInfo": map[string]string{"name": "fusion_gateway", "title": "Fusion Gateway", "version": "0.1.0"}, "capabilities": map[string]bool{"explicitGatewayOauth": true, "experimentalApi": false}}, &out); e != nil {
		return e
	}
	if !advertisesVersion(out.UserAgent) || out.PlatformFamily == "" || out.PlatformOs == "" || out.CodexHome != c.binding.CodexHome {
		return ErrProtocol
	}
	if e := c.peer.Notify(ctx, "initialized", json.RawMessage(`{}`)); e != nil {
		return ErrNative
	}
	c.initialized = true
	c.state = "initialized"
	return nil
}
func (c *Client) ReadAccount(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.initialized {
		return ErrProtocol
	}
	var out struct {
		Account *struct {
			Type     string  `json:"type"`
			Email    *string `json:"email"`
			PlanType string  `json:"planType"`
		} `json:"account"`
		RequiresOpenaiAuth *bool `json:"requiresOpenaiAuth"`
	}
	if e := c.call(ctx, "account/read", map[string]bool{"refreshToken": false}, &out); e != nil {
		c.accountVerified = false
		return e
	}
	if out.Account == nil || out.Account.Type != "chatgpt" || out.Account.PlanType == "" || out.RequiresOpenaiAuth == nil || !*out.RequiresOpenaiAuth {
		c.accountVerified = false
		return ErrUnverified
	}
	// Stable account/workspace/credential identity is verified by the private
	// registry callback, not guessed from the possibly null email/plan label.
	c.accountVerified = true
	return nil
}
func (c *Client) threadParams(id string) map[string]any {
	p := map[string]any{"model": c.binding.Target.ResolvedModel, "modelProvider": "openai", "cwd": c.binding.Cwd, "approvalPolicy": "never", "sandbox": "read-only", "config": map[string]any{"model_reasoning_effort": *c.binding.Target.Effort.Value, "model_reasoning_summary": "none"}}
	if c.writing() {
		p["sandbox"] = "workspace-write"
		p["config"].(map[string]any)["sandbox_workspace_write"] = map[string]any{"network_access": false, "exclude_slash_tmp": true, "exclude_tmpdir_env_var": true}
	}
	if id != "" {
		p["threadId"] = id
	} else {
		p["ephemeral"] = true
	}
	return p
}
func (c *Client) thread(ctx context.Context, id string) (string, error) {
	if !c.initialized || !c.accountVerified {
		return "", ErrUnverified
	}
	if e := c.generation(); e != nil {
		return "", e
	}
	method := "thread/start"
	if id != "" {
		method = "thread/resume"
	}
	var out struct {
		Thread struct {
			ID         string `json:"id"`
			CLIVersion string `json:"cliVersion"`
		} `json:"thread"`
		Model           string  `json:"model"`
		ModelProvider   string  `json:"modelProvider"`
		ReasoningEffort *string `json:"reasoningEffort"`
		Cwd             string  `json:"cwd"`
		ApprovalPolicy  string  `json:"approvalPolicy"`
		Sandbox         struct {
			Type                string   `json:"type"`
			NetworkAccess       *bool    `json:"networkAccess"`
			WritableRoots       []string `json:"writableRoots"`
			ExcludeSlashTmp     bool     `json:"excludeSlashTmp"`
			ExcludeTmpdirEnvVar bool     `json:"excludeTmpdirEnvVar"`
		} `json:"sandbox"`
	}
	if e := c.call(ctx, method, c.threadParams(id), &out); e != nil {
		return "", e
	}
	expectedSandbox := "readOnly"
	if c.writing() {
		expectedSandbox = "workspaceWrite"
	}
	if out.Thread.ID == "" || out.Thread.CLIVersion != CLIVersion || id != "" && out.Thread.ID != id || out.Model != c.binding.Target.ResolvedModel || out.ModelProvider != "openai" || out.ReasoningEffort == nil || *out.ReasoningEffort != *c.binding.Target.Effort.Value || out.Cwd != c.binding.Cwd || out.ApprovalPolicy != "never" || out.Sandbox.Type != expectedSandbox || out.Sandbox.NetworkAccess == nil || *out.Sandbox.NetworkAccess {
		return "", ErrProtocol
	}
	if c.writing() && (len(out.Sandbox.WritableRoots) != 1 || out.Sandbox.WritableRoots[0] != c.binding.Cwd || !out.Sandbox.ExcludeSlashTmp || !out.Sandbox.ExcludeTmpdirEnvVar) {
		return "", ErrProtocol
	}
	c.threadID = out.Thread.ID
	c.turnID = ""
	c.state = "ready"
	return out.Thread.ID, nil
}
func (c *Client) StartThread(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.threadID != "" {
		return "", ErrProtocol
	}
	return c.thread(ctx, "")
}
func (c *Client) Resume(ctx context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if id == "" || id != c.threadID || c.state == "running" || c.state == "cancelling" || c.state == "execution_uncertain" {
		return ErrProtocol
	}
	if e := c.generation(); e != nil {
		return e
	}
	_, e := c.thread(ctx, id)
	return e
}
func (c *Client) StartTurn(ctx context.Context, prompt string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state != "ready" || prompt == "" || len(prompt) > 64<<10 {
		return "", ErrProtocol
	}
	if e := c.generation(); e != nil {
		return "", e
	}
	params := map[string]any{"threadId": c.threadID, "model": c.binding.Target.ResolvedModel, "effort": *c.binding.Target.Effort.Value, "cwd": c.binding.Cwd, "approvalPolicy": "never", "sandboxPolicy": c.sandboxPolicy(), "summary": "none", "input": []any{map[string]any{"type": "text", "text": prompt}}}
	var out struct {
		Turn struct{ ID, Status string } `json:"turn"`
	}
	c.state = "execution_uncertain"
	if e := c.call(ctx, "turn/start", params, &out); e != nil {
		return "", e
	}
	if out.Turn.ID == "" || out.Turn.Status != "inProgress" {
		return "", ErrProtocol
	}
	c.turnID = out.Turn.ID
	c.state = "running"
	return c.turnID, nil
}

func (c *Client) writing() bool {
	return c.binding.Scope.Role == stageplan.Implementation || c.binding.Scope.Role == stageplan.Testing
}
func (c *Client) sandboxPolicy() map[string]any {
	if c.writing() {
		return map[string]any{"type": "workspaceWrite", "networkAccess": false, "writableRoots": []string{c.binding.Cwd}, "excludeSlashTmp": true, "excludeTmpdirEnvVar": true}
	}
	return map[string]any{"type": "readOnly", "networkAccess": false}
}
func (c *Client) Interrupt(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == "cancelling" {
		return nil
	}
	if c.state != "running" {
		return ErrProtocol
	}
	var out map[string]any
	if e := c.call(ctx, "turn/interrupt", map[string]string{"threadId": c.threadID, "turnId": c.turnID}, &out); e != nil {
		return e
	}
	c.state = "cancelling"
	return nil
}
func (c *Client) State() string { c.mu.Lock(); defer c.mu.Unlock(); return c.state }
func (c *Client) Event(scope Scope, raw []byte) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.check(); e != nil {
		return c.state, e
	}
	if scope != c.binding.Scope {
		return c.state, ErrIdentity
	}
	if c.state != "running" && c.state != "cancelling" {
		return c.state, ErrProtocol
	}
	var event struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if e := decode(raw, &event); e != nil {
		return c.state, e
	}
	var params struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		Turn     *struct {
			ID, Status string
			Error      json.RawMessage
			Items      *[]json.RawMessage `json:"items"`
		} `json:"turn"`
		Item *struct {
			Type string `json:"type"`
		} `json:"item"`
	}
	if e := decode(event.Params, &params); e != nil {
		return c.state, e
	}
	if params.ThreadID != c.threadID || params.TurnID != "" && params.TurnID != c.turnID {
		return c.state, ErrProtocol
	}
	if event.Method == "model/rerouted" || event.Method == "thread/compacted" || params.Item != nil && !c.allowedItem(params.Item.Type) {
		c.state = "execution_uncertain"
		return c.state, ErrUnverified
	}
	switch event.Method {
	case "turn/completed":
		if params.Turn == nil || params.Turn.ID != c.turnID || params.Turn.Items == nil {
			return c.state, ErrProtocol
		}
		if params.Turn.Status == "completed" && len(params.Turn.Error) > 0 && !bytes.Equal(bytes.TrimSpace(params.Turn.Error), []byte("null")) {
			return c.state, ErrProtocol
		}
		for _, rawItem := range *params.Turn.Items {
			var item struct {
				Type string `json:"type"`
			}
			if decode(rawItem, &item) != nil || item.Type == "" {
				return c.state, ErrProtocol
			}
			if !c.allowedItem(item.Type) {
				c.state = "execution_uncertain"
				return c.state, ErrUnverified
			}
		}
		switch params.Turn.Status {
		case "completed":
			if c.state == "cancelling" {
				c.state = "interrupted"
			} else {
				c.state = "succeeded"
			}
		case "failed", "interrupted":
			c.state = params.Turn.Status
		default:
			return c.state, ErrProtocol
		}
	case "item/agentMessage/delta", "item/started", "item/completed", "turn/started", "thread/tokenUsage/updated", "turn/diff/updated":
	default:
		c.state = "execution_uncertain"
		return c.state, ErrProtocol
	}
	return c.state, nil
}

func advertisesVersion(agent string) bool {
	for _, token := range strings.Fields(agent) {
		if strings.HasSuffix(token, "/"+CLIVersion) {
			return true
		}
	}
	return false
}

// Accept only the pinned ordinary item types. Compaction, sub-agents, review
// helpers and external integrations require separate all-calls proof. This
// observation filter does not replace OS tool confinement or route admission.
func (c *Client) allowedItem(kind string) bool {
	switch kind {
	case "userMessage", "agentMessage", "plan", "reasoning", "commandExecution", "imageView", "sleep":
		return true
	case "fileChange":
		return c.writing()
	}
	return false
}

// Native payloads may add documented fields, but duplicate/case-colliding
// keys, null envelopes, trailing values and oversized frames are rejected.
func decode(raw []byte, out any) error {
	if len(raw) == 0 || len(raw) > 1<<20 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ErrProtocol
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if e := unique(d, 0); e != nil {
		return ErrProtocol
	}
	if _, e := d.Token(); e != io.EOF {
		return ErrProtocol
	}
	if json.Unmarshal(raw, out) != nil {
		return ErrProtocol
	}
	return nil
}
func unique(d *json.Decoder, depth int) error {
	if depth > 32 {
		return ErrProtocol
	}
	t, e := d.Token()
	if e != nil {
		return e
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	if delim == '{' {
		seen := map[string]bool{}
		for d.More() {
			key, e := d.Token()
			if e != nil {
				return e
			}
			s, ok := key.(string)
			if !ok {
				return ErrProtocol
			}
			s = strings.ToLower(s)
			if seen[s] {
				return ErrProtocol
			}
			seen[s] = true
			if e = unique(d, depth+1); e != nil {
				return e
			}
		}
	} else if delim == '[' {
		for d.More() {
			if e = unique(d, depth+1); e != nil {
				return e
			}
		}
	} else {
		return ErrProtocol
	}
	_, e = d.Token()
	return e
}

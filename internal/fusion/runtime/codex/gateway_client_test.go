package codex

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"testing"
)

func gatewayFixture(t *testing.T) (*Client, *fixture) {
	t.Helper()
	base, f := newFixture(t)
	b := base.binding
	b.Target.BillingPath = "subscription"
	b.Target.Route = stageplan.RouteRef{ID: "fixture-route", Revision: 1}
	b.Target.Effort.RequestedMode = stageplan.EffortExplicit
	f.response["account/read"] = json.RawMessage(`{"account":null,"requiresOpenaiAuth":false}`)
	var out map[string]any
	json.Unmarshal(f.response["thread/start"], &out)
	out["modelProvider"] = StageProvider
	f.response["thread/start"], _ = json.Marshal(out)
	c, e := NewGateway(f, b, func(s Scope) bool { return f.current && s == f.scope }, func() Identity { return f.identity }, func() bool { return f.admitted })
	if e != nil {
		t.Fatal(e)
	}
	return c, f
}
func TestGatewayClientPinsLocalProviderWithoutClaimingNativeLogin(t *testing.T) {
	c, f := gatewayFixture(t)
	initThread(t, c)
	if _, e := c.StartTurn(context.Background(), "fixture"); e != nil {
		t.Fatal(e)
	}
	var thread map[string]any
	json.Unmarshal(f.params[3], &thread)
	if thread["modelProvider"] != StageProvider {
		t.Fatal("provider changed")
	}
	before := len(f.methods)
	if e := c.Resume(context.Background(), c.threadID); !errors.Is(e, ErrUnverified) || len(f.methods) != before {
		t.Fatal("unverified restore escaped")
	}
	// Native transport account=null must not open the default built-in client.
	base, _ := newFixture(t)
	base.peer = f
	if e := base.Initialize(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e := base.ReadAccount(context.Background()); !errors.Is(e, ErrUnverified) {
		t.Fatal("default auth guard changed", e)
	}
}
func TestGatewayClientRejectsNativeAccountSubstitution(t *testing.T) {
	for _, raw := range []string{`{"requiresOpenaiAuth":false}`, `{"account":null}`, `{"account":null,"requiresOpenaiAuth":true}`, `{"account":{},"requiresOpenaiAuth":false}`, `{"account":{"type":"chatgpt","planType":"plus"},"requiresOpenaiAuth":false}`, `{"account":null,"requiresOpenaiAuth":null}`} {
		t.Run(raw, func(t *testing.T) {
			c, f := gatewayFixture(t)
			c.Initialize(context.Background())
			f.response["account/read"] = json.RawMessage(raw)
			if c.ReadAccount(context.Background()) == nil {
				t.Fatal("local native auth substituted")
			}
			before := len(f.methods)
			if _, e := c.StartThread(context.Background()); e == nil || len(f.methods) != before {
				t.Fatal("unverified thread escaped")
			}
		})
	}
}
func TestGatewayClientAdmissionIsRecheckedAfterCallback(t *testing.T) {
	c, f := gatewayFixture(t)
	c.Initialize(context.Background())
	c.ReadAccount(context.Background())
	c.generationAdmitted = func() bool { f.current = false; return true }
	before := len(f.methods)
	if _, e := c.StartThread(context.Background()); !errors.Is(e, ErrIdentity) || len(f.methods) != before {
		t.Fatal("revoked inside callback escaped", e)
	}
}
func TestGatewayClientGrantsWritesOnlyToWritingRoles(t *testing.T) {
	for _, role := range stageplan.AllRoles() {
		t.Run(string(role), func(t *testing.T) {
			c, f := gatewayFixture(t)
			c.binding.Scope.Role = role
			f.scope.Role = role
			writing := role == stageplan.Implementation || role == stageplan.Testing
			if writing {
				var d map[string]any
				json.Unmarshal(f.response["thread/start"], &d)
				d["sandbox"] = map[string]any{"type": "workspaceWrite", "networkAccess": false, "writableRoots": []string{c.binding.Cwd}, "excludeSlashTmp": true, "excludeTmpdirEnvVar": true}
				f.response["thread/start"], _ = json.Marshal(d)
			}
			initThread(t, c)
			c.StartTurn(context.Background(), "fixture")
			if c.writing() != writing {
				t.Fatal("gateway write roles drifted")
			}
			for _, kind := range []string{"commandExecution", "imageView", "sleep", "plan", "collabAgentToolCall"} {
				if c.allowedItem(kind) {
					t.Fatal("gateway allowed unverified tool", kind)
				}
			}
			if c.allowedItem("fileChange") != writing {
				t.Fatal("gateway file-change items must track the writer roles")
			}
		})
	}
}
func TestGatewayClientUnverifiedAdmissionSendsNoThread(t *testing.T) {
	c, f := gatewayFixture(t)
	c.Initialize(context.Background())
	c.ReadAccount(context.Background())
	f.admitted = false
	before := len(f.methods)
	if _, e := c.StartThread(context.Background()); !errors.Is(e, ErrUnverified) || len(f.methods) != before {
		t.Fatal("admission bypass")
	}
}

func gatewayRunning(t *testing.T) (*Client, *fixture) {
	t.Helper()
	c, f := gatewayFixture(t)
	initThread(t, c)
	if _, e := c.StartTurn(context.Background(), "fixture"); e != nil {
		t.Fatal(e)
	}
	return c, f
}
func gatewayNotification(method, params string) []byte {
	return []byte(`{"method":"` + method + `","params":` + params + `}`)
}
func gatewayStarted(t *testing.T) (*Client, *fixture) {
	t.Helper()
	c, f := gatewayRunning(t)
	if _, e := c.Event(f.scope, gatewayNotification("turn/started", `{"threadId":"fixture-thread","turn":{"id":"fixture-turn","items":[],"status":"inProgress"}}`)); e != nil {
		t.Fatal(e)
	}
	return c, f
}
func TestGatewayEventsFollowPinnedTextLifecycle(t *testing.T) {
	c, f := gatewayRunning(t)
	events := [][]byte{
		gatewayNotification("remoteControl/status/changed", `{"installationId":"fixture","serverName":"","status":"disabled","environmentId":null}`),
		gatewayNotification("thread/started", `{"thread":{"id":"fixture-thread","cliVersion":"0.160.0","cwd":"/fixture/workspace","modelProvider":"fusion_codex_stage","model":"fixture-model","reasoningEffort":"medium"}}`),
		gatewayNotification("warning", `{"message":"fixture warning","threadId":null}`),
		gatewayNotification("thread/status/changed", `{"threadId":"fixture-thread","status":{"type":"active","activeFlags":[]}}`),
		gatewayNotification("turn/started", `{"threadId":"fixture-thread","turn":{"id":"fixture-turn","items":[],"status":"inProgress"}}`),
		gatewayNotification("item/started", `{"threadId":"fixture-thread","turnId":"fixture-turn","startedAtMs":null,"item":{"id":"fixture-user","type":"userMessage","content":[{"type":"text","text":"fixture","text_elements":[]}]}}`),
		gatewayNotification("item/completed", `{"threadId":"fixture-thread","turnId":"fixture-turn","completedAtMs":1,"item":{"id":"fixture-user","type":"userMessage","content":[{"type":"text","text":"fixture","text_elements":[]}]}}`),
		gatewayNotification("item/started", `{"threadId":"fixture-thread","turnId":"fixture-turn","startedAtMs":1,"item":{"id":"fixture-answer","type":"agentMessage","text":""}}`),
		gatewayNotification("item/agentMessage/delta", `{"threadId":"fixture-thread","turnId":"fixture-turn","itemId":"fixture-answer","delta":"fixture"}`),
		gatewayNotification("item/completed", `{"threadId":"fixture-thread","turnId":"fixture-turn","completedAtMs":2,"item":{"id":"fixture-answer","type":"agentMessage","text":"fixture","phase":"final_answer"}}`),
		gatewayNotification("thread/tokenUsage/updated", `{"threadId":"fixture-thread","turnId":"fixture-turn","tokenUsage":{"total":{"inputTokens":3,"cachedInputTokens":0,"outputTokens":2,"reasoningOutputTokens":0,"totalTokens":5},"last":{"inputTokens":3,"cachedInputTokens":0,"outputTokens":2,"reasoningOutputTokens":0,"totalTokens":5},"modelContextWindow":null}}`),
		gatewayNotification("account/rateLimits/updated", `{"rateLimits":{}}`),
		gatewayNotification("thread/status/changed", `{"threadId":"fixture-thread","status":{"type":"idle"}}`),
	}
	for _, raw := range events {
		state, e := c.Event(f.scope, raw)
		if e != nil || state != "running" {
			t.Fatal("observation changed terminal state", state, e, string(raw))
		}
	}
	raw := gatewayNotification("turn/completed", `{"threadId":"fixture-thread","turn":{"id":"fixture-turn","items":[],"status":"completed"}}`)
	if state, e := c.Event(f.scope, raw); e != nil || state != "succeeded" {
		t.Fatal(state, e)
	}
	if state, e := c.Event(f.scope, raw); e == nil || state != "succeeded" {
		t.Fatal("terminal overwritten")
	}
}
func TestGatewayEventsRejectUnknownToolsStaleAndContradictoryFrames(t *testing.T) {
	cases := map[string][]byte{
		"remote_connected":      gatewayNotification("remoteControl/status/changed", `{"installationId":"fixture","serverName":"","status":"connected"}`),
		"unknown":               gatewayNotification("model/rerouted", `{"threadId":"fixture-thread"}`),
		"compact":               gatewayNotification("thread/compacted", `{"threadId":"fixture-thread"}`),
		"stale_thread":          gatewayNotification("thread/status/changed", `{"threadId":"other","status":{"type":"idle"}}`),
		"stale_turn":            gatewayNotification("item/started", `{"threadId":"fixture-thread","turnId":"other","startedAtMs":1,"item":{"id":"fixture","type":"agentMessage","text":""}}`),
		"missing_turn":          gatewayNotification("item/started", `{"threadId":"fixture-thread","startedAtMs":1,"item":{"id":"fixture","type":"agentMessage","text":""}}`),
		"tool":                  gatewayNotification("item/started", `{"threadId":"fixture-thread","turnId":"fixture-turn","startedAtMs":1,"item":{"id":"fixture","type":"commandExecution"}}`),
		"summary":               gatewayNotification("item/started", `{"threadId":"fixture-thread","turnId":"fixture-turn","startedAtMs":1,"item":{"id":"fixture","type":"reasoning","summary":["fixture"]}}`),
		"awaiting_approval":     gatewayNotification("thread/status/changed", `{"threadId":"fixture-thread","status":{"type":"active","activeFlags":["waitingOnApproval"]}}`),
		"missing_items":         gatewayNotification("turn/completed", `{"threadId":"fixture-thread","turn":{"id":"fixture-turn","status":"completed"}}`),
		"bad_completed_error":   gatewayNotification("turn/completed", `{"threadId":"fixture-thread","turn":{"id":"fixture-turn","items":[],"status":"completed","error":{"message":"fixture secret"}}}`),
		"unknown_terminal_item": gatewayNotification("turn/completed", `{"threadId":"fixture-thread","turn":{"id":"fixture-turn","items":[{"id":"unseen","type":"agentMessage","text":"fixture"}],"status":"completed"}}`),
		"timestamp_null":        []byte(`{"method":"warning","params":{"message":"fixture"},"emittedAtMs":null}`),
		"alias":                 []byte(`{"method":"warning","method":"turn/completed","params":{"message":"fixture"}}`),
		"extra_field":           gatewayNotification("warning", `{"message":"fixture","account":"other"}`),
		"unseen_delta":          gatewayNotification("item/agentMessage/delta", `{"threadId":"fixture-thread","turnId":"fixture-turn","itemId":"unseen","delta":"fixture"}`),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			c, f := gatewayStarted(t)
			state, e := c.Event(f.scope, raw)
			if e == nil || state != "execution_uncertain" {
				t.Fatal("unsafe event accepted", state, e)
			}
			if _, e = c.StartTurn(context.Background(), "replay"); e == nil {
				t.Fatal("uncertain turn replayed")
			}
		})
	}
}
func TestGatewayEventsNeverCompleteUnfinishedItem(t *testing.T) {
	c, f := gatewayStarted(t)
	start := gatewayNotification("item/started", `{"threadId":"fixture-thread","turnId":"fixture-turn","startedAtMs":1,"item":{"id":"fixture","type":"agentMessage","text":""}}`)
	if _, e := c.Event(f.scope, start); e != nil {
		t.Fatal(e)
	}
	end := gatewayNotification("turn/completed", `{"threadId":"fixture-thread","turn":{"id":"fixture-turn","items":[],"status":"completed"}}`)
	if state, e := c.Event(f.scope, end); e == nil || state != "execution_uncertain" {
		t.Fatal("unfinished item accepted")
	}
}
func TestGatewayEventsFatalErrorCannotBecomeSuccess(t *testing.T) {
	c, f := gatewayStarted(t)
	raw := gatewayNotification("error", `{"threadId":"fixture-thread","turnId":"fixture-turn","error":{"message":"fixture secret"},"willRetry":false}`)
	if state, e := c.Event(f.scope, raw); e != nil || state != "running" {
		t.Fatal(state, e)
	}
	raw = gatewayNotification("turn/completed", `{"threadId":"fixture-thread","turn":{"id":"fixture-turn","items":[],"status":"completed"}}`)
	if state, e := c.Event(f.scope, raw); e == nil || state != "execution_uncertain" {
		t.Fatal("fatal error erased")
	}
}

func TestGatewayLostThreadAckCannotBeReplayed(t *testing.T) {
	c, f := gatewayFixture(t)
	c.Initialize(context.Background())
	c.ReadAccount(context.Background())
	delete(f.response, "thread/start")
	if _, e := c.StartThread(context.Background()); e == nil || c.State() != "execution_uncertain" {
		t.Fatal("missing ack was trusted")
	}
	n := len(f.methods)
	if _, e := c.StartThread(context.Background()); e == nil || len(f.methods) != n {
		t.Fatal("unacknowledged thread start replayed")
	}
}

func TestGatewayConstructorRejectsUncontrolledTargets(t *testing.T) {
	mutations := map[string]func(*Binding){
		"billing":        func(b *Binding) { b.Target.BillingPath = "api" },
		"route":          func(b *Binding) { b.Target.Route.ID = "" },
		"route_revision": func(b *Binding) { b.Target.Route.Revision = 0 },
		"plugin":         func(b *Binding) { v := "fixture"; b.Target.PluginVersion = &v },
		"lock":           func(b *Binding) { b.Target.LockEnforcement = stageplan.PrimaryOnly },
		"model":          func(b *Binding) { b.Target.RequestedModel = "fixture\nother" },
		"effort":         func(b *Binding) { b.Target.Effort.Value = nil },
		"effort_mode":    func(b *Binding) { b.Target.Effort.RequestedMode = "" },
		"version":        func(b *Binding) { b.Target.RuntimeVersion = "other" },
		"identity":       func(b *Binding) { b.Target.Account = "other" },
		"scope":          func(b *Binding) { b.Scope.Generation = 0 },
		"path":           func(b *Binding) { b.Cwd = "relative" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			c, f := gatewayFixture(t)
			b := c.binding
			mutate(&b)
			if _, e := NewGateway(f, b, func(Scope) bool { return true }, func() Identity { return f.identity }, func() bool { return true }); e == nil {
				t.Fatal("unsafe target accepted")
			}
		})
	}
}
func TestGatewayNativeRequestPinsFrozenEffortAndPreventsProviderSubstitution(t *testing.T) {
	c, f := gatewayFixture(t)
	old := c.binding
	clone, f2 := gatewayFixture(t)
	b := clone.binding
	frozen := *b.Target.Effort.Value
	x, e := NewGateway(f2, b, func(Scope) bool { return true }, func() Identity { return b.Identity }, func() bool { return true })
	if e != nil {
		t.Fatal(e)
	}
	*b.Target.Effort.Value = "other"
	if *x.binding.Target.Effort.Value != frozen {
		t.Fatal("effort borrowed mutable caller binding")
	}
	c.Initialize(context.Background())
	c.ReadAccount(context.Background())
	var out map[string]any
	json.Unmarshal(f.response["thread/start"], &out)
	out["modelProvider"] = "openai"
	f.response["thread/start"], _ = json.Marshal(out)
	if _, e = c.StartThread(context.Background()); !errors.Is(e, ErrProtocol) || c.State() != "execution_uncertain" {
		t.Fatal("provider substituted", e)
	}
	if c.binding.Identity != old.Identity {
		t.Fatal("identity changed")
	}
}

type gatewayCancelledPeer struct {
	Peer
	cancel context.CancelFunc
}

func (p gatewayCancelledPeer) Call(ctx context.Context, id uint64, m string, raw json.RawMessage) (json.RawMessage, error) {
	v, e := p.Peer.Call(ctx, id, m, raw)
	p.cancel()
	return v, e
}
func TestGatewayLateReplyAfterCancellationCannotInitialize(t *testing.T) {
	c, _ := gatewayFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.peer = gatewayCancelledPeer{Peer: c.peer, cancel: cancel}
	if e := c.Initialize(ctx); !errors.Is(e, context.Canceled) || c.initialized {
		t.Fatal("cancelled reply accepted", e)
	}
}

func TestGatewayLocalAccountDoesNotAdoptWorkspaceRouting(t *testing.T) {
	for _, value := range []string{"null", `{"workspaceId":"other"}`, `false`, `"fixture"`} {
		t.Run(value, func(t *testing.T) {
			c, f := gatewayFixture(t)
			f.response["account/read"] = json.RawMessage(`{"account":null,"requiresOpenaiAuth":false,"workspaceRouting":` + value + `}`)
			c.Initialize(context.Background())
			e := c.ReadAccount(context.Background())
			if (e == nil) != (value == "null") {
				t.Fatal("workspace routing shape accepted incorrectly", e)
			}
		})
	}
}

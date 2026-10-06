package codex

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

const resumeFixtureThread = "12345678-1234-1234-1234-123456789abc"

func resumeResponse(thread string, turns string) json.RawMessage {
	return json.RawMessage(`{"thread":{"id":"` + thread + `","cliVersion":"0.160.0","cwd":"/fixture/workspace","turns":` + turns + `},"model":"fixture-model","modelProvider":"fusion_codex_stage","reasoningEffort":"medium","cwd":"/fixture/workspace","approvalPolicy":"never","sandbox":{"type":"readOnly","networkAccess":false}}`)
}

// A gateway client persists its thread (no ephemeral flag) and can adopt a
// prior persisted thread by exact uuid, capturing its historical turn ids.
func TestGatewayResumeThreadAdoptsPersistedThread(t *testing.T) {
	c, f := gatewayFixture(t)
	if e := c.Initialize(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e := c.ReadAccount(context.Background()); e != nil {
		t.Fatal(e)
	}
	// Non-uuid shapes are refused before any RPC.
	before := len(f.methods)
	f.response["thread/resume"] = resumeResponse("fixture-thread", `[]`)
	if e := c.ResumeThread(context.Background(), "fixture-thread"); !errors.Is(e, ErrProtocol) || len(f.methods) != before {
		t.Fatal("non-uuid resume escaped", e)
	}
	f.response["thread/resume"] = resumeResponse(resumeFixtureThread, `[{"id":"old-turn-1"},{"id":"old-turn-2"}]`)
	if e := c.ResumeThread(context.Background(), resumeFixtureThread); e != nil {
		t.Fatal(e)
	}
	if c.threadID != resumeFixtureThread || len(c.historicalTurns) != 2 || !c.historicalTurns["old-turn-1"] {
		t.Fatal("resumed thread state wrong")
	}
	var params map[string]any
	if json.Unmarshal(f.params[len(f.params)-1], &params) != nil || params["threadId"] != resumeFixtureThread {
		t.Fatal("resume params wrong")
	}
	if _, exists := params["ephemeral"]; exists {
		t.Fatal("resume must not mark the thread ephemeral")
	}
	if e := c.ResumeThread(context.Background(), resumeFixtureThread); !errors.Is(e, ErrProtocol) {
		t.Fatal("second adoption escaped")
	}
}

func TestGatewayPersistedStartOmitsEphemeral(t *testing.T) {
	c, f := gatewayFixture(t)
	if e := c.Initialize(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e := c.ReadAccount(context.Background()); e != nil {
		t.Fatal(e)
	}
	if _, e := c.StartThread(context.Background()); e != nil {
		t.Fatal(e)
	}
	var params map[string]any
	if json.Unmarshal(f.params[len(f.params)-1], &params) != nil {
		t.Fatal("thread/start params unreadable")
	}
	if _, exists := params["ephemeral"]; exists {
		t.Fatal("persisted gateway thread still ephemeral")
	}
	// The default (non-gateway) client keeps the ephemeral vector contract.
	base, bf := newFixture(t)
	initThread(t, base)
	if json.Unmarshal(bf.params[len(bf.params)-1], &params) != nil {
		t.Fatal("default thread/start params unreadable")
	}
	if params["ephemeral"] != true {
		t.Fatal("default client lost ephemeral contract")
	}
}

// gatewayResumed brings a gateway client to a resumed, running turn whose
// thread carries the historical turns old-turn-1/old-turn-2.
func gatewayResumed(t *testing.T) (*Client, *fixture) {
	t.Helper()
	c, f := gatewayFixture(t)
	if e := c.Initialize(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e := c.ReadAccount(context.Background()); e != nil {
		t.Fatal(e)
	}
	f.response["thread/resume"] = resumeResponse(resumeFixtureThread, `[{"id":"old-turn-1"},{"id":"old-turn-2"}]`)
	if e := c.ResumeThread(context.Background(), resumeFixtureThread); e != nil {
		t.Fatal(e)
	}
	if _, e := c.StartTurn(context.Background(), "fixture prompt"); e != nil {
		t.Fatal(e)
	}
	return c, f
}

// The resumed thread's pre-turn advisory and historical-turn usage frames are
// attributable; unknown turn ids and malformed notices still poison the turn.
func TestGatewayResumeEventsFollowPinnedLifecycle(t *testing.T) {
	c, f := gatewayResumed(t)
	events := [][]byte{
		gatewayNotification("remoteControl/status/changed", `{"installationId":"fixture","serverName":"","status":"disabled","environmentId":null}`),
		gatewayNotification("deprecationNotice", `{"summary":"Full-history hydration is deprecated for paginated threads.","details":null}`),
		gatewayNotification("thread/status/changed", `{"threadId":"`+resumeFixtureThread+`","status":{"type":"idle"}}`),
		gatewayNotification("thread/tokenUsage/updated", `{"threadId":"`+resumeFixtureThread+`","turnId":"old-turn-1","tokenUsage":{"total":{"inputTokens":3,"cachedInputTokens":0,"outputTokens":2,"reasoningOutputTokens":0,"totalTokens":5},"last":{"inputTokens":3,"cachedInputTokens":0,"outputTokens":2,"reasoningOutputTokens":0,"totalTokens":5},"modelContextWindow":null}}`),
		gatewayNotification("warning", `{"message":"fixture warning","threadId":null}`),
		gatewayNotification("thread/status/changed", `{"threadId":"`+resumeFixtureThread+`","status":{"type":"active","activeFlags":[]}}`),
		gatewayNotification("turn/started", `{"threadId":"`+resumeFixtureThread+`","turn":{"id":"fixture-turn","items":[],"status":"inProgress"}}`),
		gatewayNotification("item/started", `{"threadId":"`+resumeFixtureThread+`","turnId":"fixture-turn","startedAtMs":null,"item":{"id":"fixture-user","type":"userMessage","content":[{"type":"text","text":"fixture","text_elements":[]}]}}`),
		gatewayNotification("item/completed", `{"threadId":"`+resumeFixtureThread+`","turnId":"fixture-turn","completedAtMs":1,"item":{"id":"fixture-user","type":"userMessage","content":[{"type":"text","text":"fixture","text_elements":[]}]}}`),
		gatewayNotification("item/started", `{"threadId":"`+resumeFixtureThread+`","turnId":"fixture-turn","startedAtMs":1,"item":{"id":"fixture-answer","type":"agentMessage","text":""}}`),
		gatewayNotification("item/agentMessage/delta", `{"threadId":"`+resumeFixtureThread+`","turnId":"fixture-turn","itemId":"fixture-answer","delta":"fixture"}`),
		gatewayNotification("item/completed", `{"threadId":"`+resumeFixtureThread+`","turnId":"fixture-turn","completedAtMs":2,"item":{"id":"fixture-answer","type":"agentMessage","text":"fixture","phase":"final_answer"}}`),
		gatewayNotification("thread/tokenUsage/updated", `{"threadId":"`+resumeFixtureThread+`","turnId":"fixture-turn","tokenUsage":{"total":{"inputTokens":6,"cachedInputTokens":0,"outputTokens":4,"reasoningOutputTokens":0,"totalTokens":10},"last":{"inputTokens":3,"cachedInputTokens":0,"outputTokens":2,"reasoningOutputTokens":0,"totalTokens":5},"modelContextWindow":null}}`),
		gatewayNotification("thread/status/changed", `{"threadId":"`+resumeFixtureThread+`","status":{"type":"idle"}}`),
	}
	for _, raw := range events {
		state, e := c.Event(f.scope, raw)
		if e != nil || state != "running" {
			t.Fatal("resumed observation rejected", state, e, string(raw))
		}
	}
	raw := gatewayNotification("turn/completed", `{"threadId":"`+resumeFixtureThread+`","turn":{"id":"fixture-turn","items":[],"status":"completed"}}`)
	if state, e := c.Event(f.scope, raw); e != nil || state != "succeeded" {
		t.Fatal(state, e)
	}
}

func TestGatewayResumeEventsRejectUnknownTurnAndMalformedNotice(t *testing.T) {
	cases := map[string][]byte{
		"unknown_turn_usage": gatewayNotification("thread/tokenUsage/updated", `{"threadId":"`+resumeFixtureThread+`","turnId":"unknown-turn","tokenUsage":{"total":{"inputTokens":3,"cachedInputTokens":0,"outputTokens":2,"reasoningOutputTokens":0,"totalTokens":5},"last":{"inputTokens":3,"cachedInputTokens":0,"outputTokens":2,"reasoningOutputTokens":0,"totalTokens":5},"modelContextWindow":null}}`),
		"notice_missing_summary": gatewayNotification("deprecationNotice", `{"details":null}`),
		"notice_extra_field":     gatewayNotification("deprecationNotice", `{"summary":"fixture","account":"other"}`),
		"notice_object_summary":  gatewayNotification("deprecationNotice", `{"summary":{"nested":"fixture"},"details":null}`),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			c, f := gatewayResumed(t)
			state, e := c.Event(f.scope, raw)
			if e == nil || state != "execution_uncertain" {
				t.Fatal("unsafe resumed event accepted", state, e)
			}
			if _, e = c.StartTurn(context.Background(), "replay"); e == nil {
				t.Fatal("uncertain resumed turn replayed")
			}
		})
	}
}

func TestGatewayResumeEchoAndTurnDriftRejected(t *testing.T) {
	c, f := gatewayFixture(t)
	if e := c.Initialize(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e := c.ReadAccount(context.Background()); e != nil {
		t.Fatal(e)
	}
	f.response["thread/resume"] = resumeResponse(resumeFixtureThread, `[{"id":"dup"},{"id":"dup"}]`)
	if e := c.ResumeThread(context.Background(), resumeFixtureThread); !errors.Is(e, ErrProtocol) {
		t.Fatal("duplicate historical turn escaped", e)
	}
	// The poisoned client must refuse a further adoption attempt.
	f.response["thread/resume"] = resumeResponse("87654321-4321-4321-4321-210987654321", `[]`)
	if e := c.ResumeThread(context.Background(), resumeFixtureThread); !errors.Is(e, ErrProtocol) {
		t.Fatal("echo drift escaped", e)
	}
}

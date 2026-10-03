package codex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

type fixture struct {
	methods  []string
	params   []json.RawMessage
	response map[string]json.RawMessage
	scope    Scope
	identity Identity
	current  bool
	admitted bool
}

func (f *fixture) Call(_ context.Context, _ uint64, m string, p json.RawMessage) (json.RawMessage, error) {
	f.methods = append(f.methods, m)
	f.params = append(f.params, p)
	r, ok := f.response[m]
	if !ok {
		return nil, errors.New("fixture private upstream secret")
	}
	return r, nil
}
func (f *fixture) Notify(_ context.Context, m string, p json.RawMessage) error {
	f.methods = append(f.methods, m)
	f.params = append(f.params, p)
	return nil
}
func newFixture(t *testing.T) (*Client, *fixture) {
	t.Helper()
	f := &fixture{current: true, admitted: true, scope: Scope{RunID: "fixture-run", Generation: 1, Role: stageplan.Design}, identity: Identity{Account: "fixture-account", Workspace: "fixture-workspace", CredentialIdentity: "fixture-credential", Generation: 1}, response: pinnedVectors(t).Responses}
	effort := "medium"
	target := stageplan.ExecutionTarget{RequestedModel: "fixture-model", Account: "fixture-account", Workspace: "fixture-workspace", CredentialIdentity: "fixture-credential", ResolvedModel: "fixture-model", RuntimeVersion: "0.160.0", LockEnforcement: stageplan.ControlledCalls, Effort: stageplan.FrozenEffort{Value: &effort}}
	c, e := New(f, Binding{Scope: f.scope, Identity: f.identity, Target: target, Cwd: "/fixture/workspace", CodexHome: "/fixture/private-home/.codex"}, func(s Scope) bool { return f.current && s == f.scope }, func() Identity { return f.identity }, func() bool { return f.admitted })
	if e != nil {
		t.Fatal(e)
	}
	return c, f
}

type vectors struct {
	Requests, Responses   map[string]json.RawMessage
	TerminalNotifications map[string]json.RawMessage `json:"terminal_notifications"`
}

func pinnedVectors(t *testing.T) vectors {
	t.Helper()
	b, e := os.ReadFile("testdata/native-0.160.0/vectors.json")
	if e != nil {
		t.Fatal(e)
	}
	var v vectors
	if e = json.Unmarshal(b, &v); e != nil {
		t.Fatal(e)
	}
	return v
}
func TestProtocolMatchesPinnedSchemaValidatedVectors(t *testing.T) {
	c, f := newFixture(t)
	initThread(t, c)
	if _, e := c.StartTurn(context.Background(), "fixture prompt"); e != nil {
		t.Fatal(e)
	}
	if e := c.Interrupt(context.Background()); e != nil {
		t.Fatal(e)
	}
	v := pinnedVectors(t)
	for i, method := range f.methods {
		if method == "initialized" {
			continue
		}
		var got, want any
		json.Unmarshal(f.params[i], &got)
		json.Unmarshal(v.Requests[method], &want)
		g, _ := json.Marshal(got)
		w, _ := json.Marshal(want)
		if string(g) != string(w) {
			t.Fatal("pinned request drift", method)
		}
	}
	state, e := c.Event(f.scope, append(append([]byte(`{"method":"turn/completed","params":`), v.TerminalNotifications["interrupted"]...), '}'))
	if e != nil || state != "interrupted" {
		t.Fatal(state, e)
	}
}
func initThread(t *testing.T, c *Client) {
	t.Helper()
	if e := c.Initialize(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e := c.ReadAccount(context.Background()); e != nil {
		t.Fatal(e)
	}
	if _, e := c.StartThread(context.Background()); e != nil {
		t.Fatal(e)
	}
}
func TestProtocolPinsTargetAndUsesNativeTerminalState(t *testing.T) {
	c, f := newFixture(t)
	initThread(t, c)
	id, e := c.StartTurn(context.Background(), "fixture prompt")
	if e != nil || id != "fixture-turn" {
		t.Fatal(e)
	}
	var p map[string]any
	json.Unmarshal(f.params[len(f.params)-1], &p)
	if p["model"] != "fixture-model" || p["effort"] != "medium" || p["threadId"] != "fixture-thread" || p["summary"] != "none" || p["approvalPolicy"] != "never" {
		t.Fatal("target overrides")
	}
	if _, e = c.Event(f.scope, []byte(`{"method":"item/agentMessage/delta","params":{"threadId":"fixture-thread","turnId":"fixture-turn","delta":"completed"}}`)); e != nil {
		t.Fatal(e)
	}
	if c.State() != "running" {
		t.Fatal("text treated as success")
	}
	state, e := c.Event(f.scope, []byte(`{"method":"turn/completed","params":{"threadId":"fixture-thread","turn":{"id":"fixture-turn","items":[],"status":"completed"}}}`))
	if e != nil || state != "succeeded" {
		t.Fatal(state, e)
	}
	if _, e = c.Event(f.scope, []byte(`{"method":"turn/completed","params":{"threadId":"fixture-thread","turn":{"id":"fixture-turn","items":[],"status":"failed"}}}`)); !errors.Is(e, ErrProtocol) {
		t.Fatal("terminal overwritten")
	}
}
func TestUnverifiedRouteOrChangedIdentitySendsNoGeneration(t *testing.T) {
	for _, mode := range []string{"unverified", "account", "workspace", "credential", "generation", "scope"} {
		t.Run(mode, func(t *testing.T) {
			c, f := newFixture(t)
			initThread(t, c)
			before := len(f.methods)
			switch mode {
			case "unverified":
				f.admitted = false
			case "account":
				f.identity.Account = "other"
			case "workspace":
				f.identity.Workspace = "other"
			case "credential":
				f.identity.CredentialIdentity = "other"
			case "generation":
				f.identity.Generation++
			case "scope":
				f.current = false
			}
			if _, e := c.StartTurn(context.Background(), "fixture"); e == nil {
				t.Fatal("unsafe generation accepted")
			}
			if len(f.methods) != before {
				t.Fatal("blocked request escaped")
			}
		})
	}
}
func TestNativeErrorAndInterruptedAreSanitized(t *testing.T) {
	for _, status := range []string{"failed", "interrupted"} {
		t.Run(status, func(t *testing.T) {
			c, f := newFixture(t)
			initThread(t, c)
			c.StartTurn(context.Background(), "fixture")
			raw := []byte(`{"method":"turn/completed","params":{"threadId":"fixture-thread","turn":{"id":"fixture-turn","items":[],"status":"` + status + `","error":{"message":"fixture private secret"}}}}`)
			state, e := c.Event(f.scope, raw)
			if e != nil || state != status {
				t.Fatal(state, e)
			}
			if strings.Contains(c.State(), "secret") {
				t.Fatal("error leaked")
			}
		})
	}
	c, f := newFixture(t)
	delete(f.response, "initialize")
	if e := c.Initialize(context.Background()); e == nil || strings.Contains(e.Error(), "secret") {
		t.Fatal("RPC error leaked")
	}
}
func TestResumeKeepsAccountAndEffortAndNeverInventsCapabilities(t *testing.T) {
	c, f := newFixture(t)
	initThread(t, c)
	f.identity.CredentialIdentity = "other"
	n := len(f.methods)
	if e := c.Resume(context.Background(), "fixture-thread"); e == nil || len(f.methods) != n {
		t.Fatal("old thread resumed on other identity")
	}
}
func TestRerouteNestedCallsAndStaleEventsBlockStrictStatus(t *testing.T) {
	for _, method := range []string{"model/rerouted", "thread/compacted", "item/started"} {
		t.Run(method, func(t *testing.T) {
			c, f := newFixture(t)
			initThread(t, c)
			c.StartTurn(context.Background(), "fixture")
			raw := []byte(`{"method":"` + method + `","params":{"threadId":"fixture-thread","turnId":"fixture-turn","item":{"type":"collabAgentToolCall","id":"fixture-item"}}}`)
			state, e := c.Event(f.scope, raw)
			if e == nil || state != "execution_uncertain" {
				t.Fatal("internal call accepted", state, e)
			}
		})
	}
	c, f := newFixture(t)
	initThread(t, c)
	c.StartTurn(context.Background(), "fixture")
	scope := f.scope
	scope.Generation++
	if _, e := c.Event(scope, []byte(`{}`)); e == nil {
		t.Fatal("stale event accepted")
	}
}
func TestProtocolRejectsDuplicateKeysAndResolvedDrift(t *testing.T) {
	c, f := newFixture(t)
	f.response["initialize"] = json.RawMessage(`{"userAgent":"fixture/0.160.0","userAgent":"fixture/older"}`)
	if e := c.Initialize(context.Background()); e == nil {
		t.Fatal("duplicate accepted")
	}
	for _, field := range []string{"model", "reasoningEffort", "modelProvider", "cwd"} {
		t.Run(field, func(t *testing.T) {
			c, f := newFixture(t)
			c.Initialize(context.Background())
			c.ReadAccount(context.Background())
			var d map[string]any
			json.Unmarshal(f.response["thread/start"], &d)
			d[field] = "other"
			f.response["thread/start"], _ = json.Marshal(d)
			if _, e := c.StartThread(context.Background()); e == nil {
				t.Fatal("resolved drift accepted")
			}
		})
	}
}

func TestUnacknowledgedTurnCannotBeBlindlyReplayed(t *testing.T) {
	c, f := newFixture(t)
	initThread(t, c)
	delete(f.response, "turn/start")
	if _, e := c.StartTurn(context.Background(), "fixture"); e == nil {
		t.Fatal("lost acknowledgement accepted")
	}
	if c.State() != "execution_uncertain" {
		t.Fatal("lost start left retryable state")
	}
	n := len(f.methods)
	if _, e := c.StartTurn(context.Background(), "fixture"); e == nil || len(f.methods) != n {
		t.Fatal("uncertain turn replayed")
	}
}
func TestMissingNativeItemsOrContradictoryErrorCannotSucceed(t *testing.T) {
	for _, turn := range []string{`{"id":"fixture-turn","status":"completed"}`, `{"id":"fixture-turn","items":[],"status":"completed","error":{"message":"fixture error"}}`} {
		c, f := newFixture(t)
		initThread(t, c)
		c.StartTurn(context.Background(), "fixture")
		if _, e := c.Event(f.scope, []byte(`{"method":"turn/completed","params":{"threadId":"fixture-thread","turn":`+turn+`}}`)); e == nil {
			t.Fatal("invalid native completion accepted")
		}
	}
}

func TestFiveRolesUseApprovedNativeWriteBoundary(t *testing.T) {
	for _, role := range stageplan.AllRoles() {
		t.Run(string(role), func(t *testing.T) {
			c, f := newFixture(t)
			f.scope.Role = role
			c.binding.Scope.Role = role
			writing := role == stageplan.Implementation || role == stageplan.Testing
			if writing {
				var d map[string]any
				json.Unmarshal(f.response["thread/start"], &d)
				d["sandbox"] = map[string]any{"type": "workspaceWrite", "networkAccess": false, "writableRoots": []string{c.binding.Cwd}, "excludeSlashTmp": true, "excludeTmpdirEnvVar": true}
				f.response["thread/start"], _ = json.Marshal(d)
			}
			initThread(t, c)
			c.StartTurn(context.Background(), "fixture")
			var params map[string]any
			json.Unmarshal(f.params[len(f.params)-1], &params)
			policy := params["sandboxPolicy"].(map[string]any)
			expected := "readOnly"
			if writing {
				expected = "workspaceWrite"
			}
			if policy["type"] != expected || policy["networkAccess"] != false {
				t.Fatal("role boundary wrong")
			}
			if writing {
				roots := policy["writableRoots"].([]any)
				if len(roots) != 1 || roots[0] != c.binding.Cwd || policy["excludeSlashTmp"] != true || policy["excludeTmpdirEnvVar"] != true {
					t.Fatal("broadened native writer")
				}
			}
		})
	}
}

func TestInitializeRequiresExactVersionAndExplicitLogin(t *testing.T) {
	c, f := newFixture(t)
	if e := c.Initialize(context.Background()); e != nil {
		t.Fatal(e)
	}
	var params map[string]any
	json.Unmarshal(f.params[0], &params)
	capabilities, ok := params["capabilities"].(map[string]any)
	if !ok || capabilities["explicitGatewayOauth"] != true {
		t.Fatal("automatic native gateway login not disabled")
	}
	c, f = newFixture(t)
	f.response["initialize"] = json.RawMessage(`{"userAgent":"fixture-codex/0.160.00","platformFamily":"unix","platformOs":"macos","codexHome":"/fixture/private-home"}`)
	if e := c.Initialize(context.Background()); e == nil {
		t.Fatal("version prefix accepted")
	}
}
func TestThreadStartupRequiresAdmissionAndExplicitNetworkPolicy(t *testing.T) {
	c, f := newFixture(t)
	c.Initialize(context.Background())
	c.ReadAccount(context.Background())
	f.admitted = false
	n := len(f.methods)
	if _, e := c.StartThread(context.Background()); e == nil || len(f.methods) != n {
		t.Fatal("unadmitted thread started")
	}
	c, f = newFixture(t)
	c.Initialize(context.Background())
	c.ReadAccount(context.Background())
	var response map[string]any
	json.Unmarshal(f.response["thread/start"], &response)
	delete(response["sandbox"].(map[string]any), "networkAccess")
	f.response["thread/start"], _ = json.Marshal(response)
	if _, e := c.StartThread(context.Background()); e == nil {
		t.Fatal("missing policy treated as network disabled")
	}
}
func TestHiddenCallsInItemsOrCompletionCannotSucceed(t *testing.T) {
	for _, kind := range []string{"contextCompaction", "subAgentActivity", "imageGeneration", "mcpToolCall", "dynamicToolCall", "hookPrompt"} {
		for _, terminal := range []bool{false, true} {
			c, f := newFixture(t)
			initThread(t, c)
			c.StartTurn(context.Background(), "fixture")
			item := `{"type":"` + kind + `","id":"fixture-item"}`
			raw := `{"method":"item/started","params":{"threadId":"fixture-thread","turnId":"fixture-turn","item":` + item + `}}`
			if terminal {
				raw = `{"method":"turn/completed","params":{"threadId":"fixture-thread","turn":{"id":"fixture-turn","status":"completed","items":[` + item + `]}}}`
			}
			state, e := c.Event(f.scope, []byte(raw))
			if !errors.Is(e, ErrUnverified) || state != "execution_uncertain" {
				t.Fatal("hidden operation accepted", kind, terminal, state, e)
			}
		}
	}
}

func TestInitializeCannotBorrowOtherNativeHome(t *testing.T) {
	c, f := newFixture(t)
	var response map[string]any
	json.Unmarshal(f.response["initialize"], &response)
	response["codexHome"] = "/fixture/daily-home/.codex"
	f.response["initialize"], _ = json.Marshal(response)
	if e := c.Initialize(context.Background()); e == nil {
		t.Fatal("unexpected native home accepted")
	}
}
func TestUnknownItemOrReadonlyFileChangeMakesExecutionUncertain(t *testing.T) {
	for _, kind := range []string{"futureNativeOperation", "fileChange"} {
		c, f := newFixture(t)
		initThread(t, c)
		c.StartTurn(context.Background(), "fixture")
		state, e := c.Event(f.scope, []byte(`{"method":"item/started","params":{"threadId":"fixture-thread","turnId":"fixture-turn","item":{"id":"fixture-item","type":"`+kind+`"}}}`))
		if e == nil || state != "execution_uncertain" {
			t.Fatal("unadmitted operation accepted", kind, state, e)
		}
	}
}

func TestDecodeRejectsUnicodeCaseAliasesAndInvalidUTF8(t *testing.T) {
	for _, raw := range [][]byte{[]byte(`{"ſtatus":"other","status":"completed"}`), append([]byte(`{"text":"`), 0xff, '"', '}')} {
		var v any
		if decode(raw, &v) == nil {
			t.Fatal("ambiguous native JSON accepted")
		}
	}
}

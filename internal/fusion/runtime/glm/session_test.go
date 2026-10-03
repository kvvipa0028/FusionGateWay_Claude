package glm

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

func nativeFrames(t *testing.T) [][]byte {
	t.Helper()
	raw, e := os.ReadFile("testdata/native-2.1.287/tool-free-stream.jsonl")
	if e != nil {
		t.Fatal(e)
	}
	raw = bytes.ReplaceAll(raw, []byte("<fixture-root>"), []byte("/fixture-root"))
	return bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
}
func fixtureSession(t *testing.T) (*Session, Binding, *bool) {
	t.Helper()
	frames := nativeFrames(t)
	var init struct {
		SessionID string `json:"session_id"`
	}
	json.Unmarshal(frames[1], &init)
	effort := "none"
	b := Binding{RunID: "fixture-run", Generation: 1, Role: stageplan.Design, NativeSessionID: init.SessionID, Cwd: "/fixture-root/project", Endpoint: Endpoint, Region: "CN", MaxTurns: 1, Target: stageplan.ExecutionTarget{RequestedModel: "glm-5.3", ResolvedModel: "glm-5.3", RuntimeVersion: CLIVersion, Account: "fixture-account", Workspace: "fixture-workspace", CredentialIdentity: "fixture-credential", BillingPath: "coding_plan", LockEnforcement: stageplan.ControlledCalls, Effort: stageplan.FrozenEffort{Value: &effort}}}
	current := true
	s, e := New(b, func(got Binding) bool {
		return current && got.RunID == b.RunID && got.Generation == b.Generation && got.Target.Account == b.Target.Account && got.Target.CredentialIdentity == b.Target.CredentialIdentity
	})
	if e != nil {
		t.Fatal(e)
	}
	return s, b, &current
}
func feed(t *testing.T, s *Session, b Binding, frames [][]byte) {
	t.Helper()
	for _, raw := range frames {
		if _, e := s.Event(b.RunID, b.Generation, raw); e != nil {
			t.Fatal(e)
		}
	}
}
func TestNativeStreamResultAndEOFRequiredTogether(t *testing.T) {
	s, b, _ := fixtureSession(t)
	frames := nativeFrames(t)
	feed(t, s, b, frames[:len(frames)-1])
	if s.State() == "succeeded" {
		t.Fatal("assistant text treated as terminal")
	}
	feed(t, s, b, frames[len(frames)-1:])
	if s.State() == "succeeded" {
		t.Fatal("terminal before clean EOF")
	}
	got, e := s.Finish(0)
	if e != nil || got.State != "succeeded" {
		t.Fatal(got, e)
	}
	if got.StrictLockVerified || got.BillingVerified || got.QuotaVerified || got.UpstreamVerified {
		t.Fatal("native metadata promoted to trusted route evidence")
	}
	if got.NativeModel != "glm-5.3" || got.SessionID != b.NativeSessionID || got.BillingPath != "coding_plan" {
		t.Fatal("native billing label replaced frozen route")
	}
	if got.InputTokens != 8 || got.OutputTokens != 8 {
		t.Fatal("native usage mismatch")
	}
}

func TestPinnedAPIRetryIsBoundedInformationNotACallPermit(t *testing.T) {
	for _, mode := range []string{"valid", "missing", "attempt", "max", "delay", "status", "active_message", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			s, b, _ := fixtureSession(t)
			frames := nativeFrames(t)
			feed(t, s, b, frames[:2])
			d := map[string]any{"type": "system", "subtype": "api_retry", "session_id": b.NativeSessionID, "attempt": 1, "max_retries": 10, "retry_delay_ms": 565, "error_status": 429}
			switch mode {
			case "missing":
				delete(d, "max_retries")
			case "attempt":
				d["attempt"] = 0
			case "max":
				d["max_retries"] = 11
			case "delay":
				d["retry_delay_ms"] = 60001
			case "status":
				d["error_status"] = 401
			case "active_message":
				feed(t, s, b, frames[2:4])
			case "cancelled":
				if e := s.Cancel(); e != nil {
					t.Fatal(e)
				}
			}
			raw, _ := json.Marshal(d)
			_, e := s.Event(b.RunID, b.Generation, raw)
			if mode != "valid" {
				if e == nil {
					t.Fatal("unsafe retry metadata accepted", mode)
				}
				return
			}
			if e != nil {
				t.Fatal("pinned retry metadata rejected", e)
			}
			feed(t, s, b, frames[2:])
			got, e := s.Finish(0)
			if e != nil || got.State != "succeeded" || got.ObservedMessages != 1 || got.StrictLockVerified || got.BillingVerified || got.QuotaVerified {
				t.Fatal("retry metadata became admission or call evidence", got.State, e)
			}
		})
	}
}
func TestLostResultOrExitErrorNeverSucceeds(t *testing.T) {
	for _, exit := range []int{0, 1} {
		s, b, _ := fixtureSession(t)
		frames := nativeFrames(t)
		feed(t, s, b, frames[:len(frames)-1])
		got, _ := s.Finish(exit)
		if got.State != "execution_uncertain" {
			t.Fatal("lost result accepted", got)
		}
	}
	s, b, _ := fixtureSession(t)
	feed(t, s, b, nativeFrames(t))
	got, _ := s.Finish(1)
	if got.State != "execution_uncertain" {
		t.Fatal("contradictory exit accepted")
	}
}
func TestFrozenIdentitySessionVersionAndModelDriftAreRejected(t *testing.T) {
	for _, field := range []string{"model", "claude_code_version", "cwd", "session_id"} {
		t.Run(field, func(t *testing.T) {
			s, b, _ := fixtureSession(t)
			frames := nativeFrames(t)
			feed(t, s, b, frames[:1])
			var d map[string]any
			json.Unmarshal(frames[1], &d)
			d[field] = "fixture-other"
			raw, _ := json.Marshal(d)
			if _, e := s.Event(b.RunID, b.Generation, raw); e == nil {
				t.Fatal("native drift accepted", field)
			}
			if s.State() != "execution_uncertain" {
				t.Fatal("unsafe native state remained retryable")
			}
		})
	}
	s, b, current := fixtureSession(t)
	*current = false
	if _, e := s.Event(b.RunID, b.Generation, nativeFrames(t)[0]); !errors.Is(e, ErrIdentity) {
		t.Fatal("current lease/identity ignored")
	}
}
func TestStaleScopeDoesNotPoisonCurrentNativeSession(t *testing.T) {
	s, b, _ := fixtureSession(t)
	if _, e := s.Event(b.RunID, b.Generation+1, []byte(`{}`)); !errors.Is(e, ErrIdentity) {
		t.Fatal("old generation accepted")
	}
	feed(t, s, b, nativeFrames(t))
	got, e := s.Finish(0)
	if e != nil || got.State != "succeeded" {
		t.Fatal("stale event poisoned current run")
	}
}
func TestDuplicateMalformedAndUnknownFramesFailClosed(t *testing.T) {
	for _, raw := range []string{`null`, `{"type":"system","type":"result"}`, `{"type":"system","TYPE":"result"}`, `{} {}`, `{"type":"future_native_event","session_id":"fixture"}`, strings.Repeat("x", (1<<20)+1)} {
		s, b, _ := fixtureSession(t)
		if _, e := s.Event(b.RunID, b.Generation, []byte(raw)); e == nil {
			t.Fatal("unsupported frame accepted")
		}
	}
}
func TestResultModelUsageAndHiddenCallsCannotPass(t *testing.T) {
	for _, mode := range []string{"second_model", "subagent", "missing_subagent_stats", "permission_denial", "turn_count", "result_index"} {
		t.Run(mode, func(t *testing.T) {
			s, b, _ := fixtureSession(t)
			frames := nativeFrames(t)
			feed(t, s, b, frames[:len(frames)-1])
			var d map[string]any
			json.Unmarshal(frames[len(frames)-1], &d)
			switch mode {
			case "second_model":
				d["modelUsage"].(map[string]any)["fixture-other-model"] = map[string]any{}
			case "subagent":
				d["subagent_stats"].(map[string]any)["spawned"] = float64(1)
			case "missing_subagent_stats":
				delete(d, "subagent_stats")
			case "permission_denial":
				d["permission_denials"] = []any{map[string]any{"tool_name": "Bash"}}
			case "turn_count":
				d["num_turns"] = float64(2)
			case "result_index":
				d["result_index"] = float64(1)
			}
			raw, _ := json.Marshal(d)
			if _, e := s.Event(b.RunID, b.Generation, raw); e == nil {
				t.Fatal("unsafe native result accepted", mode)
			}
			got, _ := s.Finish(0)
			if got.State == "succeeded" {
				t.Fatal("unsafe result succeeded")
			}
		})
	}
}
func TestNativeFailureIsSanitizedAndCancellationCannotInventCompletion(t *testing.T) {
	s, b, _ := fixtureSession(t)
	frames := nativeFrames(t)
	feed(t, s, b, frames[:len(frames)-1])
	raw := []byte(`{"type":"result","subtype":"error_during_execution","is_error":true,"session_id":"` + b.NativeSessionID + `","terminal_reason":"error","errors":["fixture-private-secret"],"api_error_status":401}`)
	if _, e := s.Event(b.RunID, b.Generation, raw); e != nil {
		t.Fatal(e)
	}
	got, e := s.Finish(1)
	if e != nil || got.State != "failed" || got.APIErrorStatus != 401 {
		t.Fatal(got, e)
	}
	encoded, _ := json.Marshal(got)
	if bytes.Contains(encoded, []byte("secret")) {
		t.Fatal("native error leaked")
	}
	s, b, _ = fixtureSession(t)
	feed(t, s, b, frames[:2])
	if e = s.Cancel(); e != nil {
		t.Fatal(e)
	}
	got, _ = s.Finish(143)
	if got.State != "execution_uncertain" {
		t.Fatal("OS signal treated as native completion")
	}
}
func TestPermissionRequestsAlwaysReturnNativeDeny(t *testing.T) {
	s, b, _ := fixtureSession(t)
	feed(t, s, b, nativeFrames(t)[:2])
	raw := []byte(`{"type":"control_request","request_id":"fixture-request","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"fixture-private-secret"}}}`)
	reply, e := s.Event(b.RunID, b.Generation, raw)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Contains(reply, []byte(`"behavior":"deny"`)) || bytes.Contains(reply, []byte("private-secret")) {
		t.Fatal("permission not safely denied", string(reply))
	}
}
func TestSessionNeverAdoptsChangedBindingOrAccountAtResume(t *testing.T) {
	s, b, _ := fixtureSession(t)
	feed(t, s, b, nativeFrames(t))
	if _, e := s.Finish(0); e != nil {
		t.Fatal(e)
	}
	b.Target.Account = "fixture-other"
	if e := s.Resume(b); !errors.Is(e, ErrUnsupported) {
		t.Fatal("unproven native persistence adopted old session")
	}
}

func TestNativeOwnedReadSupportsTwoBudgetedModelRounds(t *testing.T) {
	raw, e := os.ReadFile("testdata/native-2.1.287/read-tool-stream.jsonl")
	if e != nil {
		t.Fatal(e)
	}
	frames := bytes.Split(bytes.TrimSpace(bytes.ReplaceAll(raw, []byte("<fixture-root>"), []byte("/fixture-root"))), []byte("\n"))
	_, b, _ := fixtureSession(t)
	var init struct {
		SessionID string `json:"session_id"`
	}
	json.Unmarshal(frames[1], &init)
	b.NativeSessionID = init.SessionID
	b.Tools = []string{"Read"}
	b.MaxTurns = 2
	s, e := New(b, func(Binding) bool { return true })
	if e != nil {
		t.Fatal(e)
	}
	feed(t, s, b, frames)
	got, e := s.Finish(0)
	if e != nil || got.State != "succeeded" || got.ObservedMessages != 2 || got.InputTokens != 16 || got.OutputTokens != 16 {
		t.Fatal("bounded multi-round protocol mismatch", got, e)
	}
	if got.StrictLockVerified {
		t.Fatal("observed fake requests promoted to all-calls control")
	}
}
func TestCaseFoldAliasesAndInvalidUTF8CannotOverrideNativeFields(t *testing.T) {
	for _, raw := range [][]byte{[]byte(`{"ſession_id":"fixture-other","session_id":"fixture","type":"system"}`), append([]byte(`{"type":"system","text":"`), 0xff, '"', '}')} {
		var v any
		if decode(raw, &v) == nil {
			t.Fatal("ambiguous native frame accepted")
		}
	}
}
func TestUnmatchedToolResultCannotValidateNativeSuccess(t *testing.T) {
	s, b, _ := fixtureSession(t)
	feed(t, s, b, nativeFrames(t)[:2])
	_, e := s.Event(b.RunID, b.Generation, []byte(`{"type":"user","session_id":"`+b.NativeSessionID+`","message":{"content":[{"type":"tool_result","tool_use_id":"fixture-not-started","content":"passed"}]}}`))
	if e == nil || s.State() != "execution_uncertain" {
		t.Fatal("unmatched tool result accepted")
	}
}

func TestIncompleteOrDuplicateNativeMessagesCannotSucceed(t *testing.T) {
	for _, omitted := range []string{"assistant", "message_stop", "content_block_stop"} {
		t.Run(omitted, func(t *testing.T) {
			s, b, _ := fixtureSession(t)
			rejected := false
			for _, raw := range nativeFrames(t) {
				var frame struct {
					Type  string
					Event struct{ Type string }
				}
				json.Unmarshal(raw, &frame)
				if frame.Type == omitted || frame.Event.Type == omitted {
					continue
				}
				if _, e := s.Event(b.RunID, b.Generation, raw); e != nil {
					rejected = true
					break
				}
			}
			got, _ := s.Finish(0)
			if !rejected && got.State == "succeeded" {
				t.Fatal("incomplete native message succeeded")
			}
		})
	}
	s, b, _ := fixtureSession(t)
	for _, raw := range nativeFrames(t) {
		if _, e := s.Event(b.RunID, b.Generation, raw); e != nil {
			t.Fatal(e)
		}
		var frame struct{ Type string }
		json.Unmarshal(raw, &frame)
		if frame.Type == "assistant" {
			if _, e := s.Event(b.RunID, b.Generation, raw); e == nil {
				t.Fatal("duplicate assistant aggregate accepted")
			}
			break
		}
	}
}
func TestOrphanNativeStreamTransitionsAreRejected(t *testing.T) {
	for _, event := range []string{`{"type":"message_stop"}`, `{"type":"message_delta","delta":{}}`, `{"type":"content_block_stop","index":0}`, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"passed"}}`} {
		s, b, _ := fixtureSession(t)
		feed(t, s, b, nativeFrames(t)[:2])
		raw := []byte(`{"type":"stream_event","session_id":"` + b.NativeSessionID + `","event":` + event + `}`)
		if _, e := s.Event(b.RunID, b.Generation, raw); e == nil {
			t.Fatal("orphan stream transition accepted")
		}
	}
}

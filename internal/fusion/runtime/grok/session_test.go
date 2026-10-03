package grok

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

func frames(t *testing.T) [][]byte {
	t.Helper()
	raw, e := os.ReadFile("testdata/native-1.0.48/restricted-stream.jsonl")
	if e != nil {
		t.Fatal(e)
	}
	return bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
}
func fixture(t *testing.T) (*Session, Binding, *bool) {
	t.Helper()
	b := Binding{RunID: "fixture-run", Generation: 1, Role: stageplan.Design, NativeSessionID: "11111111-1111-4111-8111-111111111111", Cwd: "/fixture/workspace", RuntimeVersion: CLIVersion, ExecutableSHA256: NativeExecutableSHA256, Model: "fixture-model", Tools: []string{"read_file"}, MaxTurns: 1}
	current := true
	s, e := New(b, func(got Binding) bool {
		return current && got.RunID == b.RunID && got.Generation == b.Generation && got.Cwd == b.Cwd && got.Model == b.Model
	})
	if e != nil {
		t.Fatal(e)
	}
	return s, b, &current
}
func feed(t *testing.T, s *Session, b Binding, fs [][]byte) {
	t.Helper()
	for _, raw := range fs {
		if e := s.Event(b.RunID, b.Generation, raw); e != nil {
			t.Fatal(e)
		}
	}
}
func edited(t *testing.T, raw []byte, change func(map[string]any)) []byte {
	t.Helper()
	var v map[string]any
	if e := json.Unmarshal(raw, &v); e != nil {
		t.Fatal(e)
	}
	change(v)
	out, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return out
}

func TestPinnedTerminalRequiresCleanEOFWithoutPromotingMetadata(t *testing.T) {
	s, b, _ := fixture(t)
	fs := frames(t)
	feed(t, s, b, fs[:len(fs)-1])
	if s.State() == "succeeded" {
		t.Fatal("text or usage treated as terminal")
	}
	feed(t, s, b, fs[len(fs)-1:])
	if s.State() == "succeeded" {
		t.Fatal("end before process exit")
	}
	o, e := s.Finish(0)
	if e != nil || o.State != "succeeded" || o.SessionID != b.NativeSessionID || o.NativeModel != b.Model || o.NativeModelCalls != 1 || o.InputTokens != 3 || o.OutputTokens != 2 {
		t.Fatal(o, e)
	}
	if o.UpstreamReportedModel != nil || o.StrictLockVerified || o.AllCallsVerified || o.BillingVerified || o.QuotaVerified || o.StoppedVerified {
		t.Fatal("native labels promoted to trusted evidence")
	}
}

func TestIncompleteOrNonzeroExitNeverSucceeds(t *testing.T) {
	for _, mode := range []string{"no_end", "exit_nonzero", "only_end", "duplicate_end", "after_end"} {
		t.Run(mode, func(t *testing.T) {
			s, b, _ := fixture(t)
			fs := frames(t)
			exit := 0
			switch mode {
			case "no_end":
				feed(t, s, b, fs[:len(fs)-1])
			case "only_end":
				if e := s.Event(b.RunID, b.Generation, fs[len(fs)-1]); e == nil {
					t.Fatal("end without declarations accepted")
				}
			default:
				feed(t, s, b, fs)
				if mode == "exit_nonzero" {
					exit = 1
				} else {
					raw := fs[len(fs)-1]
					if mode == "after_end" {
						raw = []byte(`{"type":"text","data":"late"}`)
					}
					if e := s.Event(b.RunID, b.Generation, raw); e == nil {
						t.Fatal("event after terminal accepted")
					}
				}
			}
			o, _ := s.Finish(exit)
			if o.State == "succeeded" {
				t.Fatal(mode, o)
			}
		})
	}
}

func TestBindingFrozenAndCurrentAtEveryBoundary(t *testing.T) {
	s, b, current := fixture(t)
	if e := s.Event("other", b.Generation, frames(t)[0]); !errors.Is(e, ErrIdentity) {
		t.Fatal(e)
	}
	if e := s.Event(b.RunID, b.Generation+1, frames(t)[0]); !errors.Is(e, ErrIdentity) {
		t.Fatal(e)
	}
	feed(t, s, b, frames(t))
	*current = false
	o, e := s.Finish(0)
	if !errors.Is(e, ErrIdentity) || o.State != "execution_uncertain" {
		t.Fatal(o, e)
	}
	s, b, current = fixture(t)
	*current = false
	if e := s.Event(b.RunID, b.Generation, frames(t)[0]); !errors.Is(e, ErrIdentity) {
		t.Fatal(e)
	}
	s, b, _ = fixture(t)
	b.Tools[0] = "run_terminal_command"
	feed(t, s, Binding{RunID: b.RunID, Generation: b.Generation}, frames(t))
}

func TestToolsAreExactAndReadonly(t *testing.T) {
	for _, tools := range [][]string{{"read_file", "search_tool"}, {"spawn_subagent"}, {"run_terminal_command"}, {"read_file", "read_file"}, {}} {
		t.Run(strings.Join(tools, "-"), func(t *testing.T) {
			s, b, _ := fixture(t)
			raw := edited(t, frames(t)[0], func(v map[string]any) { v["tools"] = tools })
			if e := s.Event(b.RunID, b.Generation, raw); e == nil {
				t.Fatal("unexpected tool declaration accepted")
			}
			o, _ := s.Finish(0)
			if o.State == "succeeded" {
				t.Fatal(o)
			}
		})
	}
	for _, typ := range []string{"tool_call", "tool_call_update", "plan", "auto_compact_started", "memory_flush_started", "control_request", "future_event"} {
		s, b, _ := fixture(t)
		feed(t, s, b, frames(t)[:1])
		raw, _ := json.Marshal(map[string]string{"type": typ, "message": "sensitive"})
		if e := s.Event(b.RunID, b.Generation, raw); !errors.Is(e, ErrUnsupported) || strings.Contains(e.Error(), "sensitive") {
			t.Fatal(typ, e)
		}
	}
}

func TestEndIdentityModelAndUsageAreValidated(t *testing.T) {
	for _, mode := range []string{"session", "request", "reason", "turns", "model", "extra_model", "tokens", "calls", "missing_usage", "missing_model", "total"} {
		t.Run(mode, func(t *testing.T) {
			s, b, _ := fixture(t)
			fs := frames(t)
			feed(t, s, b, fs[:len(fs)-1])
			raw := edited(t, fs[len(fs)-1], func(v map[string]any) {
				switch mode {
				case "session":
					v["sessionId"] = "22222222-2222-4222-8222-222222222222"
				case "request":
					v["requestId"] = ""
				case "reason":
					v["stopReason"] = "unexpected"
				case "turns":
					v["num_turns"] = 2
				case "missing_usage":
					delete(v, "usage")
				case "missing_model":
					delete(v, "modelUsage")
				case "total":
					v["usage"].(map[string]any)["total_tokens"] = 4
				default:
					m := v["modelUsage"].(map[string]any)
					u := m[b.Model].(map[string]any)
					switch mode {
					case "model":
						delete(m, b.Model)
						m["other"] = u
					case "extra_model":
						m["other"] = u
					case "tokens":
						u["inputTokens"] = -1
					case "calls":
						u["modelCalls"] = 0
					}
				}
			})
			if e := s.Event(b.RunID, b.Generation, raw); e == nil {
				t.Fatal("malformed end accepted", mode)
			}
			o, _ := s.Finish(0)
			if o.State == "succeeded" {
				t.Fatal(o)
			}
		})
	}
}

func TestCancellationAndNativeErrorsDoNotBecomeSuccess(t *testing.T) {
	for _, mode := range []string{"before_end", "after_end", "error", "max_turns"} {
		t.Run(mode, func(t *testing.T) {
			s, b, _ := fixture(t)
			fs := frames(t)
			switch mode {
			case "before_end":
				feed(t, s, b, fs[:1])
				if e := s.Cancel(); e != nil {
					t.Fatal(e)
				}
				feed(t, s, b, fs[1:])
			case "after_end":
				feed(t, s, b, fs)
				if e := s.Cancel(); e != nil {
					t.Fatal(e)
				}
			default:
				feed(t, s, b, fs[:1])
				typ := "error"
				if mode == "max_turns" {
					typ = "max_turns_reached"
				}
				raw, _ := json.Marshal(map[string]string{"type": typ, "message": "private/provider/key"})
				if e := s.Event(b.RunID, b.Generation, raw); e != nil {
					t.Fatal(e)
				}
			}
			o, e := s.Finish(0)
			if e != nil || o.State == "succeeded" || strings.Contains(o.NativeReason, "private") {
				t.Fatal(o, e)
			}
		})
	}
}

func TestNativeJSONIsBoundedAndUnambiguous(t *testing.T) {
	for _, raw := range [][]byte{[]byte(`{"type":"text","Type":"error","data":"x"}`), []byte(`{"type":"text","data":"x","extra":{"a":1,"A":2}}`), []byte(`{"type":"text","data":"\u0000"}`), []byte(`{"type":"text","data":"x"} {}`), []byte(`null`), []byte("{\"type\":\"text\",\"data\":\"\xff\"}"), []byte(`{"type":"text","data":"` + strings.Repeat("x", 1<<20) + `"}`), []byte(`{"type":"text","data":"x","deep":` + strings.Repeat("[", 34) + `0` + strings.Repeat("]", 34) + `}`)} {
		s, b, _ := fixture(t)
		if e := s.Event(b.RunID, b.Generation, raw); !errors.Is(e, ErrProtocol) {
			t.Fatal(e)
		}
	}
	for _, mode := range []string{"frames", "bytes"} {
		t.Run(mode, func(t *testing.T) {
			s, b, _ := fixture(t)
			feed(t, s, b, frames(t)[:1])
			raw := []byte(`{"type":"text","data":"x"}`)
			n := 4096
			if mode == "bytes" {
				raw = []byte(`{"type":"text","data":"` + strings.Repeat("x", 1<<19) + `"}`)
				n = 17
			}
			failed := false
			for i := 0; i < n; i++ {
				if s.Event(b.RunID, b.Generation, raw) != nil {
					failed = true
					break
				}
			}
			if !failed {
				t.Fatal("stream budget not enforced")
			}
		})
	}
}

func TestInvalidBindingsAndResumeAreUnsupported(t *testing.T) {
	_, base, _ := fixture(t)
	for _, mode := range []string{"version", "hash", "session", "cwd", "generation", "role", "model", "turns", "tool", "duplicate", "nil_current"} {
		t.Run(mode, func(t *testing.T) {
			b := base
			b.Tools = append([]string(nil), base.Tools...)
			current := func(Binding) bool { return true }
			switch mode {
			case "version":
				b.RuntimeVersion = "latest"
			case "hash":
				b.ExecutableSHA256 = ""
			case "session":
				b.NativeSessionID = "title"
			case "cwd":
				b.Cwd = "../project"
			case "generation":
				b.Generation = 0
			case "role":
				b.Role = "unknown"
			case "model":
				b.Model = "x\x00"
			case "turns":
				b.MaxTurns = 0
			case "tool":
				b.Tools = []string{"search_replace"}
			case "duplicate":
				b.Tools = []string{"read_file", "read_file"}
			case "nil_current":
				current = nil
			}
			if _, e := New(b, current); e == nil {
				t.Fatal(mode)
			}
		})
	}
	s, b, _ := fixture(t)
	if e := s.Resume(b); !errors.Is(e, ErrUnsupported) {
		t.Fatal(e)
	}
}

func TestActualNativeDefaultsAndImplicitToolsAreRefused(t *testing.T) {
	for _, name := range []string{"baseline", "read"} {
		s, b, _ := fixture(t)
		raw, e := os.ReadFile("testdata/native-1.0.48/" + name + "-stream.jsonl")
		if e != nil {
			t.Fatal(e)
		}
		first := bytes.Split(bytes.TrimSpace(raw), []byte("\n"))[0]
		if e = s.Event(b.RunID, b.Generation, first); !errors.Is(e, ErrUnverified) {
			t.Fatal(name, e)
		}
	}
	s, b, _ := fixture(t)
	raw, e := os.ReadFile("testdata/native-1.0.48/invalid-model-stream.jsonl")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Event(b.RunID, b.Generation, bytes.TrimSpace(raw)); e != nil {
		t.Fatal(e)
	}
	o, e := s.Finish(1)
	if e != nil || o.State != "failed" || o.NativeReason != "error" {
		t.Fatal(o, e)
	}
}

func TestNativeEndUsageCannotAccountForAuxiliaryHTTPCalls(t *testing.T) {
	raw, e := os.ReadFile("testdata/native-1.0.48/http-observations.json")
	if e != nil {
		t.Fatal(e)
	}
	var report struct {
		HTTP      []struct{ Model, Purpose string }
		RealCalls int `json:"real_model_calls"`
	}
	if e = json.Unmarshal(raw, &report); e != nil {
		t.Fatal(e)
	}
	s, b, _ := fixture(t)
	feed(t, s, b, frames(t))
	o, e := s.Finish(0)
	if e != nil || o.State != "succeeded" || report.RealCalls != 0 || len(report.HTTP) != 2 || report.HTTP[0].Purpose != "title" || report.HTTP[1].Purpose != "agent" || report.HTTP[0].Model != b.Model || o.NativeModelCalls != 1 || o.AllCallsVerified {
		t.Fatal(o, e, report)
	}
}

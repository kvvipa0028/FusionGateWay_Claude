package grok

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestActualNativeReadToolsCannotBecomeObservedSuccess(t *testing.T) {
	for _, mode := range []string{"read", "outside", "private-config"} {
		t.Run(mode, func(t *testing.T) {
			_, b, _ := fixture(t)
			b.NativeSessionID = "22222222-2222-4222-8222-222222222222"
			b.MaxTurns = 3
			s, e := New(b, func(Binding) bool { return true })
			if e != nil {
				t.Fatal(e)
			}
			raw, e := os.ReadFile("testdata/native-1.0.48/tools-" + mode + "-stream.jsonl")
			if e != nil {
				t.Fatal(e)
			}
			rejected := false
			for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
				var frame struct{ Type string }
				if e := json.Unmarshal(line, &frame); e != nil {
					t.Fatal(e)
				}
				e := s.Event(b.RunID, b.Generation, line)
				if frame.Type == "tool_call" {
					if !errors.Is(e, ErrUnsupported) || strings.Contains(e.Error(), "FUSION_") || s.State() != "execution_uncertain" {
						t.Fatal("native tool accepted or leaked", e, s.State())
					}
					rejected = true
					break
				}
				if e != nil {
					t.Fatal("unexpected pre-tool mismatch", frame.Type, e)
				}
			}
			out, e := s.Finish(0)
			if !rejected || out.State != "execution_uncertain" || out.StrictLockVerified || out.AllCallsVerified || out.StoppedVerified || out.UpstreamReportedModel != nil {
				t.Fatal("tool/Native exit0 promoted to success", out, e)
			}
		})
	}
}

func TestActualNativeToolContinuationIsRefusedBeforePermit(t *testing.T) {
	for _, mode := range []string{"read", "outside", "private-config"} {
		t.Run(mode, func(t *testing.T) {
			raw, e := os.ReadFile("testdata/native-1.0.48/tools-" + mode + "-observations.json")
			if e != nil {
				t.Fatal(e)
			}
			var observed struct {
				HTTP []struct {
					Purpose            string
					ToolMessages       []map[string]any   `json:"tool_messages"`
					AssistantToolCalls [][]map[string]any `json:"assistant_tool_calls"`
				}
			}
			if e := json.Unmarshal(raw, &observed); e != nil {
				t.Fatal(e)
			}
			var messages []map[string]any
			for _, h := range observed.HTTP {
				if h.Purpose == "agent" && len(h.ToolMessages) > 0 {
					if len(h.AssistantToolCalls) != 1 || len(h.ToolMessages) != 1 {
						t.Fatal("actual continuation changed")
					}
					messages = append(messages, map[string]any{"role": "assistant", "content": nil, "tool_calls": h.AssistantToolCalls[0]})
					messages = append(messages, h.ToolMessages...)
				}
			}
			if len(messages) != 2 {
				t.Fatal("actual continuation absent")
			}
			body, e := json.Marshal(map[string]any{"model": "fixture-model", "stream": true, "messages": messages})
			if e != nil {
				t.Fatal(e)
			}
			f := newGateFixture(t)
			g := newGate(t, f)
			w := send(t, g, f, string(body), "/v1/chat/completions")
			if w.Code != 400 || f.calls.Load() != 0 || f.permits.Load() != 0 || !g.Audit().Uncertain || strings.Contains(w.Body.String(), "FUSION_") {
				t.Fatal("unimplemented tool/credential continuation accepted", w.Code, g.Audit())
			}
		})
	}
}

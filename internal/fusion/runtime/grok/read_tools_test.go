package grok

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

func readFixture(t *testing.T) (*gateFixture, *ReadTools, string) {
	t.Helper()
	f := newGateFixture(t)
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	f.config.Binding.Observer.Cwd = root
	f.config.Binding.Observer.MaxTurns = 3
	r, e := NewReadTools(f.config.Binding.Observer, func(Binding) bool { return f.current.Load() })
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { r.Close() })
	f.config.ReadTools = r
	path := filepath.Join(root, "fixture.txt")
	if e = os.WriteFile(path, []byte("FUSION_OWNED_READ_MARKER\n"), 0600); e != nil {
		t.Fatal(e)
	}
	return f, r, path
}
func toolSSE(t *testing.T, path, id string) string {
	t.Helper()
	args, _ := json.Marshal(map[string]string{"target_file": path})
	call, _ := json.Marshal(map[string]any{"index": 0, "id": id, "type": "function", "function": map[string]string{"name": "read_file", "arguments": string(args)}})
	s := strings.Replace(gateSSE, `"content":"fixture"`, `"content":null,"tool_calls":[`+string(call)+`]`, 1)
	return strings.Replace(s, `"finish_reason":"stop"`, `"finish_reason":"tool_calls"`, 1)
}
func readForwarder(f *gateFixture, sse string) {
	f.config.Forwarder = fakeForwarder(func(context.Context, stageplan.ExecutionTarget, []byte) (ForwardResponse, error) {
		f.calls.Add(1)
		return ForwardResponse{StatusCode: 200, ContentType: "text/event-stream", Body: io.NopCloser(strings.NewReader(sse))}, nil
	})
}
func continuation(t *testing.T, path, id, content string) string {
	t.Helper()
	args, _ := json.Marshal(map[string]string{"target_file": path})
	raw, _ := json.Marshal(map[string]any{"model": "fixture-model", "stream": true, "messages": []any{
		map[string]any{"role": "user", "content": "fixture"},
		map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": id, "type": "function", "function": map[string]string{"name": "read_file", "arguments": string(args)}}}},
		map[string]any{"role": "tool", "tool_call_id": id, "content": content},
	}, "tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "read_file", "parameters": map[string]string{"type": "object"}}}}})
	return string(raw)
}
func readFrames(t *testing.T, path string) [][]byte {
	t.Helper()
	raw, e := os.ReadFile("testdata/native-1.0.48/tools-read-stream.jsonl")
	if e != nil {
		t.Fatal(e)
	}
	raw = bytes.ReplaceAll(raw, []byte("<fixture-root>/workspace/fixture.txt"), []byte(path))
	raw = bytes.ReplaceAll(raw, []byte("22222222-2222-4222-8222-222222222222"), []byte("11111111-1111-4111-8111-111111111111"))
	return bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
}
func TestReadToolsCorrelatesGateTraceAndContinuation(t *testing.T) {
	f, r, path := readFixture(t)
	reply := toolSSE(t, path, "call_fixture_read")
	f.config.Forwarder = fakeForwarder(func(context.Context, stageplan.ExecutionTarget, []byte) (ForwardResponse, error) {
		value := gateSSE
		if f.calls.Add(1) == 1 {
			value = reply
		}
		return ForwardResponse{StatusCode: 200, ContentType: "text/event-stream", Body: io.NopCloser(strings.NewReader(value))}, nil
	})
	g := newGate(t, f)
	s, e := NewReadSession(f.config.Binding.Observer, func(Binding) bool { return f.current.Load() }, r)
	if e != nil {
		t.Fatal(e)
	}
	if w := send(t, g, f, gateBody, "/v1/chat/completions"); w.Code != 200 {
		t.Fatal(w.Code, g.Audit())
	}
	if healthy(g, f) {
		t.Fatal("pending read treated as healthy")
	}
	fs := readFrames(t, path)
	for _, line := range fs[:7] {
		if e = s.Event(f.config.Claims.RunID, f.config.Claims.Generation, line); e != nil {
			t.Fatal(e)
		}
	}
	if w := send(t, g, f, continuation(t, path, "call_fixture_read", "1→FUSION_OWNED_READ_MARKER\n"), "/v1/chat/completions"); w.Code != 200 {
		t.Fatal(w.Code, g.Audit())
	}
	for _, line := range fs[7:] {
		if e = s.Event(f.config.Claims.RunID, f.config.Claims.Generation, line); e != nil {
			t.Fatal(e)
		}
	}
	out, e := s.Finish(0)
	if e != nil || out.State != "succeeded" || !healthy(g, f) || out.AllCallsVerified || out.StoppedVerified || f.permits.Load() != 2 {
		t.Fatal(out, e, g.Audit())
	}
}
func TestReadToolsRejectsPathsBeforeNativeDelivery(t *testing.T) {
	for _, mode := range []string{"outside", "parent", "symlink", "dirlink", "hardlink", "directory", "missing", "unclean", "uri", "null", "binary", "oversize", "manylines", "private_marker"} {
		t.Run(mode, func(t *testing.T) {
			f, _, path := readFixture(t)
			outside, e := filepath.EvalSymlinks(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			denied := filepath.Join(outside, "secret.txt")
			os.WriteFile(denied, []byte("private owned fixture"), 0600)
			switch mode {
			case "outside":
				path = denied
			case "parent":
				path = "../secret.txt"
			case "symlink":
				os.Remove(path)
				os.Symlink(denied, path)
			case "dirlink":
				os.Symlink(outside, filepath.Join(f.config.Binding.Observer.Cwd, "alias"))
				path = filepath.Join(f.config.Binding.Observer.Cwd, "alias/secret.txt")
			case "hardlink":
				os.Remove(path)
				if e = os.Link(denied, path); e != nil {
					t.Fatal(e)
				}
			case "directory":
				path = f.config.Binding.Observer.Cwd
			case "missing":
				path += ".missing"
			case "unclean":
				path = f.config.Binding.Observer.Cwd + "/../" + filepath.Base(f.config.Binding.Observer.Cwd) + "/fixture.txt"
			case "uri":
				path = "file://" + path
			case "null":
				path += "\x00"
			case "binary":
				os.WriteFile(path, []byte{0xff, 0}, 0600)
			case "oversize":
				os.WriteFile(path, bytes.Repeat([]byte("a"), 65537), 0600)
			case "manylines":
				os.WriteFile(path, bytes.Repeat([]byte("x\n"), 1001), 0600)
			case "private_marker":
				os.WriteFile(path, []byte(f.token), 0600)
			}
			readForwarder(f, toolSSE(t, path, "call_fixture_read"))
			g := newGate(t, f)
			w := send(t, g, f, gateBody, "/v1/chat/completions")
			if w.Code != 502 || !g.Audit().Uncertain || strings.Contains(w.Body.String(), "call_fixture_read") || healthy(g, f) {
				t.Fatal(mode, w.Code, g.Audit())
			}
		})
	}
}
func TestReadToolsRechecksFileBeforeContinuation(t *testing.T) {
	for _, mode := range []string{"content", "replace", "symlink", "current", "closed", "bad_result", "bad_id", "bad_args", "orphan", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			f, r, path := readFixture(t)
			readForwarder(f, toolSSE(t, path, "call_fixture_read"))
			g := newGate(t, f)
			if w := send(t, g, f, gateBody, "/v1/chat/completions"); w.Code != 200 {
				t.Fatal(w.Code)
			}
			body := continuation(t, path, "call_fixture_read", "1→FUSION_OWNED_READ_MARKER\n")
			switch mode {
			case "content":
				os.WriteFile(path, []byte("changed"), 0600)
			case "replace":
				os.Remove(path)
				os.WriteFile(path, []byte("FUSION_OWNED_READ_MARKER\n"), 0600)
			case "symlink":
				os.Remove(path)
				os.Symlink("/dev/null", path)
			case "current":
				f.current.Store(false)
			case "closed":
				r.Close()
			case "bad_result":
				body = strings.Replace(body, "FUSION_OWNED_READ_MARKER", "changed", 1)
			case "bad_id":
				body = strings.ReplaceAll(body, "call_fixture_read", "call_other")
			case "bad_args":
				body = strings.Replace(body, "fixture.txt", "other.txt", 1)
			case "orphan":
				body = strings.Replace(body, `"tool_calls"`, `"invalid_calls"`, 1)
			case "duplicate":
				body = strings.Replace(body, `"role":"tool"`, `"role":"tool","ROLE":"tool"`, 1)
			}
			w := send(t, g, f, body, "/v1/chat/completions")
			if w.Code < 400 || f.calls.Load() != 1 || f.permits.Load() != 1 || healthy(g, f) {
				t.Fatal(mode, w.Code, g.Audit())
			}
		})
	}
}
func TestReadToolsRequiresExactTrustedBinding(t *testing.T) {
	f, r, _ := readFixture(t)
	f.config.Binding.Observer.NativeSessionID = "22222222-2222-4222-8222-222222222222"
	if _, e := NewCallGate(f.config); e == nil {
		t.Fatal("wrong Native session adopted read scope")
	}
	if _, e := NewReadSession(f.config.Binding.Observer, func(Binding) bool { return true }, r); e == nil {
		t.Fatal("wrong Native session adopted trace")
	}
	r.Close()
	if _, e := NewReadTools(Binding{}, func(Binding) bool { return true }); e == nil {
		t.Fatal("invalid binding accepted")
	}
}

func TestReadToolsOrphanEmptyIDIsRefused(t *testing.T) {
	f, _, _ := readFixture(t)
	g := newGate(t, f)
	body := `{"model":"fixture-model","stream":true,"messages":[{"role":"tool","tool_call_id":"","content":""}]}`
	w := send(t, g, f, body, "/v1/chat/completions")
	if w.Code != 400 || f.permits.Load() != 0 || f.calls.Load() != 0 {
		t.Fatal(w.Code, g.Audit())
	}
}

func TestReadToolsRejectsUnmatchedNativeTrace(t *testing.T) {
	for _, mode := range []string{"orphan", "replay", "input", "kind", "location", "failed", "output", "path", "concise", "extra", "current"} {
		t.Run(mode, func(t *testing.T) {
			f, r, path := readFixture(t)
			readForwarder(f, toolSSE(t, path, "call_fixture_read"))
			g := newGate(t, f)
			s, e := NewReadSession(f.config.Binding.Observer, func(Binding) bool { return f.current.Load() }, r)
			if e != nil {
				t.Fatal(e)
			}
			if w := send(t, g, f, gateBody, "/v1/chat/completions"); w.Code != 200 {
				t.Fatal(w.Code)
			}
			fs := readFrames(t, path)
			badIndex := 6
			switch mode {
			case "orphan":
				badIndex = 4
				fs[4] = bytes.ReplaceAll(fs[4], []byte("call_fixture_read"), []byte("call_other"))
			case "replay":
				badIndex = 5
				fs[5] = fs[4]
			case "input":
				badIndex = 4
				fs[4] = bytes.ReplaceAll(fs[4], []byte("fixture.txt"), []byte("other.txt"))
			case "kind":
				badIndex = 4
				fs[4] = bytes.ReplaceAll(fs[4], []byte(`"kind":"read"`), []byte(`"kind":"execute"`))
			case "location":
				badIndex = 5
				fs[5] = bytes.ReplaceAll(fs[5], []byte("fixture.txt"), []byte("other.txt"))
			case "failed":
				fs[6] = bytes.ReplaceAll(fs[6], []byte(`"status":"completed"`), []byte(`"status":"failed"`))
			case "output":
				fs[6] = bytes.ReplaceAll(fs[6], []byte("FUSION_OWNED_READ_MARKER"), []byte("private unexpected output"))
			case "path":
				fs[6] = bytes.ReplaceAll(fs[6], []byte("fixture.txt"), []byte("other.txt"))
			case "concise":
				fs[6] = edited(t, fs[6], func(v map[string]any) {
					v["rawOutput"].(map[string]any)["FileContent"].(map[string]any)["content_concise"] = "unknown"
				})
			case "extra":
				fs[6] = edited(t, fs[6], func(v map[string]any) {
					v["rawOutput"].(map[string]any)["FileContent"].(map[string]any)["backend_override"] = "unknown"
				})
			case "current":
				badIndex = 4
			}
			for i, line := range fs[:badIndex+1] {
				if mode == "current" && i == badIndex {
					f.current.Store(false)
				}
				e = s.Event(f.config.Claims.RunID, f.config.Claims.Generation, line)
				if i < badIndex && e != nil {
					t.Fatal(i, e)
				}
				if i == badIndex && (e == nil || s.State() != "execution_uncertain") {
					t.Fatal(mode, e, s.State())
				}
			}
			out, _ := s.Finish(0)
			if out.State == "succeeded" || healthy(g, f) {
				t.Fatal("bad trace succeeded", out.State)
			}
		})
	}
}
func TestReadToolsNativeModelMetadataCannotOverrideFrozenModel(t *testing.T) {
	for _, model := range []string{"fixture-model", "other"} {
		t.Run(model, func(t *testing.T) {
			f, _, path := readFixture(t)
			reply := toolSSE(t, path, "call_fixture_read")
			f.config.Forwarder = fakeForwarder(func(context.Context, stageplan.ExecutionTarget, []byte) (ForwardResponse, error) {
				v := gateSSE
				if f.calls.Add(1) == 1 {
					v = reply
				}
				return ForwardResponse{StatusCode: 200, ContentType: "text/event-stream", Body: io.NopCloser(strings.NewReader(v))}, nil
			})
			g := newGate(t, f)
			if w := send(t, g, f, gateBody, "/v1/chat/completions"); w.Code != 200 {
				t.Fatal(w.Code)
			}
			body := strings.Replace(continuation(t, path, "call_fixture_read", "1→FUSION_OWNED_READ_MARKER\n"), `"role":"assistant"`, `"role":"assistant","model_id":"`+model+`"`, 1)
			w := send(t, g, f, body, "/v1/chat/completions")
			if model == "fixture-model" {
				if w.Code != 200 || f.permits.Load() != 2 {
					t.Fatal(w.Code, g.Audit())
				}
			} else if w.Code != 400 || f.permits.Load() != 1 {
				t.Fatal(w.Code, g.Audit())
			}
		})
	}
}
func TestReadToolsDecodedGrantCannotCrossHTTPBoundary(t *testing.T) {
	for _, mode := range []string{"request", "response"} {
		t.Run(mode, func(t *testing.T) {
			f, _, _ := readFixture(t)
			encoded := ""
			for _, ch := range f.token {
				encoded += fmt.Sprintf(`\u%04x`, ch)
			}
			body := gateBody
			if mode == "request" {
				body = strings.Replace(body, `"content":"fixture"`, `"content":"`+encoded+`"`, 1)
			} else {
				readForwarder(f, strings.Replace(gateSSE, `"content":"fixture"`, `"content":"`+encoded+`"`, 1))
			}
			g := newGate(t, f)
			w := send(t, g, f, body, "/v1/chat/completions")
			if w.Code < 400 || strings.Contains(w.Body.String(), encoded) || healthy(g, f) {
				t.Fatal("decoded grant escaped", w.Code, g.Audit())
			}
			if mode == "request" && f.permits.Load() != 0 {
				t.Fatal("private request spent budget")
			}
		})
	}
}

func TestReadToolsOnlyCompleteFunctionCallCanExecute(t *testing.T) {
	for _, mode := range []string{"unknown_tool", "unknown_arg", "missing_type", "multiple", "incomplete", "wrong_finish", "fragmented"} {
		t.Run(mode, func(t *testing.T) {
			f, _, path := readFixture(t)
			reply := toolSSE(t, path, "call_fixture_read")
			switch mode {
			case "unknown_tool":
				reply = strings.Replace(reply, "read_file", "search_replace", 1)
			case "unknown_arg":
				args, _ := json.Marshal(map[string]any{"target_file": path, "endpoint": "https://other.invalid"})
				reply = strings.Replace(reply, `"arguments":`+strconv.Quote(`{"target_file":"`+path+`"}`), `"arguments":`+strconv.Quote(string(args)), 1)
			case "missing_type":
				reply = strings.Replace(reply, `,"type":"function"`, "", 1)
			case "multiple":
				reply = strings.Replace(reply, `"tool_calls":[`, `"tool_calls":[{"index":1,"id":"call_other","type":"function","function":{"name":"read_file","arguments":"{}"}},`, 1)
			case "incomplete":
				reply = strings.Replace(reply, "data: [DONE]", "", 1)
			case "wrong_finish":
				reply = strings.Replace(reply, `"finish_reason":"tool_calls"`, `"finish_reason":"stop"`, 1)
			case "fragmented":
				args, _ := json.Marshal(map[string]string{"target_file": path})
				part := len(args) / 2
				delta1 := map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "call_fixture_read", "type": "function", "function": map[string]string{"name": "read_file", "arguments": string(args[:part])}}}}
				delta2 := map[string]any{"tool_calls": []any{map[string]any{"index": 0, "function": map[string]string{"arguments": string(args[part:])}}}}
				reply = ""
				for _, delta := range []any{delta1, delta2} {
					b, _ := json.Marshal(map[string]any{"id": "fixture-response", "object": "chat.completion.chunk", "created": 1780000000, "model": "fixture-model", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}})
					reply += "data: " + string(b) + "\n\n"
				}
				terminal := strings.Split(gateSSE, "\n\n")[1]
				terminal = strings.Replace(terminal, `"finish_reason":"stop"`, `"finish_reason":"tool_calls"`, 1)
				reply += terminal + "\n\ndata: [DONE]\n\n"
			}
			readForwarder(f, reply)
			g := newGate(t, f)
			w := send(t, g, f, gateBody, "/v1/chat/completions")
			want := 502
			if mode == "fragmented" {
				want = 200
			}
			if w.Code != want || f.calls.Load() != 1 || f.permits.Load() != 1 || healthy(g, f) {
				t.Fatal(mode, w.Code, g.Audit())
			}
		})
	}
}
func TestReadToolsDetectsChangedProjectRoot(t *testing.T) {
	f, _, _ := readFixture(t)
	root := f.config.Binding.Observer.Cwd
	if e := os.Rename(root, root+"-old"); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.Remove(root); os.Rename(root+"-old", root) })
	if e := os.Mkdir(root, 0700); e != nil {
		t.Fatal(e)
	}
	if _, e := NewCallGate(f.config); e == nil {
		t.Fatal("replaced project root adopted")
	}
}

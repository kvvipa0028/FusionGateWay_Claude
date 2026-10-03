//go:build darwin

package grok

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/stageplan"
)

var nativeGateCLI = flag.String("fusion-native-grok", "", "explicit pinned Grok CLI for synthetic private loopback diagnostic")

func TestGrokCallGatePinnedNativeDiagnostic(t *testing.T) {
	if *nativeGateCLI == "" {
		t.Skip("explicit pinned Native diagnostic only")
	}
	for _, mode := range []string{"success", "default_title_model", "budget_second", "implicit_tools", "sdk_retry"} {
		t.Run(mode, func(t *testing.T) { nativeGateDiagnostic(t, mode) })
	}
}
func TestGrokCallGatePinnedNativeRejectsPrivateConfigTool(t *testing.T) {
	if *nativeGateCLI == "" {
		t.Skip("explicit pinned Native diagnostic only")
	}
	nativeGateDiagnostic(t, "private_config_tool")
}

func TestGrokControlledReadPinnedNative(t *testing.T) {
	if *nativeGateCLI == "" {
		t.Skip("explicit pinned Native diagnostic only")
	}
	for _, mode := range []string{"controlled_read", "controlled_private_config"} {
		t.Run(mode, func(t *testing.T) { nativeGateDiagnostic(t, mode) })
	}
}
func nativeGateDiagnostic(t *testing.T, mode string) {
	t.Helper()
	exe, e := filepath.EvalSymlinks(*nativeGateCLI)
	if e != nil {
		t.Fatal(e)
	}
	hash, e := managed.FileHash(exe)
	if e != nil || hash != NativeExecutableSHA256 {
		t.Fatal("Native pin changed")
	}
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"grok", "project", "config", "cache", "data", "tmp"} {
		if e = os.Mkdir(filepath.Join(root, name), 0700); e != nil {
			t.Fatal(e)
		}
	}
	maxCalls := 3
	if mode == "budget_second" {
		maxCalls = 1
	}
	f, s, _ := storedGateFixture(t, maxCalls)
	f.config.Binding.Observer.Cwd = filepath.Join(root, "project")
	var read *ReadTools
	controlled := mode == "controlled_read" || mode == "controlled_private_config"
	if controlled {
		f.config.Binding.Observer.MaxTurns = 3
		if e = os.WriteFile(filepath.Join(root, "project/fixture.txt"), []byte("FUSION_OWNED_READ_MARKER\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\nline11\nline12\nline13\n"), 0600); e != nil {
			t.Fatal(e)
		}
		read, e = NewReadTools(f.config.Binding.Observer, func(Binding) bool { return f.current.Load() })
		if e != nil {
			t.Fatal(e)
		}
		defer read.Close()
		f.config.ReadTools = read
	}

	var mainCalls atomic.Int64
	f.config.Forwarder = fakeForwarder(func(ctx context.Context, target stageplan.ExecutionTarget, raw []byte) (ForwardResponse, error) {
		f.calls.Add(1)
		var req struct {
			Tools []struct{ Function struct{ Name string } }
		}
		if e := decode(raw, &req); e != nil {
			t.Error("request decode failed")
		}
		for _, tool := range req.Tools {
			if tool.Function.Name == "read_file" && (mode == "private_config_tool" || controlled && mainCalls.Add(1) == 1) {
				targetFile := filepath.Join(root, "grok/config.toml")
				if mode == "controlled_read" {
					targetFile = filepath.Join(root, "project/fixture.txt")
				}
				args, _ := json.Marshal(map[string]string{"target_file": targetFile})
				delta := `"content":null,"tool_calls":[{"index":0,"id":"call_fixture_read","type":"function","function":{"name":"read_file","arguments":` + strconv.Quote(string(args)) + `}}]`
				reply := strings.Replace(gateSSE, `"content":"fixture"`, delta, 1)
				reply = strings.Replace(reply, `"finish_reason":"stop"`, `"finish_reason":"tool_calls"`, 1)
				return ForwardResponse{StatusCode: 200, ContentType: "text/event-stream", Body: io.NopCloser(strings.NewReader(reply))}, nil
			}
			if tool.Function.Name == "read_file" && mainCalls.Add(1) == 1 && mode == "sdk_retry" {
				return ForwardResponse{StatusCode: 429, ContentType: "text/plain", Body: io.NopCloser(strings.NewReader("private synthetic retry detail"))}, nil
			}
		}
		if target.Account != f.config.Binding.Target.Account || target.RequestedModel != "fixture-model" {
			t.Error("frozen route changed")
		}
		return ForwardResponse{StatusCode: 200, ContentType: "text/event-stream", Body: io.NopCloser(strings.NewReader(strings.Replace(gateSSE, `"content":"fixture"`, `"content":"Fixture ready."`, 1)))}, nil
	})
	g := newGate(t, f)
	server := httptest.NewServer(g)
	defer server.Close()
	config := fmt.Sprintf("[model.fixture]\nmodel = \"fixture-model\"\nbase_url = %s\napi_key = %s\nmax_retries = 2\nrate_limit_retry_threshold = 2\n[models]\ndefault = \"fixture\"\n", strconv.Quote(server.URL+"/v1"), strconv.Quote(f.token))
	if mode != "default_title_model" {
		config += "session_summary = \"fixture\"\n"
	}
	config += "[features]\nturn_summary = false\n[cli]\nauto_update = false\n"
	if e = os.WriteFile(filepath.Join(root, "grok/config.toml"), []byte(config), 0600); e != nil {
		t.Fatal(e)
	}
	port := strings.TrimPrefix(server.URL, "http://127.0.0.1:")
	profile := "(version 1)\n(deny default)\n(allow signal (target self))\n(allow sysctl-read)\n(allow mach-lookup)\n(deny mach-lookup (global-name \"com.apple.securityd\"))\n(allow file-read-metadata)\n(allow file-read* (literal \"/\"))\n"
	for _, p := range []string{"/System", "/usr/lib", "/usr/share", "/Library/Apple", "/private/var/db/timezone", root} {
		profile += fmt.Sprintf("(allow file-read* file-map-executable (subpath %s))\n", strconv.Quote(p))
	}
	profile += fmt.Sprintf("(allow process-exec (literal %s))\n(allow file-read* file-map-executable (literal %s))\n(allow file-write* (subpath %s))\n(allow file-read* file-write* (literal \"/dev/null\"))\n(allow file-read* (literal \"/dev/urandom\"))\n(allow network-outbound (remote tcp \"localhost:%s\"))\n", strconv.Quote(exe), strconv.Quote(exe), strconv.Quote(root), port)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	turns := "1"
	if controlled {
		turns = "3"
	}
	args := []string{"-p", profile, exe, "--single", "Reply Fixture ready.", "--model", "fixture", "--output-format", "streaming-json", "--permission-mode", "dontAsk", "--no-subagents", "--max-turns", turns, "--tools", "Read", "--disallowed-tools", "search_tool,use_tool", "--disable-web-search", "--no-auto-update", "--cwd", f.config.Binding.Observer.Cwd, "--session-id", f.config.Binding.Observer.NativeSessionID}
	if mode == "implicit_tools" {
		for i, arg := range args {
			if arg == "--disallowed-tools" {
				args = append(args[:i], args[i+2:]...)
				break
			}
		}
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/sandbox-exec", args...)
	cmd.Dir = f.config.Binding.Observer.Cwd
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + root, "GROK_HOME=" + filepath.Join(root, "grok"), "XDG_CONFIG_HOME=" + filepath.Join(root, "config"), "XDG_CACHE_HOME=" + filepath.Join(root, "cache"), "XDG_DATA_HOME=" + filepath.Join(root, "data"), "TMPDIR=" + filepath.Join(root, "tmp"), "LANG=en_US.UTF-8", "DO_NOT_TRACK=1", "RUST_LOG=error"}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatal("synthetic Native timed out", g.Audit())
	}
	if stdout.Len() > 64<<10 || stderr.Len() > 64<<10 || strings.Contains(stdout.String(), f.token) || strings.Contains(stderr.String(), f.token) {
		t.Fatal("Native diagnostic output boundary failed")
	}
	a := g.Audit()
	budget, e := s.Budget(f.config.Claims.TaskID)
	if e != nil || int64(budget.UsedCalls) != a.Forwarded {
		t.Fatal("persistent budget mismatch", budget, e, a)
	}
	if mode == "private_config_tool" || mode == "controlled_private_config" {
		if err == nil || cmd.ProcessState.ExitCode() != 1 || !a.Uncertain || healthy(g, f) || a.Forwarded != 2 || a.Completed != 1 || budget.UsedCalls != 2 {
			t.Fatal("private file instruction escaped gate", a, budget, err)
		}
		for _, line := range bytes.Split(bytes.TrimSpace(stdout.Bytes()), []byte("\n")) {
			var frame struct{ Type string }
			if json.Unmarshal(line, &frame) != nil || frame.Type == "tool_call" || frame.Type == "tool_call_update" {
				t.Fatal("Native began tool execution after gate rejection")
			}
		}
		t.Log("Pinned Native private config tool instruction rejected before tool event; two synthetic HTTP/budget charges, Native exit1; no private grant reflection")
		return
	}
	if mode == "default_title_model" || mode == "budget_second" || mode == "implicit_tools" {
		if !a.Uncertain || healthy(g, f) || f.calls.Load() > 1 {
			t.Fatal("rejected auxiliary call promoted to success", mode, a)
		}
		if mode == "budget_second" && f.permits.Load() != 2 {
			t.Fatal("second call did not require permit", a, f.permits.Load())
		}
		t.Logf("Pinned Native rejected %s; Native exit=%d, requests=%d forwarded=%d; gate unhealthy, real model calls0", mode, cmd.ProcessState.ExitCode(), a.Attempts, a.Forwarded)
		return
	}
	if err != nil {
		for _, line := range bytes.Split(bytes.TrimSpace(stdout.Bytes()), []byte("\n")) {
			var frame struct{ Type, Message string }
			if json.Unmarshal(line, &frame) == nil && frame.Type == "error" && len(frame.Message) < 1024 {
				t.Log("synthetic Native error:", strings.ReplaceAll(frame.Message, root, "<fixture-root>"))
			}
		}
		t.Fatal("synthetic Native failed", err, g.Audit(), "stdout bytes", stdout.Len(), "stderr bytes", stderr.Len())
	}
	observer, e := New(f.config.Binding.Observer, func(Binding) bool { return f.current.Load() })
	if controlled {
		observer, e = NewReadSession(f.config.Binding.Observer, func(Binding) bool { return f.current.Load() }, read)
	}
	if e != nil {
		t.Fatal(e)
	}
	for _, line := range bytes.Split(bytes.TrimSpace(stdout.Bytes()), []byte("\n")) {
		if e = observer.Event(f.config.Claims.RunID, f.config.Claims.Generation, line); e != nil {
			var typ struct{ Type string }
			json.Unmarshal(line, &typ)
			t.Fatal("Native protocol mismatch", typ.Type, e, g.Audit())
		}
	}
	out, e := observer.Finish(cmd.ProcessState.ExitCode())
	want := int64(2)
	wantNativeCalls := int64(1)
	if controlled {
		want = 3
		wantNativeCalls = 2
	}
	if mode == "sdk_retry" {
		want = 3
	}
	if e != nil || out.State != "succeeded" || out.NativeModelCalls != wantNativeCalls || a.Forwarded != want || f.permits.Load() != want || f.calls.Load() != want || !healthy(g, f) || out.AllCallsVerified || out.StoppedVerified {
		t.Fatal("Native gate mismatch", out.State, e, a, f.permits.Load())
	}
	t.Logf("Pinned Native1.0.48 %s -> %d synthetic HTTP/permits; terminal modelCalls%d; actual wait done, managed StopProof/route/billing/quota unverified", mode, want, out.NativeModelCalls)
}

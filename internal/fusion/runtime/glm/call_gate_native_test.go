//go:build darwin

package glm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

var nativeGateCLI = flag.String("fusion-native-claude", "", "explicit pinned CLI for synthetic Native loopback diagnostic")

func TestCallGatePinnedNativeLoopbackDiagnostic(t *testing.T) {
	if *nativeGateCLI == "" {
		t.Skip("explicit pinned Native diagnostic only")
	}
	t.Run("success", func(t *testing.T) { nativeGateDiagnostic(t, false) })
	t.Run("sdk_retry", func(t *testing.T) { nativeGateDiagnostic(t, true) })
}

func nativeGateDiagnostic(t *testing.T, retry bool) {
	t.Helper()
	exe, e := filepath.EvalSymlinks(*nativeGateCLI)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(exe)
	if e != nil {
		t.Fatal(e)
	}
	hash := sha256.Sum256(raw)
	if hex.EncodeToString(hash[:]) != "6eab8333fe2121553100d8f40bfada384a3e989b94f947e18ba6677a6fcb41ea" {
		t.Fatal("Native pin changed")
	}
	f := newGateFixture(t)
	f.config.Binding.NativeSessionID = "3e0c441b-4c6c-4f91-a537-6a4975e825b0"
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	worker, project := filepath.Join(root, "worker"), filepath.Join(root, "project")
	for _, p := range []string{worker, project} {
		if e = os.Mkdir(p, 0700); e != nil {
			t.Fatal(e)
		}
	}
	for _, part := range []string{"home", "config", "cache", "data", "tmp"} {
		if e = os.Mkdir(filepath.Join(worker, part), 0700); e != nil {
			t.Fatal(e)
		}
	}
	f.config.Binding.Cwd = project
	msg := map[string]any{"id": "fixture-message", "type": "message", "role": "assistant", "model": "glm-5.3", "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 8, "output_tokens": 0}}
	events := []map[string]any{
		{"type": "message_start", "message": msg},
		{"type": "content_block_start", "index": 0, "content_block": map[string]string{"type": "text", "text": ""}},
		{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "text_delta", "text": "FUSION_FIXTURE_OK"}},
		{"type": "content_block_stop", "index": 0},
		{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 8}},
		{"type": "message_stop"},
	}
	var sse strings.Builder
	for _, event := range events {
		b, e := json.Marshal(event)
		if e != nil {
			t.Fatal(e)
		}
		fmt.Fprintf(&sse, "event: %s\ndata: %s\n\n", event["type"], b)
	}
	f.config.Transport = fixtureRoundTrip(func(r *http.Request) (*http.Response, error) {
		ordinal := f.calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer fixture-upstream-key" || r.Header.Get("X-Api-Key") != "fixture-upstream-key" || r.URL.String() != Endpoint+"/v1/messages?beta=true" {
			t.Error("Native authority or endpoint escaped gate")
		}
		if retry && ordinal == 1 {
			return &http.Response{StatusCode: 429, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("fixture-upstream-error-never-echo"))}, nil
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(sse.String()))}, nil
	})
	g, e := NewCallGate(f.config)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(g)
	defer server.Close()
	port := strings.TrimPrefix(server.URL, "http://127.0.0.1:")
	profile := "(version 1)\n(deny default)\n(allow signal (target self))\n(allow file-read-metadata)\n(allow file-read* (literal \"/\"))\n(allow sysctl-read (sysctl-name-prefix \"hw.\") (sysctl-name \"kern.osrelease\") (sysctl-name \"kern.osversion\") (sysctl-name \"kern.ostype\") (sysctl-name \"kern.bootargs\"))\n"
	for _, p := range []string{"/System/Library", "/usr/lib", "/Library/Apple/System/Library"} {
		profile += fmt.Sprintf("(allow file-read* file-map-executable (subpath %s))\n", strconv.Quote(p))
	}
	for _, p := range []string{"/usr/share/icu", "/private/var/db/timezone", worker, project} {
		profile += fmt.Sprintf("(allow file-read* (subpath %s))\n", strconv.Quote(p))
	}
	profile += fmt.Sprintf("(allow process-exec (literal %s))\n(allow file-read* file-map-executable (literal %s))\n(allow file-write* (subpath %s))\n(allow file-read* file-write* (literal \"/dev/null\"))\n(allow file-read* (literal \"/dev/urandom\"))\n(allow network-outbound (remote tcp \"localhost:%s\"))\n", strconv.Quote(exe), strconv.Quote(exe), strconv.Quote(worker), port)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	args := []string{"-p", profile, exe, "--bare", "--restricted", "--strict-mcp-config", "--setting-sources", "", "--tools", "", "--disable-slash-commands", "--no-chrome", "--no-session-persistence", "--permission-mode", "dontAsk", "--model", "glm-5.3", "--effort", "high", "--session-id", f.config.Binding.NativeSessionID, "--system-prompt", "You are a tool-free synthetic connection fixture.", "-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages", "Reply FUSION_FIXTURE_OK."}
	cmd := exec.CommandContext(ctx, "/usr/bin/sandbox-exec", args...)
	cmd.Dir = project
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + filepath.Join(worker, "home"), "USERPROFILE=" + filepath.Join(worker, "home"), "XDG_CONFIG_HOME=" + filepath.Join(worker, "config"), "XDG_CACHE_HOME=" + filepath.Join(worker, "cache"), "XDG_DATA_HOME=" + filepath.Join(worker, "data"), "TMPDIR=" + filepath.Join(worker, "tmp"), "CLAUDE_CONFIG_DIR=" + filepath.Join(worker, "config/claude"), "CLAUDE_CODE_TMPDIR=" + filepath.Join(worker, "tmp"), "ANTHROPIC_API_KEY=" + f.token, "ANTHROPIC_AUTH_TOKEN=" + f.token, "ANTHROPIC_BASE_URL=" + server.URL + "/api/anthropic", "ANTHROPIC_MODEL=glm-5.3", "DISABLE_UPDATES=1", "DISABLE_TELEMETRY=1", "DISABLE_ERROR_REPORTING=1", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1", "DISABLE_COMPACT=1", "DO_NOT_TRACK=1", "API_TIMEOUT_MS=5000"}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if e = cmd.Run(); e != nil {
		t.Fatal("synthetic Native call failed", e, "stdout bytes", stdout.Len(), "stderr bytes", stderr.Len())
	}
	observer, e := New(f.config.Binding, f.config.Current)
	if e != nil {
		t.Fatal(e)
	}
	for _, line := range bytes.Split(bytes.TrimSpace(stdout.Bytes()), []byte("\n")) {
		if _, e = observer.Event(f.config.Binding.RunID, f.config.Binding.Generation, line); e != nil {
			var frame map[string]json.RawMessage
			json.Unmarshal(line, &frame)
			projection := map[string]json.RawMessage{}
			for _, key := range []string{"type", "subtype", "status", "attempt", "max_retries", "retry_delay_ms", "error_status"} {
				if v, ok := frame[key]; ok {
					projection[key] = v
				}
			}
			info, _ := json.Marshal(projection)
			t.Logf("synthetic diagnostic protocol projection: %s; requests=%d permits=%d", info, f.calls.Load(), f.permits.Load())
			t.Fatal("Native protocol mismatch", e)
		}
	}
	outcome, e := observer.Finish(cmd.ProcessState.ExitCode())
	want := int64(1)
	if retry {
		want = 2
	}
	if e != nil || outcome.State != "succeeded" || outcome.StrictLockVerified || outcome.BillingVerified || outcome.QuotaVerified || f.calls.Load() != want || f.permits.Load() != want {
		t.Fatal("Native gate diagnostic mismatch", outcome.State, e, f.calls.Load(), f.permits.Load())
	}
	t.Logf("Pinned Native 2.1.287 -> authenticated CallGate -> %d synthetic HTTP requests/permits -> full protocol success; real model calls 0, Supervisor StopProof/admission/billing/quota unverified", want)
}

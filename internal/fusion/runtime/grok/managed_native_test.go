//go:build darwin

package grok

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/stageplan"
)

// The real pinned CLI runs with the production Supervisor/Seatbelt. Every
// identity, quota and upstream reply is synthetic; this is not X admission.
func TestManagedPinnedNativeGrokChannel(t *testing.T) {
	if *nativeGateCLI == "" {
		t.Skip("explicit pinned Native fixture only")
	}
	for _, mode := range []string{"success", "read", "private_config", "outside_read", "write_unsupported", "budget_exhausted", "cancel_inflight", "sdk_retry"} {
		t.Run(mode, func(t *testing.T) { managedNativeGrok(t, mode) })
	}
}
func managedNativeGrok(t *testing.T, mode string) {
	t.Helper()
	exe, e := filepath.EvalSymlinks(*nativeGateCLI)
	if e != nil {
		t.Fatal(e)
	}
	hash, e := managed.FileHash(exe)
	if e != nil || hash != NativeExecutableSHA256 {
		t.Fatal("Native pin changed")
	}
	private := func() string {
		p, e := filepath.EvalSymlinks(t.TempDir())
		if e != nil {
			t.Fatal(e)
		}
		if os.Chmod(p, 0700) != nil {
			t.Fatal("private root failed")
		}
		return p
	}
	root, cwd, outside := private(), private(), private()
	owned := filepath.Join(cwd, "fixture.txt")
	ownedText := []byte("FUSION_OWNED_READ_MARKER\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\nline11\nline12\nline13\n")
	forbidden := filepath.Join(outside, "forbidden.txt")
	if os.WriteFile(owned, ownedText, 0600) != nil || os.WriteFile(forbidden, []byte("FUSION_FORBIDDEN_MARKER\n"), 0600) != nil {
		t.Fatal("file fixture failed")
	}
	maxCalls := 3
	if mode == "budget_exhausted" {
		maxCalls = 1
	}
	f, s, _, run, pending, scheduler := storedGrokFixture(t, maxCalls, false)
	defer pending.Cancel()
	b := clone(f.config.Binding.Observer)
	b.Cwd = cwd
	b.MaxTurns = 3
	f.config.Binding.Observer = b
	current := func(got Binding) bool {
		r, e := s.CheckActive(run.ID, run.Generation)
		return f.current.Load() && e == nil && (r.State == "starting" || r.State == "running") && r.Owner == run.Owner && r.Role == got.Role && got.RunID == b.RunID && got.Generation == b.Generation && got.Cwd == b.Cwd && got.NativeSessionID == b.NativeSessionID && got.Model == b.Model
	}
	f.config.Current = func(got GateBinding) bool {
		return current(got.Observer) && managedFixtureEqualTarget(got.Target, run.Target)
	}
	read, e := NewReadTools(b, current)
	if e != nil {
		t.Fatal(e)
	}
	defer read.Close()
	f.config.ReadTools = read
	observer, e := NewReadSession(b, current, read)
	if e != nil {
		t.Fatal(e)
	}
	var mainCalls atomic.Int64
	entered, transportDone := make(chan struct{}, 1), make(chan struct{}, 1)
	f.config.Forwarder = fakeForwarder(func(ctx context.Context, target stageplan.ExecutionTarget, raw []byte) (ForwardResponse, error) {
		f.calls.Add(1)
		if !managedFixtureEqualTarget(target, run.Target) || bytes.Contains(raw, []byte(f.token)) || bytes.Contains(raw, []byte("FUSION_FORBIDDEN_MARKER")) {
			t.Error("controller authority/content escaped")
		}
		var req struct {
			Tools []struct{ Function struct{ Name string } }
		}
		if decode(raw, &req) != nil {
			return ForwardResponse{}, ErrProtocol
		}
		main := false
		for _, tool := range req.Tools {
			if tool.Function.Name == "read_file" {
				main = true
			}
		}
		if main {
			ordinal := mainCalls.Add(1)
			if mode == "cancel_inflight" {
				entered <- struct{}{}
				<-ctx.Done()
				transportDone <- struct{}{}
				return ForwardResponse{}, ctx.Err()
			}
			if mode == "sdk_retry" && ordinal == 1 {
				return ForwardResponse{StatusCode: 429, ContentType: "text/plain", Body: io.NopCloser(strings.NewReader("synthetic retry"))}, nil
			}
			if ordinal == 1 && (mode == "read" || mode == "private_config" || mode == "outside_read" || mode == "write_unsupported") {
				targetFile := owned
				if mode == "private_config" {
					targetFile = filepath.Join(root, "config", "grok", "config.toml")
				}
				if mode == "outside_read" {
					targetFile = forbidden
				}
				name := "read_file"
				if mode == "write_unsupported" {
					name = "search_replace"
				}
				args, _ := json.Marshal(map[string]string{"target_file": targetFile})
				delta := `"content":null,"tool_calls":[{"index":0,"id":"call_fixture_read","type":"function","function":{"name":` + strconv.Quote(name) + `,"arguments":` + strconv.Quote(string(args)) + `}}]`
				reply := strings.Replace(gateSSE, `"content":"fixture"`, delta, 1)
				reply = strings.Replace(reply, `"finish_reason":"stop"`, `"finish_reason":"tool_calls"`, 1)
				return ForwardResponse{StatusCode: 200, ContentType: "text/event-stream", Body: io.NopCloser(strings.NewReader(reply))}, nil
			}
		}
		reply := strings.Replace(gateSSE, `"content":"fixture"`, `"content":"Fixture ready."`, 1)
		return ForwardResponse{StatusCode: 200, ContentType: "text/event-stream", Body: io.NopCloser(strings.NewReader(reply))}, nil
	})
	gate := newGate(t, f)
	channel, e := managed.NewGrokChannel(gate, pending, run.Target)
	if e != nil {
		t.Fatal(e)
	}
	defer channel.Close()
	var observed atomic.Bool
	var outcome Outcome
	args := []string{"--single", "Reply Fixture ready.", "--model", "fusion", "--output-format", "streaming-json", "--permission-mode", "dontAsk", "--no-subagents", "--max-turns", "3", "--tools", "Read", "--disallowed-tools", "search_tool,use_tool", "--disable-web-search", "--no-auto-update", "--cwd", cwd, "--session-id", b.NativeSessionID}
	spec := managed.Spec{Executable: exe, ExecutableHash: hash, Args: args, Root: root, Workspace: cwd, Timeout: 15 * time.Second, NativeSessionID: b.NativeSessionID, GrokChannel: channel, ValidateOutcome: func(raw []byte) bool {
		if bytes.Contains(raw, []byte(f.token)) || bytes.Contains(raw, []byte("FUSION_FORBIDDEN_MARKER")) {
			t.Error("Native output exposed private material")
			return false
		}
		for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
			if e := observer.Event(b.RunID, b.Generation, line); e != nil {
				t.Log("managed Native protocol rejected")
				return false
			}
		}
		var finishErr error
		outcome, finishErr = observer.Finish(0)
		observed.Store(true)
		if finishErr != nil || outcome.State != "succeeded" {
			return false
		}
		ctx, e := f.manager.WithStage(context.Background(), f.token, f.config.Claims)
		return e == nil && gate.Healthy(ctx)
	}}
	sup := managed.NewSupervisor(s)
	scheduler.VerifyStop = sup.VerifyStop
	h, e := sup.Start(context.Background(), run, spec)
	if e != nil {
		if h != nil {
			h.Cancel()
		}
		t.Fatal("managed Native launch refused", e)
	}
	defer h.Cancel()
	if mode == "cancel_inflight" {
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			t.Fatal("Native controlled request absent")
		}
		if channel.Close() == nil {
			t.Fatal("active process lost owned ports")
		}
		if e := h.Cancel(); e != nil {
			t.Fatal(e)
		}
		select {
		case <-transportDone:
		case <-time.After(2 * time.Second):
			t.Fatal("revocation did not cancel in-flight request")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, e := h.Wait(ctx)
	if e != nil || !result.StoppedVerified || !sup.VerifyStop(result.Proof) {
		t.Fatal("Native stop not proven", e, result.State, result.ExitCode)
	}
	audit := gate.Audit()
	budget, e := s.Budget(run.TaskID)
	if e != nil || int64(budget.UsedCalls) != audit.Forwarded || audit.Forwarded != f.calls.Load() {
		t.Fatal("persisted budget mismatch", audit, budget, e)
	}
	success := mode == "success" || mode == "read" || mode == "sdk_retry"
	if success {
		want := int64(2)
		if mode != "success" {
			want = 3
		}
		wantRetry := int64(0)
		if mode == "sdk_retry" {
			wantRetry = 1
		}
		if audit.Retryable != wantRetry {
			t.Fatal("unexpected retry accounting", audit)
		}
		if result.State != "succeeded" || !observed.Load() || audit.Forwarded != want || audit.Completed != want-audit.Retryable || audit.Uncertain || outcome.StrictLockVerified || outcome.BillingVerified || outcome.QuotaVerified || outcome.StoppedVerified {
			t.Fatal("managed Native evidence mismatch", result.State, result.ExitCode, audit, observed.Load())
		}
	} else {
		want := int64(2)
		if mode == "budget_exhausted" {
			want = 1
		}
		if result.State == "succeeded" || audit.Forwarded != want {
			t.Fatal("unsafe Native accepted", result.State, result.ExitCode, audit)
		}
		if mode == "cancel_inflight" && result.State != "cancelled" {
			t.Fatal("cancellation not persisted", result.State)
		}
	}
	if _, e := pending.Secret(); e == nil {
		t.Fatal("terminal grant retained")
	}
	if _, e := f.manager.AuthenticateStage(f.token, f.config.Claims); e == nil {
		t.Fatal("stopped run still callable")
	}
	encoded, _ := json.Marshal(result)
	journal, e := os.ReadFile(filepath.Join(root, "launch.json"))
	if e != nil || bytes.Contains(encoded, []byte(f.token)) || bytes.Contains(journal, []byte(f.token)) {
		t.Fatal("report/journal exposed grant")
	}
	if e := scheduler.Release(result.Proof); e != nil {
		t.Fatal("stop proof did not release reservation", e)
	}
	after, e := os.ReadFile(owned)
	if e != nil || !bytes.Equal(after, ownedText) {
		t.Fatal("readonly owned file changed")
	}
	after, e = os.ReadFile(forbidden)
	if e != nil || string(after) != "FUSION_FORBIDDEN_MARKER\n" {
		t.Fatal("outside file changed")
	}
	t.Logf("synthetic Native mode=%s state=%s exit=%d stop=true HTTP=%d permits=%d budget=%d model-label-calls=%d", mode, result.State, result.ExitCode, audit.Forwarded, f.permits.Load(), budget.UsedCalls, outcome.NativeModelCalls)
}

func managedFixtureEqualTarget(a, b stageplan.ExecutionTarget) bool {
	x, e := json.Marshal(a)
	y, f := json.Marshal(b)
	return e == nil && f == nil && bytes.Equal(x, y)
}

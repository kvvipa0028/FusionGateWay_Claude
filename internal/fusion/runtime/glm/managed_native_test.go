//go:build darwin

package glm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

// This test uses the real, explicitly selected Native binary and production
// Supervisor/Seatbelt/channel/store. Its upstream and all identities are fake.
func TestManagedPinnedNativeClaudeChannel(t *testing.T) {
	if *nativeGateCLI == "" {
		t.Skip("explicit pinned Native fixture only")
	}
	for _, mode := range []string{"success", "sdk_retry", "budget_exhausted", "cancel_inflight"} {
		t.Run(mode, func(t *testing.T) { managedNativeChannel(t, mode) })
	}
}

func TestManagedPinnedNativeBuiltinTools(t *testing.T) {
	if *nativeGateCLI == "" {
		t.Skip("explicit pinned Native tool fixture only")
	}
	for _, mode := range []string{"read", "edit", "create", "outside_read", "outside_create", "readonly_create", "write_unsupported"} {
		t.Run(mode, func(t *testing.T) { managedNativeChannel(t, mode) })
	}
}

func managedNativeChannel(t *testing.T, mode string) {
	t.Helper()
	exe, e := filepath.EvalSymlinks(*nativeGateCLI)
	if e != nil {
		t.Fatal(e)
	}
	hash, e := managed.FileHash(exe)
	if e != nil || hash != "6eab8333fe2121553100d8f40bfada384a3e989b94f947e18ba6677a6fcb41ea" {
		t.Fatal("Native pin changed")
	}
	private := func() string {
		p, e := filepath.EvalSymlinks(t.TempDir())
		if e != nil {
			t.Fatal(e)
		}
		if e = os.Chmod(p, 0700); e != nil {
			t.Fatal(e)
		}
		return p
	}
	s, e := store.Open(private())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	toolMode := mode == "read" || mode == "create" || mode == "edit" || mode == "outside_read" || mode == "outside_create" || mode == "readonly_create" || mode == "write_unsupported"
	role := stageplan.Design
	writable := mode == "create" || mode == "edit" || mode == "outside_create" || mode == "write_unsupported"
	if writable || mode == "readonly_create" {
		role = stageplan.Implementation
	}
	tools := []string{}
	if mode == "read" || mode == "outside_read" {
		tools = []string{"Read"}
	}
	if mode == "write_unsupported" {
		tools = []string{"Write"}
	}
	if mode == "create" || mode == "outside_create" || mode == "readonly_create" {
		tools = []string{"Edit"}
	}
	if mode == "edit" {
		tools = []string{"Read", "Edit"}
	}
	route := stageplan.Route{ID: "fixture-native-glm", Revision: 1, Model: "glm-5.3", Account: "fixture-account", Workspace: "fixture-workspace", CredentialIdentity: "fixture-credential", RuntimeVersion: CLIVersion, BillingPath: "coding_plan", BillingKnown: true, Admitted: true, Efforts: []string{"high"}, DefaultEffort: ptr("high"), Capabilities: []string{"text"}, LockEnforcement: stageplan.ControlledCalls}
	plan, e := stageplan.Compile(1, []stageplan.Role{role}, stageplan.Layer{Roles: map[stageplan.Role]stageplan.Binding{role: {Mode: stageplan.Locked, Route: &stageplan.RouteRef{ID: route.ID, Revision: 1}, Model: route.Model, Effort: &stageplan.EffortSelection{Mode: stageplan.EffortExplicit, Value: "high"}}}}, stageplan.Layer{}, stageplan.Layer{}, []stageplan.Route{route})
	if e != nil {
		t.Fatal(e)
	}
	task, e := s.Create("fixture-native-channel", store.CreateRequest{ProjectID: "fixture-project", Goal: "fixture", Plan: plan})
	if e != nil {
		t.Fatal(e)
	}
	maxCalls := 2
	if mode == "edit" {
		maxCalls = 3
	}
	if mode == "budget_exhausted" {
		maxCalls = 1
	}
	if e = s.ConfigureBudget(task.ID, store.Budget{MaxCalls: maxCalls}); e != nil {
		t.Fatal(e)
	}
	writeKey := ""
	if writable {
		writeKey = "fixture-write-workspace"
	}
	run, e := s.StartReserved(store.StartRequest{TaskID: task.ID, Role: role, PlanRevision: 1, Owner: "fixture-owner", TTL: time.Minute, Target: *plan.Bindings[role].Target}, store.ReservationRequest{PoolKey: "fixture-native-pool", WriteKey: writeKey, GlobalLimit: 2, AdmissionHash: strings.Repeat("a", 64)})
	if e != nil {
		t.Fatal(e)
	}
	c := policy.Claims{TaskID: task.ID, RunID: run.ID, Role: run.Role, Attempt: run.Attempt, PlanRevision: run.PlanRevision, Generation: run.Generation, ProjectID: task.ProjectID, Audience: policy.ModelAudience}
	m := policy.NewManager("fixture-controller-management", policy.StoreValidator(s), nil)
	pending, e := m.PrepareModel(c, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	defer pending.Cancel()
	grant, e := pending.Secret()
	if e != nil {
		t.Fatal(e)
	}
	root, cwd := private(), private()
	outside := private()
	file := filepath.Join(cwd, "fixture.txt")
	outsideFile := filepath.Join(outside, "fixture.txt")
	newFile := filepath.Join(cwd, "created.txt")
	outsideNewFile := filepath.Join(outside, "created.txt")
	if toolMode {
		if e = os.WriteFile(file, []byte("synthetic owned file content\n"), 0600); e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(outsideFile, []byte("synthetic forbidden file content\n"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	turns := int64(1)
	if toolMode {
		turns = int64(maxCalls)
	}
	sid := "3e0c441b-4c6c-4f91-a537-6a4975e825b0"
	b := Binding{RunID: run.ID, Generation: run.Generation, Role: run.Role, NativeSessionID: sid, Cwd: cwd, Endpoint: Endpoint, Region: "CN", Target: run.Target, Tools: tools, MaxTurns: turns}
	current := func(got Binding) bool {
		return got.RunID == b.RunID && got.Generation == b.Generation && got.Target.CredentialIdentity == b.Target.CredentialIdentity
	}
	var calls, permits atomic.Int64
	entered := make(chan struct{}, 1)
	config := CallGateConfig{Binding: b, Manager: m, Claims: c, APIKey: "fixture-controller-upstream-key", Current: current, Permit: func(ctx context.Context, got policy.Claims, target stageplan.ExecutionTarget, count int) error {
		permits.Add(1)
		if ctx.Err() != nil || got != c || target.ResolvedModel != "glm-5.3" || count != 1 {
			return ErrUnverified
		}
		return s.ReserveCall(got.RunID, got.Generation)
	}, Transport: fixtureRoundTrip(func(r *http.Request) (*http.Response, error) {
		ordinal := calls.Add(1)
		if r.Header.Get("X-Api-Key") != "fixture-controller-upstream-key" || r.Header.Get("Authorization") != "Bearer fixture-controller-upstream-key" || r.URL.String() != Endpoint+"/v1/messages?beta=true" {
			t.Error("controller authority drift")
		}
		if mode == "cancel_inflight" {
			entered <- struct{}{}
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		if (mode == "sdk_retry" || mode == "budget_exhausted") && ordinal == 1 {
			return &http.Response{StatusCode: 429, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("fixture-error"))}, nil
		}
		if toolMode {
			raw, e := io.ReadAll(r.Body)
			if e != nil {
				return nil, e
			}
			if bytes.Contains(raw, []byte("synthetic forbidden file content")) {
				t.Error("forbidden file leaked into tool result")
			}
			if ordinal > 1 && mode == "read" && !bytes.Contains(raw, []byte("synthetic owned file content")) {
				t.Error("actual Read content missing from tool result")
			}
			if ordinal < int64(maxCalls) {
				name, input := "Read", map[string]any{"file_path": file}
				switch mode {
				case "outside_read":
					input["file_path"] = outsideFile
				case "write_unsupported":
					name = "Write"
					input["content"] = "synthetic changed file content\n"
				case "create", "readonly_create", "outside_create":
					name = "Edit"
					input["file_path"] = newFile
					input["old_string"] = ""
					input["new_string"] = "synthetic new file content\n"
					if mode == "outside_create" {
						input["file_path"] = outsideNewFile
					}
				case "edit":
					if ordinal == 2 {
						name = "Edit"
						input["old_string"] = "synthetic owned"
						input["new_string"] = "synthetic changed"
					}
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(nativeFixtureToolSSE(t, ordinal, name, input)))}, nil
			}
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(nativeFixtureSSE(t)))}, nil
	})}
	gate, e := NewCallGate(config)
	if e != nil {
		t.Fatal(e)
	}
	channel, e := managed.NewClaudeChannel(gate, pending, run.Target)
	if e != nil {
		t.Fatal(e)
	}
	defer channel.Close()
	observer, e := New(b, current)
	if e != nil {
		t.Fatal(e)
	}
	var outcome Outcome
	var observed atomic.Bool
	var toolFailure atomic.Bool
	args := []string{"--bare", "--restricted", "--strict-mcp-config", "--setting-sources", "", "--tools", strings.Join(tools, ","), "--disable-slash-commands", "--no-chrome", "--no-session-persistence", "--permission-mode", "dontAsk", "--model", "glm-5.3", "--effort", "high", "--session-id", sid, "--system-prompt", "You are a synthetic connection and owned-file tool fixture.", "-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages", "Reply FUSION_FIXTURE_OK."}
	if len(tools) > 0 {
		args = append([]string{"--allowedTools", strings.Join(tools, ",")}, args...)
	}
	spec := managed.Spec{Executable: exe, ExecutableHash: hash, Args: args, Root: root, Workspace: cwd, Writable: writable, Timeout: 15 * time.Second, NativeSessionID: sid, ClaudeChannel: channel, ValidateOutcome: func(raw []byte) bool {
		if bytes.Contains(raw, []byte(config.APIKey)) || bytes.Contains(raw, []byte(grant)) {
			return false
		}
		for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
			if toolMode {
				var user struct {
					Type    string
					Message struct {
						Content []struct {
							Type    string
							IsError bool `json:"is_error"`
						}
					}
				}
				json.Unmarshal(line, &user)
				if user.Type == "user" {
					for _, content := range user.Message.Content {
						if content.Type == "tool_result" && content.IsError {
							toolFailure.Store(true)
						}
					}
				}
			}
			if _, e := observer.Event(b.RunID, b.Generation, line); e != nil {
				if toolMode {
					var projection struct{ Type, Subtype string }
					json.Unmarshal(line, &projection)
					t.Logf("tool fixture protocol failure: %s/%s", projection.Type, projection.Subtype)
					if projection.Type == "system" && projection.Subtype == "init" {
						var init struct{ Tools []string }
						json.Unmarshal(line, &init)
						t.Logf("synthetic init tools: %v", init.Tools)
					}
				}
				return false
			}
		}
		var e error
		outcome, e = observer.Finish(0) // Supervisor invokes this only after actual exit 0 and EOF.
		observed.Store(true)
		return e == nil && outcome.State == "succeeded"
	}}
	sup := managed.NewSupervisor(s)
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
			t.Fatal("Native request did not reach controlled transport")
		}
		if channel.Close() == nil {
			t.Fatal("active Native lost exclusive ports")
		}
		if e = h.Cancel(); e != nil {
			t.Fatal(e)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, e := h.Wait(ctx)
	if e != nil || !result.StoppedVerified || !sup.VerifyStop(result.Proof) {
		t.Fatal("actual process stop unverified", e, result.State, result.ExitCode)
	}
	want, wantPermits := int64(1), int64(1)
	if mode == "sdk_retry" {
		want, wantPermits = 2, 2
	}
	if toolMode {
		if mode == "readonly_create" && !toolFailure.Load() {
			t.Fatal("readonly failure did not carry Native tool_result.is_error")
		}
		want, wantPermits = int64(maxCalls), int64(maxCalls)
	}
	if mode == "budget_exhausted" {
		wantPermits = 2
	}
	budget, e := s.Budget(task.ID)
	if e != nil || budget.UsedCalls != int(want) || calls.Load() != want || permits.Load() != wantPermits {
		t.Fatal("native requests escaped persisted budget", budget.UsedCalls, calls.Load(), permits.Load(), e)
	}
	if mode == "success" || mode == "sdk_retry" || mode == "read" || mode == "create" || mode == "edit" {
		messages := int64(1)
		if toolMode {
			messages = turns
		}
		if result.State != "succeeded" || !observed.Load() || outcome.ObservedMessages != messages || outcome.StrictLockVerified || outcome.BillingVerified || outcome.QuotaVerified || outcome.UpstreamVerified {
			t.Fatal("Native protocol outcome mismatch", result.State, result.ExitCode)
		}
	} else if result.State == "succeeded" {
		t.Fatal("cancelled or exhausted task succeeded")
	}
	if toolMode {
		content, e := os.ReadFile(file)
		if e != nil {
			t.Fatal(e)
		}
		want := "synthetic owned file content\n"
		if mode == "edit" {
			want = "synthetic changed file content\n"
		}
		if string(content) != want {
			t.Fatal("Native tool did not preserve the approved write boundary")
		}
		content, e = os.ReadFile(outsideFile)
		if e != nil || string(content) != "synthetic forbidden file content\n" {
			t.Fatal("Native tool changed forbidden file")
		}
		content, e = os.ReadFile(newFile)
		if mode == "create" {
			if e != nil || string(content) != "synthetic new file content\n" {
				t.Fatal("Native Edit did not create approved file")
			}
		} else if !os.IsNotExist(e) {
			t.Fatal("unexpected file created in readonly/other scenario")
		}
		if _, e = os.Stat(outsideNewFile); !os.IsNotExist(e) {
			t.Fatal("Native tool created forbidden file")
		}
	}
	if _, e = pending.Secret(); e == nil {
		t.Fatal("terminal grant retained")
	}
	if _, e = m.AuthenticateStage(grant, c); e == nil {
		t.Fatal("terminal model authority retained")
	}
	if e = (&policy.Scheduler{Store: s, VerifyStop: sup.VerifyStop}).Release(result.Proof); e != nil {
		t.Fatal(e)
	}
	t.Logf("Pinned Native %s -> production Supervisor/dual-loopback CallGate -> %d fake upstream requests, %d persisted calls -> %s, exit=%d, verified reaping; real model calls=0, admission/billing/quota unverified", CLIVersion, calls.Load(), budget.UsedCalls, result.State, result.ExitCode)
}

func nativeFixtureToolSSE(t *testing.T, round int64, name string, input map[string]any) string {
	t.Helper()
	raw, e := json.Marshal(input)
	if e != nil {
		t.Fatal(e)
	}
	events := []map[string]any{
		{"type": "message_start", "message": map[string]any{"id": fmt.Sprintf("fixture-tool-message-%d", round), "type": "message", "role": "assistant", "model": "glm-5.3", "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 8, "output_tokens": 0}}},
		{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "tool_use", "id": fmt.Sprintf("fixture-tool-%d", round), "name": name, "input": map[string]any{}}},
		{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "input_json_delta", "partial_json": string(raw)}},
		{"type": "content_block_stop", "index": 0},
		{"type": "message_delta", "delta": map[string]any{"stop_reason": "tool_use", "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 8}},
		{"type": "message_stop"},
	}
	var b strings.Builder
	for _, event := range events {
		raw, e := json.Marshal(event)
		if e != nil {
			t.Fatal(e)
		}
		fmt.Fprintf(&b, "event: %s\ndata: %s\n\n", event["type"], raw)
	}
	return b.String()
}

func nativeFixtureSSE(t *testing.T) string {
	t.Helper()
	events := []map[string]any{
		{"type": "message_start", "message": map[string]any{"id": "fixture-message", "type": "message", "role": "assistant", "model": "glm-5.3", "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 8, "output_tokens": 0}}},
		{"type": "content_block_start", "index": 0, "content_block": map[string]string{"type": "text", "text": ""}},
		{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "text_delta", "text": "FUSION_FIXTURE_OK"}},
		{"type": "content_block_stop", "index": 0},
		{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 8}},
		{"type": "message_stop"},
	}
	var b strings.Builder
	for _, event := range events {
		raw, e := json.Marshal(event)
		if e != nil {
			t.Fatal(e)
		}
		fmt.Fprintf(&b, "event: %s\ndata: %s\n\n", event["type"], raw)
	}
	return b.String()
}

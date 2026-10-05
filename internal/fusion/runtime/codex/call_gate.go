package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/stageplan"
)

// NativeForwarder is trusted controller wiring for one independently admitted
// ChatGPT subscription route. Send performs exactly one send, obeys context,
// verifies credential identity/billing/endpoint, and never retries or redirects.
// Incoming headers are not forwarded. This package supplies no API key fallback.
type NativeForwarder interface {
	Send(context.Context, stageplan.ExecutionTarget, []byte) (ForwardResponse, error)
}
type ForwardResponse struct {
	StatusCode                   int
	ContentType, ContentEncoding string
	// ReportedModel must come from the actual upstream response, not its request.
	ReportedModel  string
	Body           io.ReadCloser `json:"-"`
	PrivateMarkers [][]byte      `json:"-"`
}

func (ForwardResponse) String() string   { return "Codex native response (private payload omitted)" }
func (ForwardResponse) GoString() string { return "Codex native response (private payload omitted)" }

// Config is trusted server wiring, never a Worker DTO or route admission proof.
type CallGateConfig struct {
	Binding Binding
	Manager *policy.Manager
	Claims  policy.Claims
	Current func(Binding) bool
	// Production uses Scheduler.Permit to atomically spend the persistent budget
	// after rechecking route admission, permissions, quota and reservation.
	Permit    func(context.Context, policy.Claims, stageplan.ExecutionTarget, int) error
	Forwarder NativeForwarder
}
type CallAudit struct {
	Attempts, Permits, Forwarded, Completed, Retryable int64
	Uncertain                                          bool
}
type CallGate struct {
	binding                                                    Binding
	manager                                                    *policy.Manager
	claims                                                     policy.Claims
	current                                                    func(Binding) bool
	permit                                                     func(context.Context, policy.Claims, stageplan.ExecutionTarget, int) error
	forwarder                                                  NativeForwarder
	handler                                                    http.Handler
	queue                                                      chan struct{}
	attempts, permits, forwarded, completed, retryable, active atomic.Int64
	uncertain                                                  atomic.Bool
}
type gateMarkerKey struct{}

var gateID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

func cloneGate(b Binding) Binding {
	t := &b.Target
	if t.Effort.Value != nil {
		v := *t.Effort.Value
		t.Effort.Value = &v
	}
	if t.PluginVersion != nil {
		v := *t.PluginVersion
		t.PluginVersion = &v
	}
	if t.UpstreamReportedModel != nil {
		v := *t.UpstreamReportedModel
		t.UpstreamReportedModel = &v
	}
	t.Capabilities = append([]string(nil), t.Capabilities...)
	return b
}
func NewCallGate(c CallGateConfig) (*CallGate, error) {
	b, t, s, id := c.Binding, c.Binding.Target, c.Binding.Scope, c.Binding.Identity
	role := false
	for _, r := range stageplan.AllRoles() {
		role = role || r == s.Role
	}
	if c.Manager == nil || c.Current == nil || c.Permit == nil || c.Forwarder == nil || !role || s.RunID == "" || s.Generation < 1 || id.Generation < 1 || id.Account == "" || id.Workspace == "" || id.CredentialIdentity == "" || t.Account != id.Account || t.Workspace != id.Workspace || t.CredentialIdentity != id.CredentialIdentity || t.Route.ID == "" || t.Route.Revision < 1 || t.RuntimeVersion != CLIVersion || !gateID.MatchString(t.RequestedModel) || t.ResolvedModel != t.RequestedModel || t.BillingPath != "subscription" || t.PluginVersion != nil || t.LockEnforcement != stageplan.ControlledCalls || t.Effort.Value == nil || !gateID.MatchString(*t.Effort.Value) || (t.Effort.RequestedMode != stageplan.EffortExplicit && t.Effort.RequestedMode != stageplan.EffortDefault) || !filepath.IsAbs(b.Cwd) || filepath.Clean(b.Cwd) != b.Cwd || !filepath.IsAbs(b.CodexHome) || filepath.Clean(b.CodexHome) != b.CodexHome || c.Claims.Audience != policy.ModelAudience || c.Claims.RunID != s.RunID || c.Claims.Generation != s.Generation || c.Claims.Role != s.Role || c.Claims.TaskID == "" || c.Claims.ProjectID == "" || c.Claims.Attempt < 1 || c.Claims.PlanRevision < 1 {
		return nil, ErrUnverified
	}
	g := &CallGate{binding: cloneGate(b), manager: c.Manager, claims: c.Claims, current: c.Current, permit: c.Permit, forwarder: c.Forwarder, queue: make(chan struct{}, 4)}
	g.handler = c.Manager.Stage(c.Claims, http.HandlerFunc(g.call))
	return g, nil
}
func (g *CallGate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	marker := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if len(marker) <= 256 {
		r = r.WithContext(context.WithValue(r.Context(), gateMarkerKey{}, marker))
	}
	g.handler.ServeHTTP(w, r)
}
func (g *CallGate) Audit() CallAudit {
	return CallAudit{g.attempts.Load(), g.permits.Load(), g.forwarded.Load(), g.completed.Load(), g.retryable.Load(), g.uncertain.Load()}
}
func (g *CallGate) live(ctx context.Context) bool {
	return ctx.Err() == nil && !g.uncertain.Load() && g.manager.ModelCurrent(ctx, g.claims) && g.current(cloneGate(g.binding)) && g.manager.ModelCurrent(ctx, g.claims) && !g.uncertain.Load()
}

// Healthy covers this HTTP gate only, never Native all-calls, login or StopProof.
func (g *CallGate) Healthy(ctx context.Context) bool {
	return g.live(ctx) && g.active.Load() == 0 && g.completed.Load() > 0 && g.forwarded.Load() == g.completed.Load()+g.retryable.Load()
}
func gateError(w http.ResponseWriter, status int) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "application/json")
	code, kind := "fusion_execution_rejected", "invalid_request_error"
	if status == 429 {
		code, kind = "rate_limit_exceeded", "rate_limit_error"
	}
	if status >= 500 {
		kind = "server_error"
	}
	body, _ := json.Marshal(map[string]any{"error": map[string]any{"message": http.StatusText(status), "code": code, "type": kind, "param": nil}})
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
func (g *CallGate) reject(w http.ResponseWriter, status int) {
	g.uncertain.Store(true)
	gateError(w, status)
}
func (g *CallGate) acquire(ctx context.Context) (func(), error) {
	for {
		if !g.live(ctx) {
			return nil, ErrIdentity
		}
		release, e := g.manager.BeginModelCall(ctx, g.claims)
		if e == nil {
			return release, nil
		}
		if !errors.Is(e, policy.ErrDispatchBusy) {
			return nil, e
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
func (g *CallGate) call(w http.ResponseWriter, r *http.Request) {
	g.active.Add(1)
	defer g.active.Add(-1)
	g.attempts.Add(1)
	if r.Method != http.MethodPost || r.URL.Path != "/responses" || r.URL.RawPath != "" || r.URL.RawQuery != "" || r.URL.IsAbs() || r.URL.Fragment != "" || r.Header.Get("X-OpenAI-Subagent") != "" {
		g.reject(w, 400)
		return
	}
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || media != "application/json" || r.Header.Get("Content-Encoding") != "" {
		g.reject(w, 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	raw, e := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	marker, _ := ctx.Value(gateMarkerKey{}).(string)
	if e != nil || !g.request(raw) || privateJSON(raw, []byte(marker)) {
		g.reject(w, 400)
		return
	}
	select {
	case g.queue <- struct{}{}:
		defer func() { <-g.queue }()
	default:
		g.reject(w, 409)
		return
	}
	release, e := g.acquire(ctx)
	if e != nil {
		g.reject(w, 403)
		return
	}
	defer release()
	if !g.live(ctx) {
		g.reject(w, 403)
		return
	}
	if e = g.permit(ctx, g.claims, cloneGate(g.binding).Target, 1); e != nil {
		g.reject(w, 403)
		return
	}
	g.permits.Add(1)
	if !g.live(ctx) {
		g.reject(w, 403)
		return
	}
	g.forwarded.Add(1)
	response, e := g.forwarder.Send(ctx, cloneGate(g.binding).Target, append([]byte(nil), raw...))
	if response.Body != nil {
		defer response.Body.Close()
	}
	if e != nil || response.Body == nil {
		g.reject(w, 502)
		return
	}
	if response.StatusCode == 429 {
		if !g.live(ctx) {
			g.reject(w, 403)
			return
		}
		g.retryable.Add(1)
		gateError(w, 429)
		return
	}
	media, _, e = mime.ParseMediaType(response.ContentType)
	if response.StatusCode != 200 || e != nil || media != "text/event-stream" || response.ContentEncoding != "" || response.ReportedModel != g.binding.Target.ResolvedModel {
		g.reject(w, 502)
		return
	}
	body, e := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	if e != nil || len(body) > 8<<20 {
		g.reject(w, 502)
		return
	}
	secrets := append([][]byte(nil), response.PrivateMarkers...)
	if marker != "" {
		secrets = append(secrets, []byte(marker))
	}
	for _, s := range secrets {
		if len(s) == 0 || len(s) > 16<<10 || bytes.Contains(body, s) {
			g.reject(w, 502)
			return
		}
	}
	if !responsesStream(body, g.binding.Target.ResolvedModel, secrets...) || !g.live(ctx) {
		g.reject(w, 502)
		return
	}
	// No upstream headers, cookies, partial SSE or private errors reach Native.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	n, e := w.Write(body)
	if e != nil || n != len(body) {
		g.uncertain.Store(true)
		return
	}
	g.completed.Add(1)
}
func object(raw []byte, allowed ...string) (map[string]json.RawMessage, bool) {
	var m map[string]json.RawMessage
	if decode(raw, &m) != nil || m == nil {
		return nil, false
	}
	for k := range m {
		ok := false
		for _, a := range allowed {
			ok = ok || a == k
		}
		if !ok {
			return nil, false
		}
	}
	return m, true
}
func stringField(m map[string]json.RawMessage, k string) (string, bool) {
	var s string
	e := json.Unmarshal(m[k], &s)
	return s, e == nil && !bytes.Equal(bytes.TrimSpace(m[k]), []byte("null"))
}
func array(raw []byte) ([]json.RawMessage, bool) {
	var a []json.RawMessage
	ok := decode(raw, &a) == nil && a != nil
	return a, ok
}
func privateJSON(raw []byte, markers ...[]byte) bool {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return true
	}
	var walk func(any) bool
	walk = func(v any) bool {
		switch x := v.(type) {
		case string:
			for _, s := range markers {
				if len(s) > 0 && strings.Contains(x, string(s)) {
					return true
				}
			}
		case []any:
			for _, e := range x {
				if walk(e) {
					return true
				}
			}
		case map[string]any:
			for k, e := range x {
				if walk(k) || walk(e) {
					return true
				}
			}
		}
		return false
	}
	return walk(v)
}
func (g *CallGate) request(raw []byte) bool {
	f, ok := object(raw, "model", "instructions", "input", "tools", "tool_choice", "parallel_tool_calls", "reasoning", "store", "stream", "include", "text", "prompt_cache_key", "client_metadata")
	if !ok {
		return false
	}
	model, ok := stringField(f, "model")
	if !ok || model != g.binding.Target.RequestedModel {
		return false
	}
	for k, want := range map[string]bool{"store": false, "stream": true} {
		var v bool
		if json.Unmarshal(f[k], &v) != nil || bytes.Equal(bytes.TrimSpace(f[k]), []byte("null")) || v != want {
			return false
		}
	}
	var parallel bool
	if json.Unmarshal(f["parallel_tool_calls"], &parallel) != nil || bytes.Equal(bytes.TrimSpace(f["parallel_tool_calls"]), []byte("null")) {
		return false
	}
	choice, ok := stringField(f, "tool_choice")
	if !ok || (choice != "auto" && choice != "none") {
		return false
	}
	if v, exists := f["tools"]; exists {
		a, ok := array(v)
		if !ok || len(a) != 0 {
			return false
		}
	}
	reasoning, ok := object(f["reasoning"], "effort")
	if !ok {
		return false
	}
	eff, ok := stringField(reasoning, "effort")
	if !ok || eff != *g.binding.Target.Effort.Value {
		return false
	}
	for _, k := range []string{"instructions", "prompt_cache_key"} {
		if v, exists := f[k]; exists {
			var s string
			if json.Unmarshal(v, &s) != nil || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
				return false
			}
			if k == "prompt_cache_key" && (len(s) == 0 || len(s) > 256) {
				return false
			}
		}
	}
	includes, ok := array(f["include"])
	if !ok || len(includes) > 1 {
		return false
	}
	for _, v := range includes {
		var s string
		if json.Unmarshal(v, &s) != nil || s != "reasoning.encrypted_content" {
			return false
		}
	}
	if v, exists := f["text"]; exists {
		x, ok := object(v, "verbosity")
		s, valid := stringField(x, "verbosity")
		if !ok || !valid || (s != "low" && s != "medium" && s != "high") {
			return false
		}
	}
	if v, exists := f["client_metadata"]; exists {
		meta, ok := object(v, "root_turn_id", "session_id", "thread_id", "turn_id", "x-codex-installation-id", "x-codex-turn-metadata", "x-codex-window-id")
		if !ok {
			return false
		}
		for k := range meta {
			s, ok := stringField(meta, k)
			if !ok || len(s) == 0 || len(s) > 4096 {
				return false
			}
		}
	}
	input, ok := array(f["input"])
	if !ok || len(input) == 0 || len(input) > 128 {
		return false
	}
	for _, v := range input {
		if !inputItem(v) {
			return false
		}
	}
	return true
}
func inputItem(raw []byte) bool {
	var kind struct{ Type string }
	if decode(raw, &kind) != nil {
		return false
	}
	if kind.Type == "reasoning" {
		return reasoningItem(raw)
	}
	f, ok := object(raw, "type", "id", "status", "role", "content")
	if !ok || kind.Type != "message" {
		return false
	}
	role, ok := stringField(f, "role")
	if !ok || (role != "user" && role != "assistant" && role != "developer" && role != "system") {
		return false
	}
	for _, k := range []string{"id", "status"} {
		if _, exists := f[k]; exists {
			s, ok := stringField(f, k)
			if !ok || s == "" {
				return false
			}
			if k == "status" && s != "completed" {
				return false
			}
		}
	}
	content, ok := array(f["content"])
	if !ok || len(content) == 0 || len(content) > 128 {
		return false
	}
	for _, v := range content {
		x, ok := object(v, "type", "text", "annotations", "logprobs")
		typ, valid := stringField(x, "type")
		_, textOK := stringField(x, "text")
		if !ok || !valid || !textOK || (role == "assistant" && typ != "output_text") || (role != "assistant" && typ != "input_text") {
			return false
		}
		for _, k := range []string{"annotations", "logprobs"} {
			if v, exists := x[k]; exists {
				a, ok := array(v)
				if !ok || len(a) != 0 {
					return false
				}
			}
		}
	}
	return true
}
func reasoningItem(raw []byte) bool {
	f, ok := object(raw, "type", "id", "summary", "encrypted_content", "status")
	if !ok {
		return false
	}
	typ, ok := stringField(f, "type")
	id, valid := stringField(f, "id")
	if !ok || !valid || typ != "reasoning" || !gateID.MatchString(id) {
		return false
	}
	a, ok := array(f["summary"])
	if !ok || len(a) != 0 {
		return false
	}
	if v, exists := f["encrypted_content"]; exists {
		var s string
		if json.Unmarshal(v, &s) != nil || bytes.Equal(bytes.TrimSpace(v), []byte("null")) || s == "" {
			return false
		}
	}
	if _, exists := f["status"]; exists {
		s, ok := stringField(f, "status")
		if !ok || (s != "in_progress" && s != "completed") {
			return false
		}
	}
	return true
}

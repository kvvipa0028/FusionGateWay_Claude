package grok

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/stageplan"
)

// GateBinding freezes both Native observations and the controller's exact
// registered route. None of these fields is accepted from a Native HTTP body.
type GateBinding struct {
	Observer Binding
	Target   stageplan.ExecutionTarget
}

// NativeForwarder is trusted controller wiring for an independently admitted
// Native subscription route. Send must perform exactly one send, obey context,
// validate credential identity/billing/endpoint, and never retry or redirect.
// It receives no incoming HTTP headers or routing authority. There is no
// generic xAI API endpoint/key fallback supplied by this package.
type NativeForwarder interface {
	Send(context.Context, stageplan.ExecutionTarget, []byte) (ForwardResponse, error)
}
type ForwardResponse struct {
	StatusCode                   int
	ContentType, ContentEncoding string
	Body                         io.ReadCloser
	// The private forwarder must include its credential markers for reflection
	// checks. They are never passed to Native or retained in audit metadata.
	PrivateMarkers [][]byte `json:"-"`
}

func (ForwardResponse) String() string { return "Grok native response (private payload omitted)" }

type CallGateConfig struct {
	Binding GateBinding
	Manager *policy.Manager
	Claims  policy.Claims
	Current func(GateBinding) bool
	// Must recheck route, quota, permissions, physical reservation and spend
	// the shared persistent budget atomically (Scheduler.Permit in production).
	Permit    func(context.Context, policy.Claims, stageplan.ExecutionTarget, int) error
	Forwarder NativeForwarder
}
type CallAudit struct {
	Attempts, Permits, Forwarded, Completed, Retryable int64
	Uncertain                                          bool
}
type CallGate struct {
	binding                                                    GateBinding
	manager                                                    *policy.Manager
	claims                                                     policy.Claims
	current                                                    func(GateBinding) bool
	permit                                                     func(context.Context, policy.Claims, stageplan.ExecutionTarget, int) error
	forwarder                                                  NativeForwarder
	handler                                                    http.Handler
	queue                                                      chan struct{}
	attempts, permits, forwarded, completed, retryable, active atomic.Int64
	uncertain                                                  atomic.Bool
}
type markerKey struct{}

func copyGate(b GateBinding) GateBinding {
	b.Observer = clone(b.Observer)
	if b.Target.Effort.Value != nil {
		v := *b.Target.Effort.Value
		b.Target.Effort.Value = &v
	}
	if b.Target.PluginVersion != nil {
		v := *b.Target.PluginVersion
		b.Target.PluginVersion = &v
	}
	if b.Target.UpstreamReportedModel != nil {
		v := *b.Target.UpstreamReportedModel
		b.Target.UpstreamReportedModel = &v
	}
	b.Target.Capabilities = append([]string(nil), b.Target.Capabilities...)
	return b
}
func NewCallGate(c CallGateConfig) (*CallGate, error) {
	b, t := c.Binding.Observer, c.Binding.Target
	if c.Manager == nil || c.Current == nil || c.Permit == nil || c.Forwarder == nil || c.Claims.Audience != policy.ModelAudience || c.Claims.RunID != b.RunID || c.Claims.Generation != b.Generation || c.Claims.Role != b.Role || c.Claims.TaskID == "" || c.Claims.ProjectID == "" || c.Claims.Attempt < 1 || c.Claims.PlanRevision < 1 || t.Route.ID == "" || t.Route.Revision < 1 || t.RuntimeVersion != CLIVersion || t.RequestedModel != b.Model || t.ResolvedModel != b.Model || t.Account == "" || t.Workspace == "" || t.CredentialIdentity == "" || t.BillingPath != "subscription" || t.PluginVersion != nil || t.LockEnforcement != stageplan.ControlledCalls {
		return nil, ErrUnverified
	}
	if _, e := New(b, func(Binding) bool { return true }); e != nil {
		return nil, e
	}
	if t.Effort.RequestedMode == stageplan.EffortNone {
		if t.Effort.Value != nil {
			return nil, ErrUnverified
		}
	} else if (t.Effort.RequestedMode != stageplan.EffortExplicit && t.Effort.RequestedMode != stageplan.EffortDefault) || t.Effort.Value == nil || !modelID.MatchString(*t.Effort.Value) {
		return nil, ErrUnverified
	}
	g := &CallGate{binding: copyGate(c.Binding), manager: c.Manager, claims: c.Claims, current: c.Current, permit: c.Permit, forwarder: c.Forwarder, queue: make(chan struct{}, 4)}
	g.handler = c.Manager.Stage(c.Claims, http.HandlerFunc(g.call))
	return g, nil
}
func (g *CallGate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Authentication still occurs in Manager.Stage. Keeping a bounded private
	// marker in context lets the inner handler refuse grant reflection without
	// forwarding the original Authorization header to a transport.
	marker := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if len(marker) <= 256 {
		r = r.WithContext(context.WithValue(r.Context(), markerKey{}, marker))
	}
	g.handler.ServeHTTP(w, r)
}
func (g *CallGate) Audit() CallAudit {
	return CallAudit{Attempts: g.attempts.Load(), Permits: g.permits.Load(), Forwarded: g.forwarded.Load(), Completed: g.completed.Load(), Retryable: g.retryable.Load(), Uncertain: g.uncertain.Load()}
}
func (g *CallGate) live(ctx context.Context) bool {
	return ctx.Err() == nil && !g.uncertain.Load() && g.manager.ModelCurrent(ctx, g.claims) && g.current(copyGate(g.binding))
}

// Healthy observes this gate only. It is not admission/all-calls/stop proof.
// Native's complete protocol and the managed Worker's stop must also validate.
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
	body, _ := json.Marshal(map[string]any{"error": map[string]any{"message": http.StatusText(status), "type": kind, "code": code, "param": nil}})
	w.WriteHeader(status)
	w.Write(body)
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
	if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" || r.URL.RawPath != "" || r.URL.RawQuery != "" || r.URL.IsAbs() || r.URL.Fragment != "" {
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
	marker, _ := ctx.Value(markerKey{}).(string)
	if e != nil || !g.request(raw) || marker != "" && bytes.Contains(raw, []byte(marker)) {
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
	if e = g.permit(ctx, g.claims, copyGate(g.binding).Target, 1); e != nil {
		g.reject(w, 403)
		return
	}
	g.permits.Add(1)
	if !g.live(ctx) {
		g.reject(w, 403)
		return
	}
	g.forwarded.Add(1)
	response, e := g.forwarder.Send(ctx, copyGate(g.binding).Target, append([]byte(nil), raw...))
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
	if response.StatusCode != 200 || e != nil || media != "text/event-stream" || response.ContentEncoding != "" {
		g.reject(w, 502)
		return
	}
	body, e := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	if e != nil || len(body) > 8<<20 || marker != "" && bytes.Contains(body, []byte(marker)) {
		g.reject(w, 502)
		return
	}
	for _, secret := range response.PrivateMarkers {
		if len(secret) == 0 || len(secret) > 4096 || bytes.Contains(body, secret) {
			g.reject(w, 502)
			return
		}
	}
	if !completionStream(body, g.binding.Target.ResolvedModel) || !g.live(ctx) {
		g.reject(w, 502)
		return
	}
	// Validate the full bounded SSE before releasing anything, so a late model
	// mismatch, tool instruction, error or incomplete terminal cannot escape.
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
func keys(fields map[string]json.RawMessage, allowed ...string) bool {
	for k := range fields {
		found := false
		for _, a := range allowed {
			if k == a {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return fields != nil
}
func object(raw []byte, allowed ...string) (map[string]json.RawMessage, bool) {
	var m map[string]json.RawMessage
	if decode(raw, &m) != nil || !keys(m, allowed...) {
		return nil, false
	}
	return m, true
}
func (g *CallGate) request(raw []byte) bool {
	f, ok := object(raw, "model", "messages", "stream", "stream_options", "tools", "tool_choice", "temperature", "top_p", "max_tokens", "reasoning_effort")
	if !ok {
		return false
	}
	var r struct {
		Model    string
		Stream   *bool
		Messages []struct {
			Role    string
			Content *string
		}
		Tools             []json.RawMessage
		MaxTokens         *int64 `json:"max_tokens"`
		Temperature, TopP *float64
		Effort            *string `json:"reasoning_effort"`
	}
	if decode(raw, &r) != nil || r.Model != g.binding.Target.RequestedModel || r.Stream == nil || !*r.Stream || len(r.Messages) < 1 || len(r.Messages) > 128 {
		return false
	}
	for _, v := range []string{"max_tokens", "temperature", "top_p", "reasoning_effort"} {
		if b, ok := f[v]; ok && bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
			return false
		}
	}
	if r.MaxTokens != nil && (*r.MaxTokens < 1 || *r.MaxTokens > 131072) || r.Temperature != nil && (*r.Temperature < 0 || *r.Temperature > 2) {
		return false
	}
	if v, ok := f["top_p"]; ok {
		var p float64
		if json.Unmarshal(v, &p) != nil || p < 0 || p > 1 {
			return false
		}
	}
	var messages []json.RawMessage
	if decode(f["messages"], &messages) != nil {
		return false
	}
	for i, m := range r.Messages {
		if m.Content == nil || (m.Role != "system" && m.Role != "user" && m.Role != "assistant") {
			return false
		}
		if _, ok := object(messages[i], "role", "content"); !ok {
			return false
		}
	}
	eff := g.binding.Target.Effort
	if eff.RequestedMode == stageplan.EffortNone {
		if r.Effort != nil {
			return false
		}
	} else if r.Effort == nil || *r.Effort != *eff.Value {
		return false
	}
	if v, ok := f["stream_options"]; ok {
		opt, valid := object(v, "include_usage")
		var yes bool
		if !valid || json.Unmarshal(opt["include_usage"], &yes) != nil || !yes {
			return false
		}
	}
	allowed := map[string]bool{"session_title": true}
	for _, tool := range g.binding.Observer.Tools {
		allowed[tool] = true
	}
	seen := map[string]bool{}
	if len(r.Tools) > 16 {
		return false
	}
	for _, raw := range r.Tools {
		t, ok := object(raw, "type", "function")
		var typ string
		if !ok || json.Unmarshal(t["type"], &typ) != nil || typ != "function" {
			return false
		}
		fn, ok := object(t["function"], "name", "description", "parameters", "strict")
		var name string
		if !ok || json.Unmarshal(fn["name"], &name) != nil || !allowed[name] || seen[name] {
			return false
		}
		seen[name] = true
		var params map[string]json.RawMessage
		if decode(fn["parameters"], &params) != nil || params == nil {
			return false
		}
		if v, ok := fn["strict"]; ok {
			var strict bool
			if json.Unmarshal(v, &strict) != nil {
				return false
			}
		}
		if v, ok := fn["description"]; ok {
			var desc string
			if json.Unmarshal(v, &desc) != nil {
				return false
			}
		}
	}
	if v, ok := f["tool_choice"]; ok {
		var kind string
		if json.Unmarshal(v, &kind) == nil {
			return kind == "auto" || kind == "none" || kind == "required" && len(seen) > 0
		}
		choice, ok := object(v, "type", "function")
		if !ok || json.Unmarshal(choice["type"], &kind) != nil || kind != "function" {
			return false
		}
		fn, ok := object(choice["function"], "name")
		var name string
		if !ok || json.Unmarshal(fn["name"], &name) != nil || !seen[name] {
			return false
		}
	}
	return true
}

// The current native diagnostic establishes text-only Chat Completions SSE.
// Tool execution, backend search and new delta types remain unsupported.
func completionStream(raw []byte, model string) bool {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var data []string
	id := ""
	started, stopped, done := false, false, false
	frames := 0
	consume := func() bool {
		if len(data) == 0 {
			return true
		}
		if done {
			return false
		}
		payload := strings.Join(data, "\n")
		data = nil
		frames++
		if frames > 4096 {
			return false
		}
		if payload == "[DONE]" {
			if !stopped {
				return false
			}
			done = true
			return true
		}
		fields, ok := object([]byte(payload), "id", "object", "created", "model", "choices", "usage", "system_fingerprint")
		if !ok {
			return false
		}
		var f struct {
			ID, Object, Model string
			Created           *int64
			Choices           []struct {
				Index  *int
				Delta  json.RawMessage
				Finish *string `json:"finish_reason"`
			}
			Usage json.RawMessage
		}
		if decode([]byte(payload), &f) != nil || f.ID == "" || len(f.ID) > 256 || f.Object != "chat.completion.chunk" || f.Model != model || f.Created == nil || *f.Created < 1 {
			return false
		}
		var choices []json.RawMessage
		if decode(fields["choices"], &choices) != nil || choices == nil {
			return false
		}
		for _, rawChoice := range choices {
			choice, ok := object(rawChoice, "index", "delta", "finish_reason", "logprobs")
			if !ok {
				return false
			}
			if v, ok := choice["logprobs"]; ok && !bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
				return false
			}
		}
		if id == "" {
			id = f.ID
		} else if f.ID != id {
			return false
		}
		if len(f.Choices) == 0 {
			if !stopped || len(f.Usage) == 0 {
				return false
			}
		} else {
			if len(f.Choices) != 1 || stopped {
				return false
			}
			c := f.Choices[0]
			if c.Index == nil || *c.Index != 0 {
				return false
			}
			delta, ok := object(c.Delta, "role", "content")
			if !ok {
				return false
			}
			if v, ok := delta["role"]; ok {
				var role string
				if json.Unmarshal(v, &role) != nil || role != "assistant" {
					return false
				}
			}
			if v, ok := delta["content"]; ok && !bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
				var content string
				if json.Unmarshal(v, &content) != nil {
					return false
				}
			}
			started = true
			if c.Finish != nil {
				if *c.Finish != "stop" {
					return false
				}
				stopped = true
			}
		}
		if len(f.Usage) > 0 && !bytes.Equal(bytes.TrimSpace(f.Usage), []byte("null")) {
			var u struct {
				Input  *int64 `json:"prompt_tokens"`
				Output *int64 `json:"completion_tokens"`
				Total  *int64 `json:"total_tokens"`
			}
			if decode(f.Usage, &u) != nil || u.Input == nil || u.Output == nil || u.Total == nil || *u.Input < 0 || *u.Output < 0 || *u.Input > 1e9 || *u.Output > 1e9 || *u.Total != *u.Input+*u.Output {
				return false
			}
		}
		return true
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if !consume() {
				return false
			}
			continue
		}
		if done {
			return false
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			return false
		}
		data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
	}
	return scanner.Err() == nil && consume() && started && stopped && done
}

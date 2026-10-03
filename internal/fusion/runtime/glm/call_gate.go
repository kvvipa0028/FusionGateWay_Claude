package glm

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
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/stageplan"
)

// CallGateConfig is assembled by the controller, never decoded from HTTP.
// Transport must be a separately admitted, single-send/no-redirect transport.
// Permit must check current route/quota/reservation and atomically spend the
// shared persisted budget. The Native process gets only a stage grant, not Key.
type CallGateConfig struct {
	Binding   Binding
	Manager   *policy.Manager
	Claims    policy.Claims
	APIKey    string
	Current   func(Binding) bool
	Permit    func(context.Context, policy.Claims, stageplan.ExecutionTarget, int) error
	Transport http.RoundTripper
}

type CallGate struct {
	binding   Binding
	manager   *policy.Manager
	claims    policy.Claims
	key       string
	current   func(Binding) bool
	permit    func(context.Context, policy.Claims, stageplan.ExecutionTarget, int) error
	transport http.RoundTripper
	handler   http.Handler
}

func NewCallGate(c CallGateConfig) (*CallGate, error) {
	if c.Manager == nil || c.Current == nil || c.Permit == nil || c.Transport == nil || len(c.APIKey) < 8 || len(c.APIKey) > 4096 || strings.ContainsAny(c.APIKey, " \t\r\n\x00") || c.Binding.Target.LockEnforcement != stageplan.ControlledCalls || c.Claims.Audience != policy.ModelAudience || c.Claims.RunID != c.Binding.RunID || c.Claims.Generation != c.Binding.Generation || c.Claims.Role != c.Binding.Role || c.Claims.TaskID == "" || c.Claims.ProjectID == "" || c.Claims.Attempt < 1 || c.Claims.PlanRevision < 1 {
		return nil, ErrUnverified
	}
	if _, e := New(c.Binding, c.Current); e != nil {
		return nil, e
	}
	effort := c.Binding.Target.Effort
	if effort.RequestedMode == stageplan.EffortNone {
		if effort.Value != nil {
			return nil, ErrUnverified
		}
	} else if (effort.RequestedMode != stageplan.EffortExplicit && effort.RequestedMode != stageplan.EffortDefault) || effort.Value == nil || !knownEffort(*effort.Value) {
		return nil, ErrUnverified
	}
	g := &CallGate{binding: clone(c.Binding), manager: c.Manager, claims: c.Claims, key: c.APIKey, current: c.Current, permit: c.Permit, transport: c.Transport}
	g.handler = c.Manager.Stage(c.Claims, http.HandlerFunc(g.call))
	return g, nil
}
func knownEffort(v string) bool { return v == "low" || v == "medium" || v == "high" || v == "max" }
func (g *CallGate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.handler.ServeHTTP(w, r)
}
func gateError(w http.ResponseWriter, status int) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.Error(w, http.StatusText(status), status)
}
func (g *CallGate) live(ctx context.Context) bool {
	return g.manager.ModelCurrent(ctx, g.claims) && g.current(clone(g.binding))
}
func (g *CallGate) call(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/api/anthropic/v1/messages" || r.URL.RawPath != "" || r.URL.RawQuery != "" && r.URL.RawQuery != "beta=true" || r.URL.IsAbs() || r.URL.Fragment != "" {
		gateError(w, http.StatusBadRequest)
		return
	}
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || media != "application/json" || r.Header.Get("Content-Encoding") != "" {
		gateError(w, http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	raw, e := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if e != nil || !g.request(raw) {
		gateError(w, http.StatusBadRequest)
		return
	}
	release, e := g.manager.BeginModelCall(ctx, g.claims)
	if e != nil {
		status := http.StatusForbidden
		if errors.Is(e, policy.ErrDispatchBusy) {
			status = http.StatusConflict
		}
		gateError(w, status)
		return
	}
	defer release()
	if !g.live(ctx) {
		gateError(w, http.StatusForbidden)
		return
	}
	// Each incoming HTTP request, including Native SDK retries, consumes one
	// permit. Transport errors never refund it, retry, or select another route.
	if e = g.permit(ctx, g.claims, clone(g.binding).Target, 1); e != nil || !g.live(ctx) {
		gateError(w, http.StatusForbidden)
		return
	}
	upstream, e := http.NewRequestWithContext(ctx, http.MethodPost, Endpoint+"/v1/messages?beta=true", bytes.NewReader(raw))
	if e != nil {
		gateError(w, http.StatusBadGateway)
		return
	}
	upstream.GetBody = nil // no replay body for transport retry/redirect helpers
	upstream.Header.Set("Content-Type", "application/json")
	upstream.Header.Set("Accept", "text/event-stream")
	upstream.Header.Set("Anthropic-Version", "2023-06-01")
	upstream.Header.Set("Authorization", "Bearer "+g.key)
	upstream.Header.Set("X-Api-Key", g.key)
	response, e := g.transport.RoundTrip(upstream)
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if e != nil || response == nil || response.Body == nil {
		gateError(w, http.StatusBadGateway)
		return
	}
	if response.StatusCode != http.StatusOK {
		status := http.StatusBadGateway
		if response.StatusCode == http.StatusTooManyRequests {
			status = http.StatusTooManyRequests
		}
		gateError(w, status)
		return
	}
	media, _, e = mime.ParseMediaType(response.Header.Get("Content-Type"))
	if e != nil || media != "text/event-stream" || response.Header.Get("Content-Encoding") != "" {
		gateError(w, http.StatusBadGateway)
		return
	}
	body, e := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	if e != nil || len(body) > 8<<20 || bytes.Contains(body, []byte(g.key)) || !modelStream(body, g.binding.Target.ResolvedModel) || !g.live(ctx) {
		gateError(w, http.StatusBadGateway)
		return
	}
	// Buffer the bounded response before delivery so a late model mismatch or
	// missing terminal cannot turn earlier bytes into a successful Native call.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Write(body)
}

func (g *CallGate) request(raw []byte) bool {
	var fields map[string]json.RawMessage
	if decode(raw, &fields) != nil || fields == nil {
		return false
	}
	for key := range fields {
		switch key {
		case "model", "messages", "max_tokens", "stream", "system", "tools", "tool_choice", "metadata", "thinking", "output_config", "temperature", "top_p", "top_k", "stop_sequences", "context_management":
		default:
			return false
		}
	}
	var request struct {
		Model     string
		Stream    bool
		MaxTokens int64 `json:"max_tokens"`
		Messages  []json.RawMessage
		Tools     []struct{ Name string }
	}
	if decode(raw, &request) != nil || request.Model != g.binding.Target.RequestedModel || !request.Stream || request.MaxTokens < 1 || request.MaxTokens > 131072 || len(request.Messages) < 1 || len(request.Messages) > 128 {
		return false
	}
	allowed := map[string]bool{}
	for _, tool := range g.binding.Tools {
		allowed[tool] = true
	}
	seen := map[string]bool{}
	for _, tool := range request.Tools {
		if !allowed[tool.Name] || seen[tool.Name] {
			return false
		}
		seen[tool.Name] = true
	}
	if raw, ok := fields["tool_choice"]; ok {
		var choice map[string]json.RawMessage
		var kind, name string
		if decode(raw, &choice) != nil || choice == nil || json.Unmarshal(choice["type"], &kind) != nil {
			return false
		}
		for key := range choice {
			if key != "type" && key != "name" && key != "disable_parallel_tool_use" {
				return false
			}
		}
		if raw, ok := choice["disable_parallel_tool_use"]; ok {
			var disabled bool
			if json.Unmarshal(raw, &disabled) != nil {
				return false
			}
		}
		switch kind {
		case "auto", "any":
			if _, exists := choice["name"]; exists {
				return false
			}
		case "tool":
			if json.Unmarshal(choice["name"], &name) != nil || !seen[name] {
				return false
			}
		default:
			return false
		}
	}
	var output map[string]json.RawMessage
	if raw, ok := fields["output_config"]; ok && (decode(raw, &output) != nil || output == nil) {
		return false
	}
	for key := range output {
		if key != "effort" {
			return false
		}
	}
	var effort string
	if raw, ok := output["effort"]; ok && json.Unmarshal(raw, &effort) != nil {
		return false
	}
	var thinking map[string]json.RawMessage
	if raw, ok := fields["thinking"]; ok && (decode(raw, &thinking) != nil || thinking == nil) {
		return false
	}
	for key := range thinking {
		if key != "type" && key != "display" {
			return false
		}
	}
	var kind string
	if raw, ok := thinking["type"]; ok && json.Unmarshal(raw, &kind) != nil {
		return false
	}
	if g.binding.Target.Effort.RequestedMode == stageplan.EffortNone {
		return effort == "" && (kind == "" || kind == "disabled")
	}
	return effort == *g.binding.Target.Effort.Value && (kind == "adaptive" || kind == "disabled" || kind == "")
}

// modelStream verifies one complete Messages SSE response and every frame's
// canonical type before any bytes are released. Native Session validates the
// content/tool lifecycle separately; this function is not a stage result.
func modelStream(raw []byte, model string) bool {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var event string
	var data []string
	started, stopped := false, false
	consume := func() bool {
		if event == "" && len(data) == 0 {
			return true
		}
		var frame struct {
			Type    string
			Message struct{ Model string }
		}
		raw := []byte(strings.Join(data, "\n"))
		var fields map[string]json.RawMessage
		var kind string
		if decode(raw, &fields) != nil || json.Unmarshal(fields["type"], &kind) != nil || kind != event || decode(raw, &frame) != nil || frame.Type != event {
			return false
		}
		switch event {
		case "ping":
		case "message_start":
			var message map[string]json.RawMessage
			var literalModel string
			if decode(fields["message"], &message) != nil || json.Unmarshal(message["model"], &literalModel) != nil || literalModel != model || started || stopped || frame.Message.Model != model {
				return false
			}
			started = true
		case "message_stop":
			if !started || stopped {
				return false
			}
			stopped = true
		case "content_block_start", "content_block_delta", "content_block_stop", "message_delta":
			if !started || stopped {
				return false
			}
		default:
			return false
		}
		event, data = "", nil
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
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			if event != "" {
				return false
			}
			event = strings.TrimPrefix(strings.TrimPrefix(line, "event:"), " ")
		} else if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		} else {
			return false
		}
	}
	return scanner.Err() == nil && consume() && started && stopped
}

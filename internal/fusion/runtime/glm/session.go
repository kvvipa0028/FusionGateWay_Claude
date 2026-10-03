// Package glm observes the pinned Claude Code protocol for the approved CN
// Coding Plan route. Native protocol success does not establish route admission.
package glm

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"unicode"
	"unicode/utf8"

	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/stageplan"
)

const CLIVersion = managed.ClaudeCLIVersion
const Endpoint = "https://open.bigmodel.cn/api/anthropic"

var (
	ErrProtocol    = errors.New("pinned Claude Code protocol mismatch")
	ErrIdentity    = errors.New("GLM execution identity changed")
	ErrUnverified  = errors.New("GLM native call boundary unverified")
	ErrUnsupported = errors.New("GLM native capability unsupported")
	modelID        = regexp.MustCompile(`^glm-[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
)

type Binding struct {
	RunID                                  string
	Generation                             int64
	Role                                   stageplan.Role
	NativeSessionID, Cwd, Endpoint, Region string
	Target                                 stageplan.ExecutionTarget
	Tools                                  []string
	MaxTurns                               int64
}

// Outcome contains native observations only. Cost, quota and all-calls admission
// are verified separately by the controller, never inferred from these fields.
type Outcome struct {
	State, SessionID, NativeModel, NativeReason, BillingPath             string
	InputTokens, OutputTokens, ObservedMessages                          int64
	APIErrorStatus                                                       int
	StrictLockVerified, BillingVerified, QuotaVerified, UpstreamVerified bool
}
type Session struct {
	mu                                       sync.Mutex
	binding                                  Binding
	current                                  func(Binding) bool
	state                                    string
	initialized, cancelled, denied, finished bool
	pending                                  string
	outcome                                  Outcome
	bytes, frames                            int
	messages                                 map[string]bool
	tools                                    map[string]bool
	activeMessage                            string
	blocks                                   map[int]string
	blockIndexes                             map[int]bool
}

func clone(b Binding) Binding {
	b.Tools = append([]string(nil), b.Tools...)
	b.Target.Capabilities = append([]string(nil), b.Target.Capabilities...)
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
	return b
}
func New(b Binding, current func(Binding) bool) (*Session, error) {
	known := false
	for _, r := range stageplan.AllRoles() {
		if r == b.Role {
			known = true
		}
	}
	if current == nil || !known || b.RunID == "" || b.Generation < 1 || b.NativeSessionID == "" || len(b.NativeSessionID) > 256 || !filepath.IsAbs(b.Cwd) || filepath.Clean(b.Cwd) != b.Cwd || b.Endpoint != Endpoint || b.Region != "CN" || b.Target.RuntimeVersion != CLIVersion || !modelID.MatchString(b.Target.RequestedModel) || b.Target.RequestedModel != b.Target.ResolvedModel || b.Target.Account == "" || b.Target.Workspace == "" || b.Target.CredentialIdentity == "" || b.Target.BillingPath != "coding_plan" || b.MaxTurns < 1 || b.MaxTurns > 1000 {
		return nil, ErrProtocol
	}
	tools := map[string]bool{}
	for _, tool := range b.Tools {
		switch tool {
		case "Read", "Grep", "Glob", "Bash":
		case "Write", "Edit":
			if b.Role != stageplan.Implementation && b.Role != stageplan.Testing {
				return nil, ErrUnverified
			}
		default:
			return nil, ErrUnsupported
		}
		if tools[tool] {
			return nil, ErrProtocol
		}
		tools[tool] = true
	}
	return &Session{binding: clone(b), current: current, state: "awaiting_init", messages: map[string]bool{}, tools: map[string]bool{}, outcome: Outcome{SessionID: b.NativeSessionID, BillingPath: b.Target.BillingPath}}, nil
}
func (s *Session) State() string { s.mu.Lock(); defer s.mu.Unlock(); return s.state }
func (s *Session) fail(e error) (json.RawMessage, error) {
	s.state = "execution_uncertain"
	return nil, e
}
func (s *Session) check() error {
	if !s.current(clone(s.binding)) {
		return ErrIdentity
	}
	return nil
}
func (s *Session) Event(run string, generation int64, raw []byte) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if run != s.binding.RunID || generation != s.binding.Generation {
		return nil, ErrIdentity
	}
	if e := s.check(); e != nil {
		return s.fail(e)
	}
	if s.finished || s.state == "execution_uncertain" {
		return nil, ErrProtocol
	}
	s.bytes += len(raw)
	s.frames++
	if s.bytes > 8<<20 || s.frames > 4096 {
		return s.fail(ErrProtocol)
	}
	var frame struct {
		Type, Subtype string
		SessionID     string          `json:"session_id"`
		Parent        json.RawMessage `json:"parent_tool_use_id"`
		RequestID     string          `json:"request_id"`
		Request       json.RawMessage
		Event         json.RawMessage
		Message       json.RawMessage
	}
	if decode(raw, &frame) != nil {
		return s.fail(ErrProtocol)
	}
	if frame.Type == "control_request" {
		if !s.initialized || s.pending != "" || frame.RequestID == "" || len(frame.RequestID) > 256 {
			return s.fail(ErrProtocol)
		}
		var request struct{ Subtype string }
		if decode(frame.Request, &request) != nil || request.Subtype != "can_use_tool" {
			return s.fail(ErrUnsupported)
		}
		s.denied = true
		reply, _ := json.Marshal(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": frame.RequestID, "response": map[string]string{"behavior": "deny", "message": "stage permissions do not allow this tool"}}})
		return reply, nil
	}
	if frame.SessionID != s.binding.NativeSessionID || len(frame.Parent) > 0 && !bytes.Equal(bytes.TrimSpace(frame.Parent), []byte("null")) {
		return s.fail(ErrUnverified)
	}
	if s.pending != "" {
		return s.fail(ErrProtocol)
	}
	switch frame.Type {
	case "system":
		switch frame.Subtype {
		case "ui_invalidate":
		case "init":
			if s.initialized {
				return s.fail(ErrProtocol)
			}
			var init struct {
				Model, Cwd     string
				Version        string `json:"claude_code_version"`
				PermissionMode string
				Tools          *[]string
				MCP            *[]json.RawMessage `json:"mcp_servers"`
			}
			if decode(raw, &init) != nil || init.Model != s.binding.Target.ResolvedModel || init.Version != CLIVersion || init.Cwd != s.binding.Cwd || init.PermissionMode != "dontAsk" || init.Tools == nil || !sameTools(*init.Tools, s.binding.Tools) || init.MCP == nil || len(*init.MCP) != 0 {
				return s.fail(ErrUnverified)
			}
			s.initialized = true
			if s.cancelled {
				s.state = "cancelling"
			} else {
				s.state = "running"
			}
		case "status":
			if !s.initialized {
				return s.fail(ErrProtocol)
			}
			var status struct{ Status string }
			if decode(raw, &status) != nil || status.Status != "requesting" && status.Status != "thinking" && status.Status != "responding" && status.Status != "idle" {
				return s.fail(ErrUnsupported)
			}
		case "api_retry":
			// Pinned Native emits this before retrying a failed Messages HTTP
			// request. It is informational: CallGate must permit every request.
			var retry struct {
				Attempt *int
				Max     *int `json:"max_retries"`
				Delay   *int `json:"retry_delay_ms"`
				Status  *int `json:"error_status"`
			}
			if !s.initialized || s.cancelled || s.activeMessage != "" || decode(raw, &retry) != nil || retry.Attempt == nil || retry.Max == nil || retry.Delay == nil || retry.Status == nil || *retry.Max < 1 || *retry.Max > 10 || *retry.Attempt < 1 || *retry.Attempt > *retry.Max || *retry.Delay < 0 || *retry.Delay > 60000 || *retry.Status != 429 && *retry.Status != 502 {
				return s.fail(ErrProtocol)
			}
		default:
			return s.fail(ErrUnsupported)
		}
	case "stream_event":
		if !s.initialized {
			return s.fail(ErrProtocol)
		}
		var event struct {
			Type    string
			Index   *int
			Message json.RawMessage
			Block   json.RawMessage `json:"content_block"`
			Delta   json.RawMessage
		}
		if decode(frame.Event, &event) != nil {
			return s.fail(ErrProtocol)
		}
		switch event.Type {
		case "message_start":
			if e := s.message(event.Message, true); e != nil {
				return s.fail(e)
			}
		case "content_block_start":
			var block struct{ Type, Name string }
			if s.activeMessage == "" || event.Index == nil || *event.Index < 0 || *event.Index > 4095 || s.blockIndexes[*event.Index] || decode(event.Block, &block) != nil || !s.content(block.Type, block.Name) {
				return s.fail(ErrUnverified)
			}
			s.blocks[*event.Index] = block.Type
			s.blockIndexes[*event.Index] = true
		case "content_block_delta":
			var delta struct{ Type string }
			if s.activeMessage == "" || event.Index == nil || s.blocks[*event.Index] == "" || decode(event.Delta, &delta) != nil {
				return s.fail(ErrProtocol)
			}
			kind := s.blocks[*event.Index]
			switch delta.Type {
			case "text_delta":
				if kind != "text" {
					return s.fail(ErrProtocol)
				}
			case "thinking_delta", "signature_delta":
				if kind != "thinking" {
					return s.fail(ErrProtocol)
				}
			case "input_json_delta":
				if kind != "tool_use" {
					return s.fail(ErrProtocol)
				}
			default:
				return s.fail(ErrUnsupported)
			}
		case "content_block_stop":
			if s.activeMessage == "" || event.Index == nil || s.blocks[*event.Index] == "" {
				return s.fail(ErrProtocol)
			}
			delete(s.blocks, *event.Index)
		case "message_delta":
			if s.activeMessage == "" || len(s.blocks) != 0 {
				return s.fail(ErrProtocol)
			}
		case "message_stop":
			if s.activeMessage == "" || !s.messages[s.activeMessage] || len(s.blocks) != 0 {
				return s.fail(ErrProtocol)
			}
			s.activeMessage = ""
		default:
			return s.fail(ErrUnsupported)
		}
	case "assistant":
		if !s.initialized {
			return s.fail(ErrProtocol)
		}
		if e := s.message(frame.Message, false); e != nil {
			return s.fail(e)
		}
	case "user":
		if !s.initialized || s.activeMessage != "" {
			return s.fail(ErrProtocol)
		}
		var message struct {
			Content []struct {
				Type      string
				ToolUseID string `json:"tool_use_id"`
			}
		}
		if decode(frame.Message, &message) != nil || len(message.Content) == 0 {
			return s.fail(ErrProtocol)
		}
		for _, content := range message.Content {
			complete, known := s.tools[content.ToolUseID]
			if content.Type != "tool_result" || !known || complete {
				return s.fail(ErrUnsupported)
			}
			s.tools[content.ToolUseID] = true
		}
	case "result":
		if e := s.result(raw); e != nil {
			return s.fail(e)
		}
		s.state = "result_pending"
	default:
		return s.fail(ErrUnsupported)
	}
	return nil, nil
}
func sameTools(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	a = append([]string(nil), a...)
	b = append([]string(nil), b...)
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func (s *Session) content(kind, name string) bool {
	switch kind {
	case "text", "thinking", "redacted_thinking":
		return true
	case "tool_use":
		for _, tool := range s.binding.Tools {
			if name == tool {
				return true
			}
		}
	}
	return false
}
func (s *Session) message(raw []byte, start bool) error {
	var message struct {
		ID, Model, Role string
		Content         []struct{ ID, Type, Name string }
	}
	if decode(raw, &message) != nil || message.ID == "" || message.Model != s.binding.Target.ResolvedModel || message.Role != "assistant" {
		return ErrUnverified
	}
	for _, block := range message.Content {
		if !s.content(block.Type, block.Name) {
			return ErrUnverified
		}
		if block.Type == "tool_use" && !start {
			if block.ID == "" || len(block.ID) > 256 || len(s.tools) >= 4096 {
				return ErrProtocol
			}
			if _, duplicate := s.tools[block.ID]; duplicate {
				return ErrProtocol
			}
			s.tools[block.ID] = false
		}
	}
	if start {
		_, duplicate := s.messages[message.ID]
		if s.activeMessage != "" || duplicate || int64(len(s.messages)) >= s.binding.MaxTurns {
			return ErrProtocol
		}
		s.activeMessage = message.ID
		s.blocks = map[int]string{}
		s.blockIndexes = map[int]bool{}
		s.messages[message.ID] = false
	} else {
		if s.activeMessage != message.ID || s.messages[message.ID] {
			return ErrProtocol
		}
		s.messages[message.ID] = true
	}
	s.outcome.NativeModel = message.Model
	s.outcome.ObservedMessages = int64(len(s.messages))
	return nil
}
func (s *Session) result(raw []byte) error {
	var r struct {
		Subtype     string
		IsError     *bool  `json:"is_error"`
		Terminal    string `json:"terminal_reason"`
		NumTurns    *int64 `json:"num_turns"`
		ResultIndex *int64 `json:"result_index"`
		QueuedTurns *int64 `json:"queued_turn_count"`
		APIError    *int   `json:"api_error_status"`
		Models      map[string]struct {
			InputTokens, OutputTokens *int64
			CanonicalModel            string
		} `json:"modelUsage"`
		Stats   json.RawMessage    `json:"subagent_stats"`
		Denials *[]json.RawMessage `json:"permission_denials"`
	}
	if decode(raw, &r) != nil || r.IsError == nil {
		return ErrProtocol
	}
	if r.APIError != nil {
		if *r.APIError < 100 || *r.APIError > 599 {
			return ErrProtocol
		}
		s.outcome.APIErrorStatus = *r.APIError
	}
	s.outcome.NativeReason = r.Terminal
	if *r.IsError {
		switch r.Subtype {
		case "error_during_execution", "error_max_turns", "error_max_budget_usd", "error_max_structured_output_retries":
		default:
			return ErrUnsupported
		}
		s.pending = "failed"
		if r.Terminal == "interrupted" {
			s.pending = "interrupted"
		}
		return nil
	}
	if !s.initialized || s.activeMessage != "" || r.Subtype != "success" || r.Terminal != "completed" || r.NumTurns == nil || *r.NumTurns < 1 || *r.NumTurns > s.binding.MaxTurns || *r.NumTurns != s.outcome.ObservedMessages || r.ResultIndex == nil || *r.ResultIndex != 0 || r.QueuedTurns == nil || *r.QueuedTurns != 0 {
		return ErrProtocol
	}
	if r.Denials == nil || len(*r.Denials) > 0 || s.denied || !zeroStats(r.Stats) {
		return ErrUnverified
	}
	for _, complete := range s.tools {
		if !complete {
			return ErrUnverified
		}
	}
	if len(r.Models) != 1 {
		return ErrUnverified
	}
	usage, ok := r.Models[s.binding.Target.ResolvedModel]
	if !ok || usage.CanonicalModel != "" && usage.CanonicalModel != s.binding.Target.ResolvedModel || usage.InputTokens == nil || usage.OutputTokens == nil || *usage.InputTokens < 0 || *usage.OutputTokens < 0 || *usage.InputTokens > 1e9 || *usage.OutputTokens > 1e9 {
		return ErrUnverified
	}
	s.outcome.InputTokens = *usage.InputTokens
	s.outcome.OutputTokens = *usage.OutputTokens
	s.pending = "succeeded"
	if s.cancelled {
		s.pending = "interrupted"
	}
	return nil
}
func zeroStats(raw []byte) bool {
	var fields map[string]json.RawMessage
	if decode(raw, &fields) != nil || fields["spawned"] == nil {
		return false
	}
	var value any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(&value) != nil {
		return false
	}
	var zero func(any) bool
	zero = func(v any) bool {
		switch v := v.(type) {
		case json.Number:
			n, e := v.Int64()
			return e == nil && n == 0
		case map[string]any:
			for _, x := range v {
				if !zero(x) {
					return false
				}
			}
			return true
		}
		return false
	}
	return zero(value)
}
func (s *Session) Cancel() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.check(); e != nil {
		s.state = "execution_uncertain"
		return e
	}
	if s.finished {
		return nil
	}
	s.cancelled = true
	if s.pending == "succeeded" {
		s.pending = "interrupted"
	}
	if s.pending == "" {
		s.state = "cancelling"
	}
	return nil
}
func (s *Session) Finish(exitCode int) (Outcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		return s.outcome, ErrProtocol
	}
	s.finished = true
	if e := s.check(); e != nil {
		s.state = "execution_uncertain"
		s.outcome.State = s.state
		return s.outcome, e
	}
	if s.state == "execution_uncertain" || s.pending == "" || s.pending == "succeeded" && exitCode != 0 {
		s.state = "execution_uncertain"
	} else {
		s.state = s.pending
	}
	s.outcome.State = s.state
	return s.outcome, nil
}

// Resume needs persistent native session storage and a separately proven
// recovery contract. A matching string or a prior result cannot supply it.
func (s *Session) Resume(Binding) error { return ErrUnsupported }

// Decode bounded native payloads without echoing upstream or repository text.
// Unknown informational fields are retained as protocol extensions; duplicate
// keys (including case collisions), nested overflow and trailing values fail.
func decode(raw []byte, out any) error {
	if len(raw) == 0 || len(raw) > 1<<20 || !utf8.Valid(raw) || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ErrProtocol
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if unique(d, 0) != nil {
		return ErrProtocol
	}
	if _, e := d.Token(); e != io.EOF {
		return ErrProtocol
	}
	if json.Unmarshal(raw, out) != nil {
		return ErrProtocol
	}
	return nil
}
func unique(d *json.Decoder, depth int) error {
	if depth > 32 {
		return ErrProtocol
	}
	t, e := d.Token()
	if e != nil {
		return e
	}
	v, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch v {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, e := d.Token()
			if e != nil {
				return e
			}
			name, ok := key.(string)
			if !ok {
				return ErrProtocol
			}
			name = foldKey(name)
			if seen[name] {
				return ErrProtocol
			}
			seen[name] = true
			if e = unique(d, depth+1); e != nil {
				return e
			}
		}
	case '[':
		for d.More() {
			if e = unique(d, depth+1); e != nil {
				return e
			}
		}
	default:
		return ErrProtocol
	}
	_, e = d.Token()
	return e
}
func foldKey(s string) string {
	b := []rune(s)
	for i, c := range b {
		for next := unicode.SimpleFold(c); next > c; next = unicode.SimpleFold(c) {
			c = next
		}
		b[i] = unicode.SimpleFold(c)
	}
	return string(b)
}

// Package grok observes the version-pinned native headless protocol. These
// observations do not authorize a process, a tool or an upstream model call.
package grok

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"regexp"
	"sync"
	"unicode"
	"unicode/utf8"

	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/stageplan"
)

const CLIVersion = managed.GrokCLIVersion
const NativeExecutableSHA256 = managed.GrokExecutableSHA256

var (
	ErrProtocol    = errors.New("pinned Grok headless protocol mismatch")
	ErrIdentity    = errors.New("Grok execution identity changed")
	ErrUnverified  = errors.New("Grok execution boundary unverified")
	ErrUnsupported = errors.New("Grok native capability unsupported")
	uuid           = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	toolID         = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	modelID        = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
)

// Binding is controller wiring, not an HTTP DTO or proof of route admission.
// The caller must independently validate the executable and current identity.
type Binding struct {
	RunID                                                         string
	Generation                                                    int64
	Role                                                          stageplan.Role
	NativeSessionID, Cwd, RuntimeVersion, ExecutableSHA256, Model string
	Tools                                                         []string
	MaxTurns                                                      int64
}

// Outcome deliberately separates native labels from trusted upstream evidence.
// NativeModelCalls counts only what end.modelUsage reports: auxiliary HTTP
// calls are absent from that record in the pinned real executable.
type Outcome struct {
	State, SessionID, RequestID, NativeModel, NativeReason                                string
	InputTokens, OutputTokens, NativeModelCalls, NativeTurns                              int64
	UpstreamReportedModel                                                                 *string
	StrictLockVerified, AllCallsVerified, BillingVerified, QuotaVerified, StoppedVerified bool
}
type Session struct {
	read                          *ReadTools
	mu                            sync.Mutex
	binding                       Binding
	current                       func(Binding) bool
	state, pending                string
	declared, cancelled, finished bool
	bytes, frames                 int
	outcome                       Outcome
}

func clone(b Binding) Binding { b.Tools = append([]string(nil), b.Tools...); return b }
func New(b Binding, current func(Binding) bool) (*Session, error) {
	known := false
	for _, r := range stageplan.AllRoles() {
		if r == b.Role {
			known = true
		}
	}
	if current == nil || !known || b.RunID == "" || len(b.RunID) > 256 || !utf8.ValidString(b.RunID) || hasNUL(b.RunID) || b.Generation < 1 || !uuid.MatchString(b.NativeSessionID) || !filepath.IsAbs(b.Cwd) || filepath.Clean(b.Cwd) != b.Cwd || !utf8.ValidString(b.Cwd) || hasNUL(b.Cwd) || b.RuntimeVersion != CLIVersion || b.ExecutableSHA256 != NativeExecutableSHA256 || !modelID.MatchString(b.Model) || b.MaxTurns < 1 || b.MaxTurns > 1000 {
		return nil, ErrProtocol
	}
	seen := map[string]bool{}
	for _, tool := range b.Tools {
		switch tool {
		case "read_file", "list_dir", "grep":
		default:
			return nil, ErrUnsupported
		}
		if seen[tool] {
			return nil, ErrProtocol
		}
		seen[tool] = true
	}
	if len(seen) == 0 {
		return nil, ErrUnsupported
	}
	return &Session{binding: clone(b), current: current, state: "awaiting_declaration", outcome: Outcome{SessionID: b.NativeSessionID}}, nil
}

// NewReadSession observes only tools pre-authorized by the same CallGate scope.
func NewReadSession(b Binding, current func(Binding) bool, read *ReadTools) (*Session, error) {
	if read == nil || !read.matches(b) || !read.valid() {
		return nil, ErrUnverified
	}
	s, e := New(b, current)
	if e != nil {
		return nil, e
	}
	s.read = read
	return s, nil
}
func hasNUL(s string) bool       { return bytes.ContainsRune([]byte(s), 0) }
func (s *Session) State() string { s.mu.Lock(); defer s.mu.Unlock(); return s.state }
func (s *Session) check() error {
	if !s.current(clone(s.binding)) {
		return ErrIdentity
	}
	return nil
}
func (s *Session) fail(e error) error { s.state = "execution_uncertain"; return e }

// Event consumes one complete NDJSON frame. Raw provider/repository text is
// never included in errors or retained in Outcome. Unknown event kinds fail
// closed; tool execution and compaction need separately characterized contracts.
func (s *Session) Event(run string, generation int64, raw []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if run != s.binding.RunID || generation != s.binding.Generation {
		return ErrIdentity
	}
	if e := s.check(); e != nil {
		return s.fail(e)
	}
	if s.finished || s.state == "execution_uncertain" {
		return ErrProtocol
	}
	if s.pending != "" {
		return s.fail(ErrProtocol)
	}
	s.bytes += len(raw)
	s.frames++
	if s.bytes > 8<<20 || s.frames > 4096 {
		return s.fail(ErrProtocol)
	}
	var frame struct {
		Type     string
		Data     *string
		Tools    *[]string
		Commands *[]string
		Usage    json.RawMessage
	}
	if decode(raw, &frame) != nil {
		return s.fail(ErrProtocol)
	}
	switch frame.Type {
	case "available_commands":
		if frame.Tools == nil || frame.Commands == nil || !sameTools(*frame.Tools, s.binding.Tools) || len(*frame.Commands) > 128 {
			return s.fail(ErrUnverified)
		}
		s.declared = true
		if s.cancelled {
			s.state = "cancelling"
		} else {
			s.state = "running"
		}
	case "text", "thought":
		if !s.declared || frame.Data == nil {
			return s.fail(ErrProtocol)
		}
	case "usage":
		if !s.declared || validUsage(frame.Usage, false) == nil {
			return s.fail(ErrProtocol)
		}
		// Informational, potentially repeated. Only the terminal aggregate is
		// recorded; adding this to end.usage would double-count the same turn.
	case "tool_call", "tool_call_update":
		if !s.declared || s.read == nil {
			return s.fail(ErrUnsupported)
		}
		if e := s.read.event(raw); e != nil {
			return s.fail(e)
		}
	case "end":
		if !s.declared {
			return s.fail(ErrProtocol)
		}
		if s.read != nil && !s.read.ready() && !s.cancelled {
			return s.fail(ErrUnverified)
		}
		if e := s.end(raw); e != nil {
			return s.fail(e)
		}
	case "error", "max_turns_reached":
		s.outcome.NativeReason = frame.Type
		s.pending = "failed"
		if s.cancelled {
			s.pending = "interrupted"
		}
	default:
		return s.fail(ErrUnsupported)
	}
	return nil
}
func sameTools(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]bool{}
	for _, t := range a {
		if seen[t] {
			return false
		}
		seen[t] = true
	}
	for _, t := range b {
		if !seen[t] {
			return false
		}
	}
	return true
}

type usage struct {
	Input     *int64 `json:"input_tokens"`
	Output    *int64 `json:"output_tokens"`
	Read      *int64 `json:"cache_read_input_tokens"`
	Create    *int64 `json:"cache_creation_input_tokens"`
	Reasoning *int64 `json:"reasoning_tokens"`
	Total     *int64 `json:"total_tokens"`
}

func validUsage(raw []byte, total bool) *usage {
	var u usage
	if decode(raw, &u) != nil {
		return nil
	}
	for _, v := range []*int64{u.Input, u.Output, u.Read, u.Create, u.Reasoning} {
		if v == nil || *v < 0 || *v > 1e9 {
			return nil
		}
	}
	// Cached/reasoning accounting has not yet been characterized on this
	// pinned version; refuse it rather than inventing total-token semantics.
	if *u.Read != 0 || *u.Create != 0 || *u.Reasoning != 0 {
		return nil
	}
	if total && (u.Total == nil || *u.Total != *u.Input+*u.Output) {
		return nil
	}
	return &u
}
func (s *Session) end(raw []byte) error {
	var r struct {
		SessionID string `json:"sessionId"`
		RequestID string `json:"requestId"`
		Reason    string `json:"stopReason"`
		Turns     *int64 `json:"num_turns"`
		Usage     json.RawMessage
		Models    map[string]struct {
			Input  *int64 `json:"inputTokens"`
			Output *int64 `json:"outputTokens"`
			Read   *int64 `json:"cacheReadInputTokens"`
			Create *int64 `json:"cacheCreationInputTokens"`
			Calls  *int64 `json:"modelCalls"`
		} `json:"modelUsage"`
	}
	if decode(raw, &r) != nil || r.SessionID != s.binding.NativeSessionID || !uuid.MatchString(r.RequestID) || r.Reason != "end_turn" || r.Turns == nil || *r.Turns < 1 || *r.Turns > s.binding.MaxTurns {
		return ErrProtocol
	}
	u := validUsage(r.Usage, true)
	if u == nil {
		return ErrProtocol
	}
	if len(r.Models) != 1 {
		return ErrUnverified
	}
	m, ok := r.Models[s.binding.Model]
	if !ok || m.Input == nil || m.Output == nil || m.Read == nil || m.Create == nil || m.Calls == nil || *m.Calls < 1 || *m.Calls > s.binding.MaxTurns || *m.Input != *u.Input || *m.Output != *u.Output || *m.Read != 0 || *m.Create != 0 {
		return ErrUnverified
	}
	s.outcome.RequestID = r.RequestID
	s.outcome.NativeReason = r.Reason
	s.outcome.NativeModel = s.binding.Model
	s.outcome.InputTokens = *u.Input
	s.outcome.OutputTokens = *u.Output
	s.outcome.NativeTurns = *r.Turns
	s.outcome.NativeModelCalls = *m.Calls
	s.pending = "succeeded"
	if s.cancelled {
		s.pending = "interrupted"
	}
	return nil
}

// Cancel records intent only. It neither kills a process nor supplies a trusted
// stop proof; the managed Worker owns cancellation and reservation release.
func (s *Session) Cancel() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.check(); e != nil {
		return s.fail(e)
	}
	if s.finished {
		return nil
	}
	s.cancelled = true
	if s.pending == "succeeded" {
		s.pending = "interrupted"
	}
	if s.pending == "" && s.state != "execution_uncertain" {
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
	if s.state == "execution_uncertain" || s.pending == "" || s.pending == "succeeded" && (exitCode != 0 || s.read != nil && !s.read.ready()) {
		s.state = "execution_uncertain"
	} else {
		s.state = s.pending
	}
	s.outcome.State = s.state
	return s.outcome, nil
}
func (*Session) Resume(Binding) error { return ErrUnsupported }

// Reject ambiguous keys, invalid UTF-8/NUL, oversized/deep payloads and trailing
// values. Unknown informational fields remain extensions of known frames.
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
	if text, ok := t.(string); ok && hasNUL(text) {
		return ErrProtocol
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
			if !ok || hasNUL(name) {
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
			if e := unique(d, depth+1); e != nil {
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

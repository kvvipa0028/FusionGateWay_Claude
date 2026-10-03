package grok

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// ReadTools is controller-owned, per-run state. It authorizes a text read
// before Native receives the instruction and correlates the resulting trace
// and HTTP history. It does not supply a Worker sandbox or admission proof.
type ReadTools struct {
	mu             sync.Mutex
	binding        Binding
	current        func(Binding) bool
	root           *os.Root
	rootInfo       os.FileInfo
	records        map[string]*readRecord
	pending        string
	bytes          int
	closed, failed bool
}
type readCall struct {
	ID, Name, Arguments string
	typed               bool
}
type readRecord struct {
	call                                readCall
	argument, path, relative, raw, text string
	info                                os.FileInfo
	started, located, done, continued   bool
}

func (*ReadTools) String() string { return "Grok read scope (private snapshots omitted)" }
func NewReadTools(b Binding, current func(Binding) bool) (*ReadTools, error) {
	if !readPlatformSupported {
		return nil, ErrUnsupported
	}
	if _, e := New(b, current); e != nil {
		return nil, e
	}
	if len(b.Tools) != 1 || b.Tools[0] != "read_file" {
		return nil, ErrUnsupported
	}
	canonical, e := filepath.EvalSymlinks(b.Cwd)
	if e != nil || canonical != b.Cwd || !current(clone(b)) {
		return nil, ErrIdentity
	}
	root, e := os.OpenRoot(b.Cwd)
	if e != nil {
		return nil, ErrUnverified
	}
	info, e := root.Stat(".")
	if e != nil || !info.IsDir() {
		root.Close()
		return nil, ErrUnverified
	}
	return &ReadTools{binding: clone(b), current: current, root: root, rootInfo: info, records: map[string]*readRecord{}}, nil
}
func (r *ReadTools) matches(b Binding) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.binding
	return !r.closed && a.RunID == b.RunID && a.Generation == b.Generation && a.Role == b.Role && a.NativeSessionID == b.NativeSessionID && a.Cwd == b.Cwd && a.Model == b.Model && a.RuntimeVersion == b.RuntimeVersion && a.ExecutableSHA256 == b.ExecutableSHA256 && a.MaxTurns == b.MaxTurns && sameTools(a.Tools, b.Tools)
}
func (r *ReadTools) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	return r.root.Close()
}
func (r *ReadTools) fail() { r.mu.Lock(); defer r.mu.Unlock(); r.failed = true }
func (r *ReadTools) check() error {
	if r.closed || r.failed || !r.current(clone(r.binding)) {
		return ErrIdentity
	}
	path, e := filepath.EvalSymlinks(r.binding.Cwd)
	if e != nil || path != r.binding.Cwd {
		return ErrIdentity
	}
	info, e := os.Stat(path)
	if e != nil || !os.SameFile(info, r.rootInfo) {
		return ErrIdentity
	}
	for _, record := range r.records {
		raw, info, e := r.file(record.relative)
		if e != nil || !sameReadFile(info, record.info) || string(raw) != record.raw {
			return ErrIdentity
		}
	}
	return nil
}
func (r *ReadTools) valid() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.check() == nil }
func (r *ReadTools) ready() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.check() != nil || r.pending != "" {
		return false
	}
	for _, v := range r.records {
		if !v.done || !v.continued {
			return false
		}
	}
	return true
}
func sameReadFile(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.Mode() == b.Mode() && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}
func (r *ReadTools) file(relative string) ([]byte, os.FileInfo, error) {
	// Reject links at every component as well as hard-linked regular files.
	// Root.Open also constrains resolution to the pinned directory descriptor.
	parts := strings.Split(relative, string(filepath.Separator))
	for i := range parts {
		st, e := r.root.Lstat(filepath.Join(parts[:i+1]...))
		if e != nil || st.Mode()&os.ModeSymlink != 0 {
			return nil, nil, ErrUnverified
		}
		if i < len(parts)-1 && !st.IsDir() {
			return nil, nil, ErrUnverified
		}
	}
	f, e := r.root.Open(relative)
	if e != nil {
		return nil, nil, ErrUnverified
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || !singleReadLink(st) || st.Size() > 65536 {
		return nil, nil, ErrUnverified
	}
	raw, e := io.ReadAll(io.LimitReader(f, 65537))
	if e != nil || len(raw) > 65536 || !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 || bytes.Count(raw, []byte("\n"))+1 > 1000 {
		return nil, nil, ErrUnverified
	}
	after, e := f.Stat()
	if e != nil || !sameReadFile(st, after) {
		return nil, nil, ErrUnverified
	}
	onPath, e := r.root.Lstat(relative)
	if e != nil || !sameReadFile(st, onPath) {
		return nil, nil, ErrUnverified
	}
	return raw, st, nil
}
func readArgument(raw []byte) (string, bool) {
	f, ok := object(raw, "target_file")
	if !ok || len(f) != 1 {
		return "", false
	}
	var path string
	if json.Unmarshal(f["target_file"], &path) != nil || path == "" || !utf8.ValidString(path) || hasNUL(path) || filepath.Clean(path) != path || strings.Contains(path, "://") {
		return "", false
	}
	return path, true
}
func readText(raw string) string {
	lines := strings.Split(raw, "\n")
	for i := range lines {
		// The terminal empty line preserves the newline, not another number.
		if i == len(lines)-1 && lines[i] == "" {
			continue
		}
		if i == 0 || (i+1)%10 == 0 {
			lines[i] = strconv.Itoa(i+1) + "→" + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}
func (r *ReadTools) snapshot(call readCall, markers [][]byte) (*readRecord, error) {
	if call.Name != "read_file" || !toolID.MatchString(call.ID) || r.records[call.ID] != nil || len(r.records) >= 64 || int64(len(r.records)) >= r.binding.MaxTurns {
		return nil, ErrUnverified
	}
	argument, ok := readArgument([]byte(call.Arguments))
	if !ok {
		return nil, ErrUnverified
	}
	path := argument
	if !filepath.IsAbs(path) {
		path = filepath.Join(r.binding.Cwd, path)
	}
	relative, e := filepath.Rel(r.binding.Cwd, path)
	if e != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return nil, ErrUnverified
	}
	raw, st, e := r.file(relative)
	if e != nil {
		return nil, e
	}
	if r.bytes+len(raw) > 512<<10 {
		return nil, ErrUnverified
	}
	for _, secret := range markers {
		if len(secret) == 0 || len(secret) > 4096 || (bytes.Contains(raw, secret) || privateJSON(raw, secret)) {
			return nil, ErrUnverified
		}
	}
	return &readRecord{call: call, argument: argument, path: path, relative: relative, raw: string(raw), text: readText(string(raw)), info: st}, nil
}

// messages validates every historical pair, including retries, without
// consuming a new tool grant. A pending result is mandatory on the next main
// request; auxiliary title requests neither acknowledge nor execute tools.
func (r *ReadTools) messages(messages []json.RawMessage, title bool) (map[string]bool, error) {
	seen := map[string]bool{}
	waiting := ""
	for _, raw := range messages {
		f, ok := object(raw, "role", "content", "tool_calls", "tool_call_id", "model_id")
		if !ok {
			return nil, ErrProtocol
		}
		var role string
		if json.Unmarshal(f["role"], &role) != nil {
			return nil, ErrProtocol
		}
		if value, ok := f["model_id"]; ok {
			var model string
			if role != "assistant" || json.Unmarshal(value, &model) != nil || model != r.binding.Model {
				return nil, ErrUnverified
			}
		}
		calls, hasCalls := f["tool_calls"]
		id, hasID := f["tool_call_id"]
		if waiting != "" && role != "tool" {
			return nil, ErrProtocol
		}
		if hasCalls {
			if title || role != "assistant" || hasID || waiting != "" {
				return nil, ErrUnverified
			}
			var list []json.RawMessage
			if decode(calls, &list) != nil || len(list) != 1 {
				return nil, ErrUnverified
			}
			c, ok := object(list[0], "id", "type", "function")
			var name, kind, callID, args string
			if !ok || json.Unmarshal(c["id"], &callID) != nil || json.Unmarshal(c["type"], &kind) != nil || kind != "function" {
				return nil, ErrProtocol
			}
			fn, ok := object(c["function"], "name", "arguments")
			if !ok || json.Unmarshal(fn["name"], &name) != nil || json.Unmarshal(fn["arguments"], &args) != nil {
				return nil, ErrProtocol
			}
			record := r.records[callID]
			arg, ok := readArgument([]byte(args))
			if record == nil || seen[callID] || !ok || name != record.call.Name || arg != record.argument {
				return nil, ErrUnverified
			}
			if value, ok := f["content"]; ok && !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				var text string
				if json.Unmarshal(value, &text) != nil {
					return nil, ErrProtocol
				}
			}
			waiting = callID
		} else if role == "tool" {
			var callID, text string
			if title || waiting == "" || !hasID || json.Unmarshal(id, &callID) != nil || callID != waiting || json.Unmarshal(f["content"], &text) != nil || text != r.records[callID].text {
				return nil, ErrUnverified
			}
			seen[callID] = true
			waiting = ""
		} else {
			var text string
			if hasID || (role != "system" && role != "user" && role != "assistant") || json.Unmarshal(f["content"], &text) != nil || bytes.Equal(bytes.TrimSpace(f["content"]), []byte("null")) {
				return nil, ErrProtocol
			}
		}
	}
	if waiting != "" || !title && r.pending != "" && !seen[r.pending] {
		return nil, ErrUnverified
	}
	return seen, nil
}
func (r *ReadTools) request(messages []json.RawMessage, title bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.check() != nil {
		return false
	}
	_, e := r.messages(messages, title)
	return e == nil
}
func (r *ReadTools) response(messages []json.RawMessage, title bool, call *readCall, markers [][]byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e := r.check(); e != nil {
		return e
	}
	seen, e := r.messages(messages, title)
	if e != nil {
		return e
	}
	var next *readRecord
	if call != nil {
		if title {
			return ErrUnverified
		}
		next, e = r.snapshot(*call, markers)
		if e != nil {
			return e
		}
	}
	if !title {
		for id := range seen {
			r.records[id].continued = true
		}
		r.pending = ""
	}
	if next != nil {
		r.records[next.call.ID] = next
		r.pending = next.call.ID
		r.bytes += len(next.raw)
	}
	return nil
}
func (r *ReadTools) event(raw []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e := r.check(); e != nil {
		r.failed = true
		return e
	}
	var frame struct {
		Type               string
		ID                 string `json:"toolCallId"`
		Name               string `json:"toolName"`
		Title, Kind        string
		Status             *string
		Input              json.RawMessage `json:"rawInput"`
		Output             json.RawMessage `json:"rawOutput"`
		Content, Locations []json.RawMessage
	}
	if decode(raw, &frame) != nil {
		r.failed = true
		return ErrProtocol
	}
	record := r.records[frame.ID]
	if record == nil || record.done {
		r.failed = true
		return ErrUnverified
	}
	bad := func() error { r.failed = true; return ErrUnverified }
	if frame.Type == "tool_call" {
		if _, ok := object(raw, "type", "toolCallId", "toolName", "title", "kind", "status", "rawInput", "content", "locations"); !ok {
			return bad()
		}
		arg, ok := readArgument(frame.Input)
		if !ok || arg != record.argument || record.started || frame.Name != "read_file" || frame.Title != "read_file" || frame.Kind != "read" || frame.Status == nil || *frame.Status != "pending" || frame.Content == nil || len(frame.Content) != 0 || frame.Locations == nil || len(frame.Locations) != 0 {
			return bad()
		}
		record.started = true
		return nil
	}
	if frame.Type != "tool_call_update" || !record.started {
		return bad()
	}
	if _, ok := object(raw, "type", "toolCallId", "status", "content", "rawOutput", "locations"); !ok {
		return bad()
	}
	if frame.Status == nil {
		if record.located || frame.Content == nil || len(frame.Content) != 0 || !bytes.Equal(bytes.TrimSpace(frame.Output), []byte("null")) || len(frame.Locations) != 1 {
			return bad()
		}
		loc, ok := object(frame.Locations[0], "path")
		var path string
		if !ok || json.Unmarshal(loc["path"], &path) != nil || path != record.path {
			return bad()
		}
		record.located = true
		return nil
	}
	if *frame.Status != "completed" || !record.located || frame.Locations == nil || len(frame.Locations) != 0 || len(frame.Content) != 1 {
		return bad()
	}
	content, ok := object(frame.Content[0], "type", "content")
	var kind, text string
	if !ok || json.Unmarshal(content["type"], &kind) != nil || kind != "content" {
		return bad()
	}
	inner, ok := object(content["content"], "type", "text")
	if !ok || json.Unmarshal(inner["type"], &kind) != nil || kind != "text" || json.Unmarshal(inner["text"], &text) != nil || text != record.text {
		return bad()
	}
	output, ok := object(frame.Output, "type", "FileContent")
	if !ok || json.Unmarshal(output["type"], &kind) != nil || kind != "ReadFile" {
		return bad()
	}
	file, ok := object(output["FileContent"], "content", "content_concise", "absolute_path", "offset", "raw_output", "total_lines")
	if !ok {
		return bad()
	}
	var data struct {
		Content string
		Concise string `json:"content_concise"`
		Path    string `json:"absolute_path"`
		Offset  json.RawMessage
		Raw     string `json:"raw_output"`
		Lines   int    `json:"total_lines"`
	}
	if decode(output["FileContent"], &data) != nil || data.Content != record.text || data.Concise != record.text || data.Raw != record.raw || data.Path != record.path || data.Lines != strings.Count(record.raw, "\n")+1 || !bytes.Equal(bytes.TrimSpace(file["offset"]), []byte("null")) {
		return bad()
	}
	record.done = true
	return nil
}

// Check decoded strings too: JSON escaping must not hide a credential from a
// request/response reflection check. Embedded argument JSON is bounded by the
// same depth limit. No secret or decoded text appears in the returned value.
func privateJSON(raw []byte, markers ...[]byte) bool {
	if len(markers) == 0 {
		return false
	}
	var value any
	if decode(raw, &value) != nil {
		return false
	}
	return privateValue(value, markers, 0)
}
func privateValue(value any, markers [][]byte, depth int) bool {
	if depth > 32 {
		return true
	}
	switch v := value.(type) {
	case string:
		for _, marker := range markers {
			if len(marker) > 0 && strings.Contains(v, string(marker)) {
				return true
			}
		}
		if json.Valid([]byte(v)) {
			var nested any
			if json.Unmarshal([]byte(v), &nested) == nil {
				return privateValue(nested, markers, depth+1)
			}
		}
	case []any:
		for _, child := range v {
			if privateValue(child, markers, depth+1) {
				return true
			}
		}
	case map[string]any:
		for key, child := range v {
			if privateValue(key, markers, depth+1) || privateValue(child, markers, depth+1) {
				return true
			}
		}
	}
	return false
}

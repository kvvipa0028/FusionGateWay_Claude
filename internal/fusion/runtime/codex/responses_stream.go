package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"unicode/utf8"
)

// Validate a complete text-only Responses stream before any bytes escape. Tool
// calls, errors, incomplete terminals, summaries, new event kinds and ambiguous
// framing fail closed. ReportedModel is independently checked by CallGate.
func responsesStream(raw []byte, model string, markers ...[]byte) bool {
	if !utf8.Valid(raw) || len(raw) == 0 || len(raw) > 8<<20 || !bytes.HasSuffix(raw, []byte("\n\n")) && !bytes.HasSuffix(raw, []byte("\r\n\r\n")) {
		return false
	}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var data []string
	event := ""
	id := ""
	started, done := false, false
	frames := 0
	lastSequence := int64(-1)
	outputs := []json.RawMessage{}
	seen := map[string]bool{}
	messages := 0
	toolCalls := 0
	added := map[int]string{}
	addedKinds := map[int]string{}
	deltas := map[string]string{}
	textDone := map[string]bool{}
	consume := func() bool {
		if len(data) == 0 {
			return event == ""
		}
		if done {
			return false
		}
		payload := []byte(strings.Join(data, "\n"))
		data = nil
		fields, ok := object(payload, "type", "sequence_number", "response", "output_index", "item", "item_id", "content_index", "part", "delta", "text", "logprobs")
		if !ok || privateJSON(payload, markers...) {
			return false
		}
		kind, ok := stringField(fields, "type")
		if !ok || event != "" && event != kind {
			return false
		}
		event = ""
		frames++
		if frames > 4096 {
			return false
		}
		if v, exists := fields["sequence_number"]; exists {
			var n int64
			if json.Unmarshal(v, &n) != nil || bytes.Equal(bytes.TrimSpace(v), []byte("null")) || n < 0 || n <= lastSequence {
				return false
			}
			lastSequence = n
		}
		if kind == "response.created" {
			if started || len(fields["response"]) == 0 {
				return false
			}
			r, ok := responseObject(fields["response"], model, "in_progress")
			if !ok {
				return false
			}
			id, ok = stringField(r, "id")
			a, valid := array(r["output"])
			if !ok || !gateID.MatchString(id) || !valid || len(a) != 0 {
				return false
			}
			started = true
			return envelopeKeys(fields, "type", "sequence_number", "response")
		}
		if !started {
			return false
		}
		switch kind {
		case "response.in_progress":
			if !envelopeKeys(fields, "type", "sequence_number", "response") {
				return false
			}
			r, ok := responseObject(fields["response"], model, "in_progress")
			rid, valid := stringField(r, "id")
			a, outOK := array(r["output"])
			return ok && valid && rid == id && outOK && len(a) == 0 && len(outputs) == 0
		case "response.output_item.added", "response.output_item.done":
			if !envelopeKeys(fields, "type", "sequence_number", "output_index", "item") {
				return false
			}
			idx, ok := index(fields, "output_index")
			if !ok || idx != len(outputs) {
				return false
			}
			item, itemID, kind, ok := outputItem(fields["item"], kind == "response.output_item.done")
			if !ok {
				return false
			}
			if fieldsKind, _ := stringField(fields, "type"); fieldsKind == "response.output_item.added" {
				if _, exists := added[idx]; exists || seen[itemID] {
					return false
				}
				added[idx] = itemID
				addedKinds[idx] = kind
				return true
			}
			if seen[itemID] {
				return false
			}
			if previous, exists := added[idx]; exists && (previous != itemID || addedKinds[idx] != kind) {
				return false
			}
			if kind == "message" {
				messages++
				content, _ := array(item["content"])
				part, _ := object(content[0], "type", "text", "annotations", "logprobs")
				text, _ := stringField(part, "text")
				if delta, exists := deltas[itemID]; exists && delta != text {
					return false
				}
			}
			if kind == "function_call" || kind == "custom_tool_call" {
				toolCalls++
			}
			delete(added, idx)
			delete(addedKinds, idx)
			seen[itemID] = true
			outputs = append(outputs, fields["item"])
			return true
		case "response.output_text.delta", "response.output_text.done", "response.content_part.added", "response.content_part.done":
			idx, ok := index(fields, "output_index")
			ci, valid := index(fields, "content_index")
			itemID, idOK := stringField(fields, "item_id")
			if !ok || !valid || ci != 0 || !idOK || idx != len(outputs) || added[idx] != itemID || addedKinds[idx] != "message" {
				return false
			}
			switch kind {
			case "response.output_text.delta":
				if !envelopeKeys(fields, "type", "sequence_number", "output_index", "content_index", "item_id", "delta", "logprobs") || textDone[itemID] {
					return false
				}
				s, ok := stringField(fields, "delta")
				if !ok {
					return false
				}
				deltas[itemID] += s
				if len(deltas[itemID]) > 1<<20 {
					return false
				}
			case "response.output_text.done":
				if !envelopeKeys(fields, "type", "sequence_number", "output_index", "content_index", "item_id", "text", "logprobs") || textDone[itemID] {
					return false
				}
				s, ok := stringField(fields, "text")
				if !ok || deltas[itemID] != s {
					return false
				}
				textDone[itemID] = true
			default:
				if !envelopeKeys(fields, "type", "sequence_number", "output_index", "content_index", "item_id", "part") {
					return false
				}
				x, ok := object(fields["part"], "type", "text", "annotations", "logprobs")
				typ, valid := stringField(x, "type")
				s, textOK := stringField(x, "text")
				if !ok || !valid || !textOK || typ != "output_text" || !emptyOptionalArrays(x, "annotations", "logprobs") {
					return false
				}
				if kind == "response.content_part.added" && s != "" || kind == "response.content_part.done" && deltas[itemID] != s {
					return false
				}
			}
			return emptyOptionalArrays(fields, "logprobs")
		case "response.completed":
			// A writing turn may answer with tool calls only; an empty
			// response (no message and no tool call) is still rejected.
			if !envelopeKeys(fields, "type", "sequence_number", "response") || messages+toolCalls < 1 || len(added) != 0 {
				return false
			}
			r, ok := responseObject(fields["response"], model, "completed")
			rid, valid := stringField(r, "id")
			out, outOK := array(r["output"])
			if !ok || !valid || rid != id || !outOK || len(out) != len(outputs) || !usage(r["usage"]) {
				return false
			}
			for i, v := range out {
				var a, b any
				if decode(v, &a) != nil || decode(outputs[i], &b) != nil {
					return false
				}
				x, _ := json.Marshal(a)
				y, _ := json.Marshal(b)
				if !bytes.Equal(x, y) {
					return false
				}
			}
			done = true
			return true
		default:
			return false
		}
	}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
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
		switch {
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		case strings.HasPrefix(line, "event:"):
			if event != "" {
				return false
			}
			event = strings.TrimPrefix(strings.TrimPrefix(line, "event:"), " ")
			if event == "" {
				return false
			}
		default:
			return false
		}
	}
	return scanner.Err() == nil && len(data) == 0 && event == "" && started && done
}
func envelopeKeys(m map[string]json.RawMessage, allowed ...string) bool {
	for k := range m {
		found := false
		for _, a := range allowed {
			found = found || k == a
		}
		if !found {
			return false
		}
	}
	return true
}
func index(m map[string]json.RawMessage, k string) (int, bool) {
	var i int
	e := json.Unmarshal(m[k], &i)
	return i, e == nil && !bytes.Equal(bytes.TrimSpace(m[k]), []byte("null")) && i >= 0 && i < 128
}
func emptyOptionalArrays(m map[string]json.RawMessage, keys ...string) bool {
	for _, k := range keys {
		if v, exists := m[k]; exists {
			a, ok := array(v)
			if !ok || len(a) != 0 {
				return false
			}
		}
	}
	return true
}
func outputItem(raw []byte, terminal bool) (map[string]json.RawMessage, string, string, bool) {
	var v struct{ Type string }
	if decode(raw, &v) != nil {
		return nil, "", "", false
	}
	if v.Type == "reasoning" {
		if !reasoningItem(raw) {
			return nil, "", "", false
		}
		var m map[string]json.RawMessage
		_ = json.Unmarshal(raw, &m)
		id, _ := stringField(m, "id")
		return m, id, v.Type, true
	}
	if v.Type == "function_call" || v.Type == "custom_tool_call" {
		if !toolCallItem(raw, v.Type, terminal) {
			return nil, "", "", false
		}
		var m map[string]json.RawMessage
		_ = json.Unmarshal(raw, &m)
		id, _ := stringField(m, "id")
		return m, id, v.Type, true
	}
	m, ok := object(raw, "type", "id", "status", "role", "content")
	if !ok || v.Type != "message" {
		return nil, "", "", false
	}
	id, idOK := stringField(m, "id")
	role, roleOK := stringField(m, "role")
	status, statusOK := stringField(m, "status")
	content, contentOK := array(m["content"])
	if !idOK || !gateID.MatchString(id) || !roleOK || role != "assistant" || !statusOK || !contentOK {
		return nil, "", "", false
	}
	if !terminal {
		return m, id, v.Type, status == "in_progress" && len(content) == 0
	}
	if status != "completed" || len(content) != 1 {
		return nil, "", "", false
	}
	p, ok := object(content[0], "type", "text", "annotations", "logprobs")
	typ, valid := stringField(p, "type")
	_, textOK := stringField(p, "text")
	return m, id, v.Type, ok && valid && typ == "output_text" && textOK && emptyOptionalArrays(p, "annotations", "logprobs")
}
func responseObject(raw []byte, model, status string) (map[string]json.RawMessage, bool) {
	m, ok := object(raw, "id", "object", "created_at", "status", "model", "headers", "output", "usage", "error", "incomplete_details", "instructions", "max_output_tokens", "parallel_tool_calls", "previous_response_id", "reasoning", "store", "temperature", "text", "tool_choice", "tools", "top_p", "truncation", "metadata", "service_tier", "user", "background", "safety_identifier", "prompt_cache_key", "prompt_cache_retention", "usage_metadata", "end_turn")
	if !ok {
		return nil, false
	}
	s, valid := stringField(m, "status")
	if !valid || s != status {
		return nil, false
	}
	if _, exists := m["model"]; exists {
		v, ok := stringField(m, "model")
		if !ok || v != model {
			return nil, false
		}
	}
	if headers, exists := m["headers"]; exists {
		reported, ok := sseReportedModel(headers)
		if !ok || reported != model {
			return nil, false
		}
	}
	for _, k := range []string{"error", "incomplete_details"} {
		if v, exists := m[k]; exists && !bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return nil, false
		}
	}
	if v, exists := m["tools"]; exists {
		a, ok := array(v)
		if !ok || len(a) != 0 {
			return nil, false
		}
	}
	if v, exists := m["background"]; exists {
		var b bool
		if json.Unmarshal(v, &b) != nil || b || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return nil, false
		}
	}
	return m, true
}
func usage(raw []byte) bool {
	m, ok := object(raw, "input_tokens", "output_tokens", "total_tokens", "input_tokens_details", "output_tokens_details", "codex_rollout_budget_units")
	if !ok {
		return false
	}
	n := map[string]int64{}
	for _, k := range []string{"input_tokens", "output_tokens", "total_tokens"} {
		var x int64
		if json.Unmarshal(m[k], &x) != nil || x < 0 || bytes.Equal(bytes.TrimSpace(m[k]), []byte("null")) {
			return false
		}
		n[k] = x
	}
	if n["input_tokens"] > math.MaxInt64-n["output_tokens"] || n["total_tokens"] != n["input_tokens"]+n["output_tokens"] {
		return false
	}
	for _, group := range []struct {
		k      string
		limit  int64
		fields []string
	}{{"input_tokens_details", n["input_tokens"], []string{"cached_tokens", "cache_write_tokens"}}, {"output_tokens_details", n["output_tokens"], []string{"reasoning_tokens"}}} {
		if v, exists := m[group.k]; exists {
			d, ok := object(v, group.fields...)
			if !ok {
				return false
			}
			for _, b := range d {
				var x int64
				if json.Unmarshal(b, &x) != nil || bytes.Equal(bytes.TrimSpace(b), []byte("null")) || x < 0 || x > group.limit {
					return false
				}
			}
		}
	}
	if v, exists := m["codex_rollout_budget_units"]; exists {
		var x float64
		if json.Unmarshal(v, &x) != nil || bytes.Equal(bytes.TrimSpace(v), []byte("null")) || x < 0 {
			return false
		}
	}
	return true
}

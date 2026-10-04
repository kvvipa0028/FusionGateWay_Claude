package codex

import (
	"bytes"
	"encoding/json"
)

type gatewayItem struct {
	kind, delta string
	complete    bool
}

func isNull(raw []byte) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }
func integer(raw []byte, nullable bool) bool {
	if nullable && isNull(raw) {
		return true
	}
	var n int64
	return len(raw) > 0 && !isNull(raw) && json.Unmarshal(raw, &n) == nil && n >= 0
}
func nullableString(raw []byte) bool {
	if isNull(raw) {
		return true
	}
	var s string
	return len(raw) > 0 && json.Unmarshal(raw, &s) == nil
}

// These observations never confer account/quota/tool authority. Unknown or
// contradictory execution notifications poison only the currently active turn.
func (c *Client) gatewayEvent(raw []byte) (err error) {
	if c.state != "running" && c.state != "cancelling" {
		return ErrProtocol
	}
	defer func() {
		if err != nil {
			c.state = "execution_uncertain"
		}
	}()
	envelope, ok := object(raw, "method", "params", "emittedAtMs")
	if !ok {
		return ErrProtocol
	}
	if ts, exists := envelope["emittedAtMs"]; exists {
		var v int64
		if isNull(ts) || json.Unmarshal(ts, &v) != nil {
			return ErrProtocol
		}
	}
	method, ok := stringField(envelope, "method")
	if !ok {
		return ErrProtocol
	}
	var params map[string]json.RawMessage
	if decode(envelope["params"], &params) != nil || params == nil {
		return ErrProtocol
	}
	fields := map[string][]string{
		"remoteControl/status/changed": {"installationId", "serverName", "status", "environmentId"},
		"thread/started":               {"thread"},
		"warning":                      {"message", "threadId"},
		"account/rateLimits/updated":   {"rateLimits"},
		"thread/status/changed":        {"threadId", "status"},
		"turn/started":                 {"threadId", "turn"},
		"turn/completed":               {"threadId", "turn"},
		"item/started":                 {"threadId", "turnId", "item", "startedAtMs"},
		"item/completed":               {"threadId", "turnId", "item", "completedAtMs"},
		"item/agentMessage/delta":      {"threadId", "turnId", "itemId", "delta"},
		"thread/tokenUsage/updated":    {"threadId", "turnId", "tokenUsage"},
		"error":                        {"threadId", "turnId", "error", "willRetry"},
	}
	allowed, known := fields[method]
	if !known {
		return ErrUnverified
	}
	params, ok = object(envelope["params"], allowed...)
	if !ok {
		return ErrProtocol
	}
	if x, exists := params["threadId"]; exists && !(method == "warning" && isNull(x)) {
		id, valid := stringField(params, "threadId")
		if !valid || id != c.threadID {
			return ErrIdentity
		}
	}
	if _, exists := params["turnId"]; exists {
		id, valid := stringField(params, "turnId")
		if !valid || id != c.turnID {
			return ErrIdentity
		}
	}
	switch method {
	case "remoteControl/status/changed":
		status, ok := stringField(params, "status")
		if !ok || status != "disabled" {
			return ErrUnverified
		}
		for _, k := range []string{"installationId", "serverName"} {
			s, ok := stringField(params, k)
			if !ok || len(s) > 4096 {
				return ErrProtocol
			}
		}
		if x, ok := params["environmentId"]; ok && !nullableString(x) {
			return ErrProtocol
		}
	case "thread/started":
		var out struct {
			ID, CLIVersion, Cwd, ModelProvider string
			Model, ReasoningEffort             *string
		}
		if decode(params["thread"], &out) != nil || out.ID != c.threadID || out.CLIVersion != CLIVersion || out.Cwd != c.binding.Cwd || out.ModelProvider != StageProvider || out.Model != nil && *out.Model != c.binding.Target.ResolvedModel || out.ReasoningEffort != nil && *out.ReasoningEffort != *c.binding.Target.Effort.Value {
			return ErrIdentity
		}
	case "warning":
		if _, ok := stringField(params, "message"); !ok {
			return ErrProtocol
		}
	case "account/rateLimits/updated":
		var v map[string]json.RawMessage
		if decode(params["rateLimits"], &v) != nil || v == nil {
			return ErrProtocol
		}
	case "thread/status/changed":
		if _, ok := params["threadId"]; !ok {
			return ErrProtocol
		}
		status, ok := object(params["status"], "type", "activeFlags")
		typ, valid := stringField(status, "type")
		if !ok || !valid || (typ != "idle" && typ != "active" && typ != "systemError") {
			return ErrProtocol
		}
		if typ == "active" {
			flags, ok := array(status["activeFlags"])
			if !ok || len(flags) != 0 {
				return ErrUnverified
			}
		} else if _, exists := status["activeFlags"]; exists {
			return ErrProtocol
		}
	case "turn/started", "turn/completed":
		if _, ok := params["threadId"]; !ok {
			return ErrProtocol
		}
		turn, ok := object(params["turn"], "id", "items", "status", "error", "startedAt", "completedAt", "durationMs", "itemsView")
		id, idOK := stringField(turn, "id")
		status, statusOK := stringField(turn, "status")
		items, itemsOK := array(turn["items"])
		if !ok || !idOK || id != c.turnID || !statusOK || !itemsOK || len(items) > 128 {
			return ErrProtocol
		}
		for _, k := range []string{"startedAt", "completedAt", "durationMs"} {
			if v, exists := turn[k]; exists && !integer(v, true) {
				return ErrProtocol
			}
		}
		for _, item := range items {
			id, kind, ok := c.gatewayItem(item)
			if !ok {
				return ErrUnverified
			}
			if method == "turn/completed" {
				seen, exists := c.gatewayItems[id]
				if !exists || !seen.complete || seen.kind != kind {
					return ErrProtocol
				}
			}
		}
		if method == "turn/started" {
			if c.gatewayStarted || status != "inProgress" || len(items) != 0 || len(turn["error"]) > 0 && !isNull(turn["error"]) {
				return ErrProtocol
			}
			c.gatewayStarted = true
			return nil
		}
		if !c.gatewayStarted {
			return ErrProtocol
		}
		for _, item := range c.gatewayItems {
			if !item.complete {
				return ErrProtocol
			}
		}
		switch status {
		case "completed":
			if c.gatewayFatal || len(turn["error"]) > 0 && !isNull(turn["error"]) {
				return ErrProtocol
			}
			if c.state == "cancelling" {
				c.state = "interrupted"
			} else {
				c.state = "succeeded"
			}
		case "failed":
			if !gatewayError(turn["error"]) {
				return ErrProtocol
			}
			c.state = "failed"
		case "interrupted":
			c.state = "interrupted"
		default:
			return ErrProtocol
		}
	case "item/started", "item/completed":
		if !c.gatewayStarted || len(params["threadId"]) == 0 || len(params["turnId"]) == 0 {
			return ErrProtocol
		}
		timing := "startedAtMs"
		if method == "item/completed" {
			timing = "completedAtMs"
		}
		if !integer(params[timing], true) {
			return ErrProtocol
		}
		id, kind, ok := c.gatewayItem(params["item"])
		if !ok {
			return ErrUnverified
		}
		item, exists := c.gatewayItems[id]
		if method == "item/started" {
			if exists || len(c.gatewayItems) >= 128 {
				return ErrProtocol
			}
			if c.gatewayItems == nil {
				c.gatewayItems = map[string]gatewayItem{}
			}
			c.gatewayItems[id] = gatewayItem{kind: kind}
		} else {
			if !exists || item.complete || kind != item.kind {
				return ErrProtocol
			}
			if kind == "agentMessage" && item.delta != "" {
				var out struct{ Text string }
				if decode(params["item"], &out) != nil || out.Text != item.delta {
					return ErrProtocol
				}
			}
			item.complete = true
			c.gatewayItems[id] = item
		}
	case "item/agentMessage/delta":
		if len(params["threadId"]) == 0 || len(params["turnId"]) == 0 {
			return ErrProtocol
		}
		id, idOK := stringField(params, "itemId")
		delta, ok := stringField(params, "delta")
		item, exists := c.gatewayItems[id]
		if !idOK || !ok || !exists || item.complete || item.kind != "agentMessage" || len(item.delta)+len(delta) > 1<<20 {
			return ErrProtocol
		}
		item.delta += delta
		c.gatewayItems[id] = item
	case "thread/tokenUsage/updated":
		if len(params["threadId"]) == 0 || len(params["turnId"]) == 0 {
			return ErrProtocol
		}
		usage, ok := object(params["tokenUsage"], "total", "last", "modelContextWindow")
		if !ok {
			return ErrProtocol
		}
		for _, k := range []string{"total", "last"} {
			b, ok := object(usage[k], "inputTokens", "cachedInputTokens", "cacheWriteInputTokens", "outputTokens", "reasoningOutputTokens", "totalTokens")
			if !ok {
				return ErrProtocol
			}
			for _, field := range []string{"inputTokens", "cachedInputTokens", "outputTokens", "reasoningOutputTokens", "totalTokens"} {
				if !integer(b[field], false) {
					return ErrProtocol
				}
			}
			if x, exists := b["cacheWriteInputTokens"]; exists && !integer(x, false) {
				return ErrProtocol
			}
		}
		if x, exists := usage["modelContextWindow"]; exists && !integer(x, true) {
			return ErrProtocol
		}
	case "error":
		if len(params["threadId"]) == 0 || len(params["turnId"]) == 0 || !gatewayError(params["error"]) {
			return ErrProtocol
		}
		var retry bool
		if isNull(params["willRetry"]) || len(params["willRetry"]) == 0 || json.Unmarshal(params["willRetry"], &retry) != nil {
			return ErrProtocol
		}
		if !retry {
			c.gatewayFatal = true
		}
	}
	return nil
}
func gatewayError(raw []byte) bool {
	m, ok := object(raw, "message", "additionalDetails", "codexErrorInfo", "misalignment")
	_, valid := stringField(m, "message")
	return ok && valid
}
func (c *Client) gatewayItem(raw []byte) (string, string, bool) {
	var v struct{ ID, Type string }
	if decode(raw, &v) != nil || v.ID == "" || len(v.ID) > 4096 || !c.allowedItem(v.Type) {
		return "", "", false
	}
	var m map[string]json.RawMessage
	var ok bool
	switch v.Type {
	case "userMessage":
		m, ok = object(raw, "id", "type", "content", "clientId")
		a, valid := array(m["content"])
		ok = ok && valid && len(a) > 0 && len(a) <= 128
		for _, x := range a {
			f, valid := object(x, "type", "text", "text_elements")
			typ, typeOK := stringField(f, "type")
			_, textOK := stringField(f, "text")
			if !valid || !typeOK || typ != "text" || !textOK {
				return "", "", false
			}
			if x, exists := f["text_elements"]; exists {
				a, ok := array(x)
				if !ok || len(a) != 0 {
					return "", "", false
				}
			}
		}
	case "agentMessage":
		m, ok = object(raw, "id", "type", "text", "phase", "memoryCitation", "delivery", "questions")
		_, valid := stringField(m, "text")
		ok = ok && valid
		for _, k := range []string{"memoryCitation", "delivery", "questions"} {
			if x, exists := m[k]; exists && !isNull(x) {
				return "", "", false
			}
		}
		if x, exists := m["phase"]; exists && !isNull(x) {
			s, valid := stringField(m, "phase")
			if !valid || (s != "commentary" && s != "final_answer") {
				return "", "", false
			}
		}
	case "reasoning":
		m, ok = object(raw, "id", "type", "summary", "content")
		if x, exists := m["summary"]; exists {
			a, valid := array(x)
			if !valid || len(a) != 0 {
				return "", "", false
			}
		}
		if x, exists := m["content"]; exists {
			a, valid := array(x)
			if !valid || len(a) > 128 {
				return "", "", false
			}
			for _, x := range a {
				var s string
				if isNull(x) || json.Unmarshal(x, &s) != nil {
					return "", "", false
				}
			}
		}
	}
	return v.ID, v.Type, ok
}

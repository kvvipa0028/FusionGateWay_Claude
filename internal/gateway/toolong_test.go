package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/provider"
)

// A vendor's "too long" reaches the agent in its own API's words, so it
// compacts and retries: Claude Code on "prompt is too long", Codex and
// chat clients on context_length_exceeded.
func TestContextOverflowSaidTheClientsWay(t *testing.T) {
	volc := "Volcengine: Input exceeds the context limit (1048568 tokens)"
	for _, tc := range []struct {
		proto    provider.Protocol
		status   int
		msg      string
		wantCode string
		wantMsg  string
	}{
		{provider.Anthropic, 400, volc, "", "prompt is too long: " + volc},
		{provider.Chat, 400, volc, "context_length_exceeded", volc},
		{provider.Responses, 413, volc, "context_length_exceeded", volc},
		{provider.Responses, 400, "relay: This model's maximum context length is 131072 tokens. However, you requested 140000 tokens", "context_length_exceeded", ""},
		{provider.Anthropic, 400, "a: prompt is too long: 210000 tokens > 200000 maximum", "", "a: prompt is too long: 210000 tokens > 200000 maximum"},
		{provider.Chat, 400, "zhipu: 输入内容超过模型最大上下文长度", "context_length_exceeded", ""},
		// not the conversation: left as the vendor said it
		{provider.Chat, 400, "x: max_tokens is too large: 100000 exceeds the model's context", "", ""},
		{provider.Chat, 400, "x: invalid tool schema", "", ""},
		{provider.Chat, 502, "x: upstream exceeds context budget", "", ""},
	} {
		w := httptest.NewRecorder()
		status := writeError(w, tc.proto, tc.status, tc.msg)
		var body struct {
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
				Code    any    `json:"code"`
			} `json:"error"`
		}
		json.Unmarshal(w.Body.Bytes(), &body)
		code, _ := body.Error.Code.(string)
		overflow := tc.wantCode != "" || strings.HasPrefix(tc.wantMsg, "prompt is too long") || strings.Contains(tc.wantMsg, ": prompt is too long")
		switch {
		case tc.proto == provider.Anthropic && tc.wantMsg != "" && body.Error.Message != tc.wantMsg,
			tc.proto != provider.Anthropic && code != tc.wantCode,
			overflow && (status != 400 || body.Error.Type != "invalid_request_error"),
			!overflow && status != tc.status:
			t.Errorf("%s %d %q: status %d body %s", tc.proto, tc.status, tc.msg, status, w.Body.String())
		}
	}
}

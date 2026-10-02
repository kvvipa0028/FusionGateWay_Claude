package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/yetone/magpie/internal/provider"
)

// A relay whose one key serves a model only on Anthropic's messages, while
// its chat completions take the others and answer that one with no channel
// (01huadalang on Discord): with Anthropic picked for the model in its
// provider's editor, an agent's chat request for it goes to /v1/messages,
// translated, and chat completions is never tried; another model still
// goes to chat.
func TestModelAPIRoutes(t *testing.T) {
	var mu sync.Mutex
	var hits []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &body)
		mu.Lock()
		hits = append(hits, r.URL.Path+" "+body.Model)
		mu.Unlock()
		switch {
		case r.URL.Path == "/v1/chat/completions" && body.Model == "plain":
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, sse(`data: {"id":"c","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"from chat"}}]}`,
				`data: {"id":"c","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`,
				`data: [DONE]`))
		case r.URL.Path == "/v1/chat/completions":
			http.Error(w, `{"error":{"message":"no available channel for model `+body.Model+` under group default"}}`, 503)
		case r.URL.Path == "/v1/messages" && body.Model == "mixed":
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, sse(`event: message_start
data: {"type":"message_start","message":{"id":"m","type":"message","role":"assistant","model":"mixed","content":[],"usage":{"input_tokens":3,"output_tokens":0}}}`,
				`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
				`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"from messages"}}`,
				`event: content_block_stop
data: {"type":"content_block_stop","index":0}`,
				`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
				`event: message_stop
data: {"type":"message_stop"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer up.Close()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Chat: up.URL + "/v1", Anthropic: up.URL, Models: []string{"mixed", "plain"}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetModelAPI("relay/mixed", "anthropic"); err != nil {
		t.Fatal(err)
	}
	gw := httptest.NewServer(New().Handler())
	defer gw.Close()
	for _, c := range []struct{ model, want, hit string }{
		{"mixed", "from messages", "/v1/messages mixed"},
		{"plain", "from chat", "/v1/chat/completions plain"},
	} {
		mu.Lock()
		hits = nil
		mu.Unlock()
		res, err := http.Post(gw.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"relay/`+c.model+`","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		mu.Lock()
		h := append([]string(nil), hits...)
		mu.Unlock()
		if res.StatusCode != 200 || !strings.Contains(string(b), c.want) {
			t.Fatalf("%s: %d %s (upstream %v)", c.model, res.StatusCode, b, h)
		}
		if len(h) != 1 || h[0] != c.hit {
			t.Fatalf("%s: upstream asked %v", c.model, h)
		}
	}
}

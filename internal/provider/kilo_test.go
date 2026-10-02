package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/yetone/magpie/internal/catalog"
)

// kiloList is the Kilo Gateway's /models as it answers (October 2026):
// OpenRouter's shape with isFree, an auto router priced at -1, a free
// model by its flag alone, ":free" ones, one that takes no tools and one
// that only draws.
const kiloList = `{"data":[
	{"id":"kilo-auto/efficient","name":"Auto Efficient","context_length":1000000,"pricing":{"prompt":"-1","completion":"-1"},"supported_parameters":["tools","reasoning"],"isFree":false},
	{"id":"kilo-auto/free","name":"Auto Free","context_length":256000,"pricing":{"prompt":"0","completion":"0"},"supported_parameters":["tools"],"isFree":true},
	{"id":"stealth/space-bunny-alpha","name":"Space Bunny Alpha","context_length":1000000,"top_provider":{"max_completion_tokens":65536},"architecture":{"input_modalities":["text","image"],"output_modalities":["text"]},"pricing":{"prompt":"0","completion":"0"},"supported_parameters":["tools","reasoning"],"isFree":true},
	{"id":"qwen/qwen3.8-27b:free","name":"Qwen: Qwen3.8 27B (free)","context_length":262144,"supported_parameters":["tools"],"isFree":true},
	{"id":"nvidia/nemotron-3.5-content-safety:free","name":"NVIDIA: Nemotron 3.5 Content Safety (free)","context_length":128000,"supported_parameters":["max_tokens"],"isFree":true},
	{"id":"google/lyria-3-pro-preview","name":"Lyria","context_length":1048576,"architecture":{"output_modalities":["audio"]},"pricing":{"prompt":"0","completion":"0"},"isFree":false},
	{"id":"anthropic/claude-sonnet-5","name":"Anthropic: Claude Sonnet 5","context_length":1000000,"pricing":{"prompt":"0.000003","completion":"0.000015"},"supported_parameters":["tools"],"isFree":false}
]}`

// The Kilo Gateway serves its free models to anyone (by IP), and Kilo's
// clients list them from its /models, asked as the Kilo CLI: with no key
// the provider lists the free ones alone, asked with no Authorization;
// with a key every model, the free ones marked Free and priced at nothing
// (lml on Discord: the community plugin's free models from KiloCode).
func TestKiloModels(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var asked []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/openrouter/models" {
			http.NotFound(w, r)
			return
		}
		asked = append(asked, r)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(kiloList))
	}))
	defer srv.Close()
	p, err := FromPreset("kilo")
	if err != nil {
		t.Fatal(err)
	}
	if !p.Ready() {
		t.Fatal("a Kilo provider with no key isn't ready")
	}
	p.ID, p.Chat = "kilo-test", srv.URL+"/api/openrouter"
	ids := func(ms []catalog.Model) (all, free []string) {
		for _, m := range ms {
			all = append(all, m.ID)
			if m.Free {
				free = append(free, m.ID)
			}
		}
		return
	}

	ms, err := p.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	h := asked[0].Header
	for k, v := range map[string]string{"Authorization": "", "User-Agent": "Kilo-Code/" + KiloVersion,
		"X-KILOCODE-EDITORNAME": "Kilo CLI " + KiloVersion, "HTTP-Referer": "https://kilocode.ai", "X-Title": "Kilo Code"} {
		if h.Get(k) != v {
			t.Errorf("list asked with %s %q, want %q", k, h.Get(k), v)
		}
	}
	all, free := ids(ms)
	want := []string{"kilo-auto/free", "stealth/space-bunny-alpha", "qwen/qwen3.8-27b:free"}
	if !slices.Equal(all, want) || !slices.Equal(free, want) {
		t.Fatalf("no key: %v (free %v)", all, free)
	}
	if m := ms[1]; m.Context != 1000000 || m.Output != 65536 || !m.Images || !m.Reasoning || m.Name != "Space Bunny Alpha" {
		t.Fatalf("model: %+v", m)
	}
	// a free model by its flag alone costs nothing, not its maker's price
	if pr, ok := p.ListPrice("stealth/space-bunny-alpha"); !ok || pr != (catalog.Price{}) {
		t.Fatalf("free price: %+v %v", pr, ok)
	}

	p.Key = "kilo_jwt"
	ms, err = p.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := asked[1].Header.Get("Authorization"); got != "Bearer kilo_jwt" {
		t.Fatalf("with a key, asked with %q", got)
	}
	all, free = ids(ms)
	if !slices.Equal(all, []string{"kilo-auto/efficient", "kilo-auto/free", "stealth/space-bunny-alpha", "qwen/qwen3.8-27b:free", "anthropic/claude-sonnet-5"}) || !slices.Equal(free, want) {
		t.Fatalf("with a key: %v (free %v)", all, free)
	}

	// another provider's ":free" model isn't Kilo's to price
	o, _ := FromPreset("together")
	if o.kiloFreeModel("qwen/qwen3.8-27b:free") {
		t.Fatal("kiloFreeModel on another provider")
	}
}

// The CLI's headers: the session as its task, and no Authorization at all
// when there is no key (a placeholder bearer is refused).
func TestKiloClient(t *testing.T) {
	h := http.Header{"Authorization": {"Bearer "}}
	KiloClient(h, "", "conversation-1")
	id := h.Get("X-KILOCODE-TASKID")
	if !openCodeIDRe.MatchString(id) || id[:4] != "ses_" || h.Get("x-session-affinity") != id || h.Get("X-Session-Id") != id ||
		h.Get("x-kilocode-mode") != "code" || len(h.Values("Authorization")) != 0 {
		t.Fatalf("headers: %v", h)
	}
	h2 := http.Header{}
	KiloClient(h2, "k", "conversation-1")
	if h2.Get("X-KILOCODE-TASKID") != id || h2.Get("Authorization") != "" {
		t.Fatalf("with a key: %v", h2)
	}
}

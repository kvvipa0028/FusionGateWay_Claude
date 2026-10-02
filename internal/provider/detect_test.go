package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/yetone/magpie/internal/agentenv"
	"github.com/yetone/magpie/internal/settings"
)

func detectHome(t *testing.T) {
	t.Helper()
	isolate(t)
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	t.Setenv("PATH", h)
	for _, v := range agentenv.Vars {
		t.Setenv(v, "")
	}
}

// asked is one request a fake relay was sent.
type asked struct {
	method, path, auth, model string
	body                      map[string]any
}

// relay serves Chat Completions and Anthropic Messages under /v1, not
// Responses, and lists a GPT and a Claude model.
func relay(t *testing.T) (*httptest.Server, func() []asked) {
	var mu sync.Mutex
	var got []asked
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		json.Unmarshal(b, &body)
		m, _ := body["model"].(string)
		mu.Lock()
		got = append(got, asked{r.Method, r.URL.Path, r.Header.Get("Authorization") + r.Header.Get("x-api-key"), m, body})
		mu.Unlock()
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/models":
			io.WriteString(w, `{"data":[{"id":"gpt-image-2"},{"id":"gpt-5.5"},{"id":"claude-sonnet-5"}]}`)
		case "POST /v1/chat/completions":
			io.WriteString(w, `{"id":"c","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"length"}]}`)
		case "POST /v1/messages":
			io.WriteString(w, `{"id":"m","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"stop_reason":"max_tokens"}`)
		default:
			http.Error(w, `{"error":{"message":"Invalid URL (POST `+r.URL.Path+`)"}}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []asked {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(got)
	}
}

// One URL typed, one click: each API magpie speaks upstream is sent the
// smallest request at that URL as it takes it, and what answered says
// which the relay serves (01huadalang on Discord). A model is picked from
// the vendor's list for each, which is asked for first, and kept nowhere.
func TestDetectProtocols(t *testing.T) {
	detectHome(t)
	srv, got := relay(t)
	for _, typed := range []string{srv.URL, srv.URL + "/v1/", srv.URL + "/v1/chat/completions"} {
		before := len(got())
		res, err := Provider{Key: "sk-relay"}.Detect(context.Background(), typed, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(res) != 3 {
			t.Fatalf("%s: %+v", typed, res)
		}
		want := []struct {
			proto Protocol
			ok    bool
			base  string
			model string
		}{
			{Chat, true, srv.URL + "/v1", "gpt-5.5"},
			{Responses, false, srv.URL + "/v1", "gpt-5.5"},
			{Anthropic, true, srv.URL, "claude-sonnet-5"},
		}
		for i, w := range want {
			r := res[i]
			if r.Protocol != w.proto || r.OK != w.ok || r.Base != w.base || r.Model != w.model {
				t.Fatalf("%s: %s = %+v, want %+v", typed, w.proto, r, w)
			}
		}
		if res[1].Status != 404 || !strings.Contains(res[1].Error, "Invalid URL") {
			t.Fatalf("%s: responses said %+v", typed, res[1])
		}
		// the list, then one request an API, each the smallest, with the key
		var posts []string
		for _, a := range got()[before:] {
			if a.auth == "" || !strings.Contains(a.auth, "sk-relay") {
				t.Fatalf("%s %s went without the key", a.method, a.path)
			}
			if a.method == "GET" {
				continue
			}
			posts = append(posts, a.path)
			for _, k := range []string{"max_tokens", "max_output_tokens"} {
				if n, ok := a.body[k].(float64); ok && n > 16 {
					t.Fatalf("%s asked for %v tokens", a.path, n)
				}
			}
			if a.body["stream"] == true {
				t.Fatalf("%s streamed", a.path)
			}
		}
		slices.Sort(posts)
		if !slices.Equal(posts, []string{"/v1/chat/completions", "/v1/messages", "/v1/responses"}) {
			t.Fatalf("%s: asked %v", typed, posts)
		}
	}
	// nothing was kept: no provider, no model list
	if ps := All(); len(ps) != 0 {
		t.Fatalf("detecting saved %+v", ps)
	}
}

// A model typed is the one asked on all three, and the vendor's list isn't
// asked for; a URL the provider has for an API is asked rather than the
// one typed; no URL at all says so.
func TestDetectModelAndOwnURLs(t *testing.T) {
	detectHome(t)
	srv, got := relay(t)
	other, gotOther := relay(t)
	res, err := Provider{Key: "k", Anthropic: other.URL}.Detect(context.Background(), srv.URL, "claude-sonnet-5")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		if r.Model != "claude-sonnet-5" {
			t.Fatalf("%s asked for %s", r.Protocol, r.Model)
		}
	}
	if res[2].Base != other.URL || !res[2].OK {
		t.Fatalf("anthropic: %+v", res[2])
	}
	for _, a := range got() {
		if a.method == "GET" || a.path == "/v1/messages" {
			t.Fatalf("asked %s %s at the typed URL", a.method, a.path)
		}
	}
	if as := gotOther(); len(as) != 1 || as[0].path != "/v1/messages" {
		t.Fatalf("the provider's own Anthropic URL was asked %+v", as)
	}
	if _, err := (Provider{Key: "k"}).Detect(context.Background(), " ", ""); err != ErrNoURL {
		t.Fatalf("no URL: %v", err)
	}
	if _, err := (Provider{Key: "k"}).Detect(context.Background(), "relay.example.com/v1", ""); err == nil {
		t.Fatal("a URL with no scheme was asked")
	}
}

// A relay whose one key serves some models on one API and others on
// another: the API the user picks for a model is the only one it is asked
// on — by the gateway (through APIs and Native) and by a model's test —
// and must be one the provider has a URL for.
func TestModelAPI(t *testing.T) {
	detectHome(t)
	srv, got := relay(t)
	if err := Save(Provider{ID: "relay", Name: "Relay", Key: "k", Chat: srv.URL + "/v1", Anthropic: srv.URL, Models: []string{"mixed", "plain"}}); err != nil {
		t.Fatal(err)
	}
	p, _ := Find("relay")
	if apis := p.APIs("mixed"); apis != nil {
		t.Fatalf("before: %v", apis)
	}
	if n := p.Native("mixed"); n != Chat {
		t.Fatalf("before: native %s", n)
	}
	if err := SetModelAPI("relay/mixed", "responses"); err == nil || !strings.Contains(err.Error(), "no responses URL") {
		t.Fatalf("an API with no URL: %v", err)
	}
	if err := SetModelAPI("relay/mixed", "gemini"); err == nil {
		t.Fatal("gemini was taken")
	}
	if err := SetModelAPI("relay/nope", "anthropic"); err == nil {
		t.Fatal("a model it hasn't was given an API")
	}
	api := "anthropic"
	if err := SetModelPrefs("relay", map[string]ModelPref{"mixed": {API: &api}}); err != nil {
		t.Fatal(err)
	}
	if settings.Load().ModelAPIs["relay/mixed"] != "anthropic" {
		t.Fatalf("kept %v", settings.Load().ModelAPIs)
	}
	p, _ = Find("relay")
	if apis := p.APIs("mixed"); !slices.Equal(apis, []Protocol{Anthropic}) {
		t.Fatalf("apis %v", apis)
	}
	if n := p.Native("mixed"); n != Anthropic {
		t.Fatalf("native %s", n)
	}
	if apis := p.APIs("plain"); apis != nil {
		t.Fatalf("the other model followed: %v", apis)
	}
	if got, ok := p.ModelAPI("mixed"); !ok || got != Anthropic {
		t.Fatalf("ModelAPI %v %v", got, ok)
	}
	before := len(got())
	if r := p.TestModels(context.Background(), []string{"mixed"}); !r[0].OK || r[0].Protocol != Anthropic {
		t.Fatalf("test %+v", r)
	}
	if as := got()[before:]; len(as) != 1 || as[0].path != "/v1/messages" {
		t.Fatalf("the test asked %+v", as)
	}
	// its Anthropic URL gone, the model is asked where the provider can
	q := *p
	q.Anthropic = ""
	if apis := q.APIs("mixed"); apis != nil {
		t.Fatalf("with no Anthropic URL: %v", apis)
	}
	// "" gives it back to the list
	none := ""
	if err := SetModelPrefs("relay", map[string]ModelPref{"mixed": {API: &none}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := settings.Load().ModelAPIs["relay/mixed"]; ok {
		t.Fatal("still kept")
	}
	if p, _ = Find("relay"); p.APIs("mixed") != nil {
		t.Fatal("still anthropic")
	}
}

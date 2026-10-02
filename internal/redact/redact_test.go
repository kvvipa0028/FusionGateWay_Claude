package redact

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	openaiKey = "sk-proj-abcdEFGH1234ijklMNOP5678qrst"
	ghToken   = "ghp_0123456789abcdefghijABCDEFGHIJ0123"
)

func TestMask(t *testing.T) {
	cases := []struct {
		in    string
		o     Options
		gone  []string // not in what is sent
		kept  []string // still there
		count int
	}{
		{in: "export OPENAI_API_KEY=" + openaiKey, gone: []string{openaiKey}, kept: []string{"export OPENAI_API_KEY="}, count: 1},
		{in: "token " + ghToken + " and AKIAIOSFODNN7EXAMPLE", gone: []string{ghToken, "AKIAIOSFODNN7EXAMPLE"}, count: 2},
		{in: "postgres://app:s3cretPass@db.internal:5432/app", gone: []string{"s3cretPass"}, kept: []string{"postgres://app:", "@db.internal"}, count: 1},
		{in: "postgres://app:${DB_PASS}@db/app", kept: []string{"${DB_PASS}"}},
		{in: `DB_PASSWORD="hunter2hunter2x9"`, gone: []string{"hunter2hunter2x9"}, count: 1},
		{in: "password = getPassword()", kept: []string{"getPassword()"}},
		{in: "the tokenizer splits words", kept: []string{"tokenizer"}},
		{in: "-----BEGIN RSA PRIVATE KEY-----\nMIIEow\n-----END RSA PRIVATE KEY-----", gone: []string{"MIIEow"}, count: 1},
		{in: "mail me: jane.doe@acme.io", kept: []string{"jane.doe@acme.io"}}, // personal off
		{in: "mail me: jane.doe@acme.io or 13812345678", o: Options{Secrets: true, Personal: true}, gone: []string{"jane.doe@acme.io", "13812345678"}, count: 2},
		{in: "see user@example.com", o: Options{Secrets: true, Personal: true}, kept: []string{"user@example.com"}},
		{in: "id 11010519491231002X card 4111 1111 1111 1111", o: Options{Secrets: true, Personal: true}, gone: []string{"11010519491231002X", "4111 1111 1111 1111"}, count: 2},
		{in: "order 4111111111111112", o: Options{Secrets: true, Personal: true}, kept: []string{"4111111111111112"}}, // fails Luhn
		{in: "Project Nightjar ships", o: Options{Secrets: true, Words: []string{"Nightjar"}}, gone: []string{"Nightjar"}, count: 1},
	}
	for _, c := range cases {
		if !c.o.Personal && c.o.Words == nil {
			c.o.Secrets = true
		}
		out, n := Mask(c.in, c.o)
		if n != c.count {
			t.Errorf("%q: %d masked, want %d: %q", c.in, n, c.count, out)
		}
		for _, g := range c.gone {
			if strings.Contains(out, g) {
				t.Errorf("%q: %q still in %q", c.in, g, out)
			}
		}
		for _, k := range c.kept {
			if !strings.Contains(out, k) {
				t.Errorf("%q: %q gone from %q", c.in, k, out)
			}
		}
		if back := Restore(out, false); back != c.in {
			t.Errorf("round trip: %q → %q → %q", c.in, out, back)
		}
	}
	// the same value, the same placeholder
	a, _ := Mask("k="+openaiKey, Options{Secrets: true})
	b, _ := Mask("other "+openaiKey, Options{Secrets: true})
	if pa, pb := placeholderRe.FindString(a), placeholderRe.FindString(b); pa == "" || pa != pb {
		t.Fatalf("placeholders differ: %q %q", a, b)
	}
}

func TestMaskJSON(t *testing.T) {
	body := `{"model":"sk-proj-notmasked-because-model-key","messages":[{"role":"user","content":[` +
		`{"type":"text","text":"key: ` + openaiKey + `\nok"},` +
		`{"type":"thinking","thinking":"hm","signature":"` + openaiKey + `"}]}],"n":1,"stream":true}`
	out, n := MaskJSON([]byte(body), Options{Secrets: true})
	if n != 1 || strings.Count(string(out), openaiKey) != 1 {
		t.Fatalf("%d: %s", n, out)
	}
	if !json.Valid(out) || !strings.Contains(string(out), `"model":"sk-proj-notmasked-because-model-key"`) || !strings.Contains(string(out), `\nok"`) {
		t.Fatalf("body: %s", out)
	}
	if back := RestoreJSON(out); string(back) != body {
		t.Fatalf("restored:\n%s\n%s", back, body)
	}
	// untouched bodies come back byte for byte
	plain := `{ "a" : [1, 2.5e3, true, null, "xé"] }`
	if out, n := MaskJSON([]byte(plain), Options{Secrets: true}); n != 0 || string(out) != plain {
		t.Fatalf("plain: %s", out)
	}
}

// what the model writes back with a placeholder in it, streamed as vendors do
func TestWriterStream(t *testing.T) {
	masked, _ := Mask(openaiKey, Options{Secrets: true})
	pw := masked
	secret := openaiKey
	// Anthropic text split inside the placeholder, and a tool call's JSON
	tool := `{"cmd":"echo ` + pw + `"}`
	var events []string
	textEvent := func(s string) string {
		b, _ := json.Marshal(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": s}})
		return "event: content_block_delta\ndata: " + string(b) + "\n\n"
	}
	jsonEvent := func(s string) string {
		b, _ := json.Marshal(map[string]any{"type": "content_block_delta", "index": 1, "delta": map[string]any{"type": "input_json_delta", "partial_json": s}})
		return "event: content_block_delta\ndata: " + string(b) + "\n\n"
	}
	events = append(events, textEvent("use "+pw[:5]), textEvent(pw[5:12]), textEvent(pw[12:]+" now {"),
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		jsonEvent(tool[:14]), jsonEvent(tool[14:]), "data: [DONE]\n\n")

	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "text/event-stream")
	w := NewWriter(rec)
	w.WriteHeader(200)
	all := strings.Join(events, "")
	// in awkward pieces
	for i := 0; i < len(all); i += 7 {
		w.Write([]byte(all[i:min(i+7, len(all))]))
	}
	w.Finish()

	var text, js strings.Builder
	for _, ev := range strings.Split(rec.Body.String(), "\n\n") {
		_, d, ok := strings.Cut(ev, "data: ")
		if !ok || d == "[DONE]" {
			continue
		}
		var e struct {
			Delta struct {
				Text        string
				PartialJSON string `json:"partial_json"`
			}
		}
		if err := json.Unmarshal([]byte(d), &e); err != nil {
			t.Fatalf("event %q: %v", d, err)
		}
		text.WriteString(e.Delta.Text)
		js.WriteString(e.Delta.PartialJSON)
	}
	if text.String() != "use "+secret+" now {" {
		t.Fatalf("text: %q", text.String())
	}
	var args map[string]string
	if err := json.Unmarshal([]byte(js.String()), &args); err != nil || args["cmd"] != "echo "+secret {
		t.Fatalf("args %q: %v", js.String(), err)
	}
	if strings.Count(rec.Body.String(), "\n\n") != len(events) {
		t.Fatalf("events changed:\n%s", rec.Body.String())
	}
}

func TestWriterWhole(t *testing.T) {
	masked, _ := Mask(ghToken, Options{Secrets: true})
	b, _ := json.Marshal(map[string]any{"output": []any{
		map[string]any{"type": "function_call", "arguments": `{"t":"` + masked + `"}`},
		map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": "got " + masked}}},
	}})
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/json")
	rec.Header().Set("Content-Length", "999")
	w := NewWriter(rec)
	w.WriteHeader(http.StatusOK)
	w.Write(b)
	w.Finish()
	if rec.Header().Get("Content-Length") != "" || strings.Contains(rec.Body.String(), "{{") ||
		!strings.Contains(rec.Body.String(), "got "+ghToken) || !json.Valid(rec.Body.Bytes()) {
		t.Fatalf("%v %s", rec.Header(), rec.Body.String())
	}
}

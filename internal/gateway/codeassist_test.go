package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/catalog"
)

func codeAssistRequest(t *testing.T) *Request {
	t.Helper()
	r, err := parseAnthropic([]byte(`{"model":"x","max_tokens":2000,"system":"be brief","stream":true,
		"thinking":{"type":"enabled","budget_tokens":4000},
		"tools":[{"name":"read","description":"read a file","input_schema":{"$schema":"http://json-schema.org/draft-07/schema#","type":"object",
			"properties":{"path":{"type":["string","null"],"format":"uri"},"mode":{"const":"r"},"opts":{"$ref":"#/$defs/Opts"},
			"n":{"anyOf":[{"type":"integer"},{"type":"null"}]}},"required":["path","gone"],"additionalProperties":false,
			"$defs":{"Opts":{"type":"object","properties":{"deep":{"type":"boolean","default":false}}}}}}],
		"messages":[
			{"role":"user","content":"read a"},
			{"role":"assistant","content":[{"type":"thinking","thinking":"hm","signature":"sig-from-claude"},
				{"type":"text","text":"ok"},{"type":"tool_use","id":"toolu_01:x","name":"read","input":{"path":"a"}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_01:x","content":"A!"}]},
			{"role":"user","content":"and?"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestBuildCodeAssistForGemini(t *testing.T) {
	var env struct {
		Model   string         `json:"model"`
		Request map[string]any `json:"request"`
	}
	json.Unmarshal(buildCodeAssist(codeAssistRequest(t), "gemini-2.5-pro", "gemini"), &env)
	req := env.Request
	if env.Model != "gemini-2.5-pro" {
		t.Errorf("model %q", env.Model)
	}
	b, _ := json.Marshal(req)
	s := string(b)
	for _, want := range []string{
		`"systemInstruction":{"parts":[{"text":"be brief"}],"role":"user"}`,
		`"functionCall":{"args":{"path":"a"},"id":"toolu_01:x","name":"read"},"thoughtSignature":"skip_thought_signature_validator"`,
		`"functionResponse":{"id":"toolu_01:x","name":"read","response":{"output":"A!"}}`,
		`"parametersJsonSchema":{`,
		`"thinkingConfig":{"includeThoughts":true,"thinkingBudget":4096}`,
		`"maxOutputTokens":2000`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in\n%s", want, s)
		}
	}
	if strings.Contains(s, "sig-from-claude") || strings.Contains(s, `"hm"`) {
		t.Error("thinking was sent back")
	}
	// the tool result and the next question are one user turn
	contents := req["contents"].([]any)
	if len(contents) != 3 || len(contents[2].(map[string]any)["parts"].([]any)) != 2 {
		t.Errorf("contents = %v", contents)
	}
}

func TestBuildCodeAssistForAntigravity(t *testing.T) {
	var env struct {
		Request map[string]any `json:"request"`
	}
	json.Unmarshal(buildCodeAssist(codeAssistRequest(t), "claude-sonnet-4-6", "antigravity"), &env)
	b, _ := json.Marshal(env.Request)
	s := string(b)
	for _, want := range []string{
		`"id":"toolu_01_x"`,
		`"response":{"result":"A!"}`,
		`"mode":"VALIDATED"`,
		`"maxOutputTokens":2000`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in\n%s", want, s)
		}
	}
	// Sonnet here doesn't think
	if strings.Contains(s, "thinkingConfig") {
		t.Error("thinking asked of a model that doesn't")
	}
	decl := env.Request["tools"].([]any)[0].(map[string]any)["functionDeclarations"].([]any)[0].(map[string]any)
	params, _ := json.Marshal(decl["parameters"])
	want := `{"properties":{"mode":{"enum":["r"]},"n":{"nullable":true,"type":"integer"},"opts":{"properties":{"deep":{"type":"boolean"}},"type":"object"},"path":{"nullable":true,"type":"string"}},"required":["path"],"type":"object"}`
	if string(params) != want {
		t.Errorf("parameters =\n%s\nwant\n%s", params, want)
	}

	// a Gemini model on Antigravity has no output cap sent, and a thinking
	// Claude has room past its budget
	json.Unmarshal(buildCodeAssist(codeAssistRequest(t), "gemini-3-flash", "antigravity"), &env)
	if gen, _ := env.Request["generationConfig"].(map[string]any); gen["maxOutputTokens"] != nil {
		t.Errorf("gen = %v", gen)
	}
	r := codeAssistRequest(t)
	r.MaxTokens = 0
	env.Request = nil
	json.Unmarshal(buildCodeAssist(r, "claude-opus-4-6-thinking", "antigravity"), &env)
	gen := env.Request["generationConfig"].(map[string]any)
	tc := gen["thinkingConfig"].(map[string]any)
	if tc["thinkingBudget"].(float64) >= gen["maxOutputTokens"].(float64) {
		t.Errorf("gen = %v", gen)
	}
}

func TestCodeAssistDecoder(t *testing.T) {
	var got []Event
	d := &codeAssistDecoder{}
	for _, line := range []string{
		`{"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"let me see","thought":true}]}}],"modelVersion":"gemini-2.5-pro","responseId":"r1"},"traceId":"t"}`,
		`{"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"Reading."}]}}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":3}}}`,
		`{"response":{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"read","args":{"path":"a"}},"thoughtSignature":"abc"}]},"finishReason":"STOP"}],
			"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":10,"thoughtsTokenCount":20,"cachedContentTokenCount":40}}}`,
	} {
		d.decode(line, func(ev Event) { got = append(got, ev) })
	}
	kinds := []EventKind{KStart, KThink, KText, KToolStart, KToolArgs, KStop, KUsage}
	if len(got) != len(kinds) {
		t.Fatalf("events = %+v", got)
	}
	for i, k := range kinds {
		if got[i].Kind != k {
			t.Fatalf("event %d = %+v, want kind %d", i, got[i], k)
		}
	}
	if got[0].Model != "gemini-2.5-pro" || got[3].Name != "read" || got[3].ID == "" || got[4].Text != `{"path":"a"}` {
		t.Errorf("events = %+v", got)
	}
	if got[5].Stop != "tool" {
		t.Errorf("stop = %q", got[5].Stop)
	}
	if u := got[6].Usage; u != (Usage{Input: 60, CacheRead: 40, Output: 30, Reasoning: 20}) {
		t.Errorf("usage = %+v", u)
	}

	var errs []Event
	(&codeAssistDecoder{}).decode(`{"error":{"code":429,"message":"quota"}}`, func(ev Event) { errs = append(errs, ev) })
	if len(errs) != 1 || errs[0].Kind != KError || errs[0].Text != "quota" {
		t.Errorf("error events = %+v", errs)
	}
}

// On Antigravity a model that is one of its families of levels is asked
// for as the variant the effort picks, with no thinking level that could
// say otherwise; an old variant id moves with an effort asked; Gemini 3
// Flash takes medium as medium, Pro as high.
func TestCodeAssistAntigravityLevels(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var raw []catalog.Model
	for _, l := range []string{"gemini-3-flash - Gemini 3 Flash", "gemini-3.1-pro-high - Gemini 3.1 Pro (High)", "gemini-3.1-pro-low - Gemini 3.1 Pro (Low)",
		"gemini-3.7-flash-high - Gemini 3.7 Flash (High)", "gemini-3.7-flash-low - Gemini 3.7 Flash (Low)",
		"gemini-3.7-flash-medium - Gemini 3.7 Flash (Medium)", "gemini-pro-agent - Gemini 3.1 Pro (High)"} {
		id, name, _ := strings.Cut(l, " - ")
		raw = append(raw, catalog.Model{ID: id, Name: name})
	}
	if err := catalog.SaveLive("antigravity", "", raw); err != nil {
		t.Fatal(err)
	}
	// level: "" no thinkingConfig, "-" thinking at the level the id says
	for _, c := range []struct{ model, effort, agent, want, level string }{
		{"gemini-3.7-flash", "low", "antigravity", "gemini-3.7-flash-low", "-"},
		{"gemini-3.7-flash", "medium", "antigravity", "gemini-3.7-flash-medium", "-"},
		{"gemini-3.7-flash", "xhigh", "antigravity", "gemini-3.7-flash-high", "-"},
		{"gemini-3.7-flash", "", "antigravity", "gemini-3.7-flash-high", ""},
		{"gemini-3.7-flash-high", "", "antigravity", "gemini-3.7-flash-high", ""},
		{"gemini-3.7-flash-high", "low", "antigravity", "gemini-3.7-flash-low", "-"},
		{"gemini-3.1-pro", "medium", "antigravity", "gemini-3.1-pro-high", "-"},
		{"gemini-3-flash", "medium", "antigravity", "gemini-3-flash", "medium"},
		{"gemini-pro-agent", "medium", "antigravity", "gemini-pro-agent", "high"},
		{"gemini-pro-agent", "low", "antigravity", "gemini-pro-agent", "low"},
		{"gemini-3.7-flash", "low", "gemini", "gemini-3.7-flash", "low"},
	} {
		r := codeAssistRequest(t)
		r.Thinking, r.Effort = false, c.effort
		var env struct {
			Model   string         `json:"model"`
			Request map[string]any `json:"request"`
		}
		json.Unmarshal(buildCodeAssist(r, c.model, c.agent), &env)
		gen, _ := env.Request["generationConfig"].(map[string]any)
		tc, _ := gen["thinkingConfig"].(map[string]any)
		level, _ := tc["thinkingLevel"].(string)
		if tc != nil && level == "" {
			level = "-"
		}
		if env.Model != c.want || level != c.level {
			t.Errorf("%s at %q on %s: %s at %q, want %s at %q", c.model, c.effort, c.agent, env.Model, level, c.want, c.level)
		}
	}
}

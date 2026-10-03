package runtime

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func grokSeedFiles(t *testing.T, sid, cwd, model string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	for _, name := range []string{"chat_history.jsonl", "chat_history.jsonl.lock", "events.jsonl", "prompt_context.json", "rewind_points.jsonl", "rewind_points.jsonl.lock", "signals.json", "summary.json", "summary.json.lock", "system_prompt.txt", "tool_definitions.json", "updates.jsonl", "updates.jsonl.lock", "usage.json"} {
		files[name] = []byte("{}\n")
		if strings.HasSuffix(name, ".lock") {
			files[name] = nil
		}
	}
	files["chat_history.jsonl"] = []byte("{\"fixture\":\"ORIGINAL_PRIVATE_HISTORY\"}\n")
	files["summary.json"], _ = json.Marshal(map[string]any{"info": map[string]string{"id": sid, "cwd": cwd}, "current_model_id": model})
	return files
}

func TestGrokResumeSeedRejectsUnboundedOrForeignInput(t *testing.T) {
	for _, mode := range []string{"missing", "extra", "traversal", "large", "binary", "lock", "uuid", "summary_id", "summary_cwd", "summary_model", "cwd"} {
		t.Run(mode, func(t *testing.T) {
			_, pending, run, _ := grokChannelFixture(t)
			sid, cwd := "11111111-1111-4111-8111-111111111111", pdir(t)
			files := grokSeedFiles(t, sid, cwd, run.Target.RequestedModel)
			switch mode {
			case "missing":
				delete(files, "events.jsonl")
			case "extra":
				files["config.toml"] = []byte("caller-grant")
			case "traversal":
				delete(files, "events.jsonl")
				files["../events.jsonl"] = []byte("{}")
			case "large":
				files["system_prompt.txt"] = []byte(strings.Repeat("x", 2<<20+1))
			case "binary":
				files["system_prompt.txt"] = []byte{0xff, 0}
			case "lock":
				files["summary.json.lock"] = []byte("not empty")
			case "uuid":
				sid = "../caller-session"
			case "summary_id":
				files["summary.json"] = []byte(`{"info":{"id":"other","cwd":"other"},"current_model_id":"fixture-model"}`)
			case "summary_cwd":
				cwd = pdir(t)
			case "summary_model":
				files["summary.json"] = []byte(`{"info":{"id":"11111111-1111-4111-8111-111111111111"},"current_model_id":"other"}`)
			case "cwd":
				cwd = "relative"
			}
			ch, e := NewGrokResumeChannel(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), pending, run.Target, sid, cwd, files)
			if e == nil {
				ch.Close()
				t.Fatal("unsafe/foreign seed accepted")
			}
		})
	}
}

func TestGrokResumeSeedFreezesBytesAndRemainsPrivate(t *testing.T) {
	_, pending, run, _ := grokChannelFixture(t)
	sid, cwd, home := "11111111-1111-4111-8111-111111111111", pdir(t), pdir(t)
	files := grokSeedFiles(t, sid, cwd, run.Target.RequestedModel)
	ch, e := NewGrokResumeChannel(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), pending, run.Target, sid, cwd, files)
	if e != nil {
		t.Fatal(e)
	}
	defer ch.Close()
	files["chat_history.jsonl"][0] = 'x'
	delete(files, "summary.json")
	if raw, e := json.Marshal(ch); e != nil || strings.Contains(string(raw), "ORIGINAL_PRIVATE_HISTORY") {
		t.Fatal("private seed serialized")
	}
	if e = ch.seed.install(home, Spec{Workspace: cwd, NativeSessionID: sid}); e != nil {
		t.Fatal(e)
	}
	if len(ch.seed.files) != 0 {
		t.Fatal("installed seed retained private history bytes")
	}
	if e = ch.seed.install(pdir(t), Spec{Workspace: cwd, NativeSessionID: sid}); e == nil {
		t.Fatal("consumed seed replayed in another Root")
	}
	path := filepath.Join(home, "sessions", grokSeedCwd(cwd), sid, "chat_history.jsonl")
	raw, e := os.ReadFile(path)
	if e != nil || string(raw) != "{\"fixture\":\"ORIGINAL_PRIVATE_HISTORY\"}\n" {
		t.Fatal("caller modified seed")
	}
	st, e := os.Stat(path)
	if e != nil || st.Mode().Perm() != 0600 {
		t.Fatal("seed file not private")
	}
	if e = ch.seed.install(home, Spec{Workspace: cwd, NativeSessionID: sid}); e == nil {
		t.Fatal("existing session overwritten")
	}
	if e = ch.seed.install(pdir(t), Spec{Workspace: cwd, NativeSessionID: "other"}); e == nil {
		t.Fatal("foreign launch UUID installed")
	}
	if e = ch.seed.install(pdir(t), Spec{Workspace: pdir(t), NativeSessionID: sid}); e == nil {
		t.Fatal("foreign launch cwd installed")
	}
}

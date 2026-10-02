package agent

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/provider"
)

// Codex on one of its own models, signed in to ChatGPT, goes through
// magpie while another of its accounts is on there — so a turn the first
// has no allowance left for goes to the next — and straight to OpenAI
// once none is.
func TestCodexOwnModelThroughMagpieWhileAccountsOn(t *testing.T) {
	claims := func(m map[string]any) string {
		b, _ := json.Marshal(m)
		return "h." + base64.RawURLEncoding.EncodeToString(b) + ".s"
	}
	auth := func(email, acct string) map[string]any {
		return map[string]any{"auth_mode": "chatgpt", "tokens": map[string]any{
			"id_token":      claims(map[string]any{"email": email}),
			"access_token":  claims(map[string]any{"exp": time.Now().Add(time.Hour).Unix()}),
			"refresh_token": "r-" + acct, "account_id": acct}}
	}
	me, _ := json.Marshal(auth("me@example.com", "acct-1"))
	home, read := codexHome(t, string(me), "model = \"gpt-5.5\"\n")
	cx := codex(home)
	logins := func(on bool) {
		b, _ := json.Marshal([]map[string]any{{"agent": "codex", "user": "spare@example.com", "on": on,
			"seen": time.Now(), "auth": auth("spare@example.com", "acct-2")}})
		os.MkdirAll(filepath.Dir(provider.Path()), 0o755)
		os.WriteFile(filepath.Join(filepath.Dir(provider.Path()), "logins.json"), b, 0o600)
	}
	logins(false)
	if err := cx.Fields[0].Set("gpt-5.4"); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); strings.Contains(cfg, "openai_base_url") {
		t.Fatalf("one account:\n%s", cfg)
	}
	logins(true)
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); !strings.Contains(cfg, `openai_base_url = "http://127.0.0.1:`) || !strings.Contains(cfg, `model = "gpt-5.4"`) {
		t.Fatalf("second account on:\n%s", cfg)
	}
	// picked again, it stays so
	if err := cx.Fields[0].Set("gpt-5.5"); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); !strings.Contains(cfg, "openai_base_url") || !strings.Contains(cfg, `model = "gpt-5.5"`) {
		t.Fatalf("picked with two accounts on:\n%s", cfg)
	}
	logins(false)
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); strings.Contains(cfg, "openai_base_url") || !strings.Contains(cfg, `model = "gpt-5.5"`) {
		t.Fatalf("second account off:\n%s", cfg)
	}
}

package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The standalone CLI's sign-in: who in a JSONC config.json, the token in
// the keychain, sent as is under the CLI's integration.
func TestCopilotCLILogin(t *testing.T) {
	home := signIn(t)
	os.Remove(filepath.Join(home, ".config", "github-copilot", "apps.json"))
	cli := filepath.Join(home, "copilot-home")
	t.Setenv("COPILOT_HOME", cli)
	os.MkdirAll(cli, 0o700)
	os.WriteFile(filepath.Join(cli, "config.json"), []byte(`// written by copilot
{
  "lastLoggedInUser": {"host": "https://github.com", "login": "octocat"},
  "loggedInUsers": [{"host": "https://github.com", "login": "octocat"}], // one
}`), 0o600)
	oldSecret := copilotCLISecret
	var asked string
	copilotCLISecret = func(account string) string { asked = account; return "gho_cli" }
	defer func() { copilotCLISecret = oldSecret }()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer gho_cli" || r.Header.Get("Copilot-Integration-Id") != "copilot-developer-cli" {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(`{"data":[{"id":"claude-sonnet-5","name":"Claude Sonnet 5","model_picker_enabled":true,"capabilities":{"type":"chat"}}]}`))
	}))
	defer api.Close()
	user := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token gho_cli" {
			w.WriteHeader(401)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"endpoints": map[string]string{"api": api.URL}})
	}))
	defer user.Close()
	oldUser, oldToken := CopilotUserURL, CopilotTokenURL
	CopilotUserURL, CopilotTokenURL = user.URL, "http://127.0.0.1:1/never"
	defer func() { CopilotUserURL, CopilotTokenURL = oldUser, oldToken }()
	copilotSessions = map[string]copilotSession{}

	p, ok := find(All(), "copilot")
	if !ok || p.Account.User != "octocat" || asked != "https://github.com:octocat" {
		t.Fatalf("copilot: %+v, asked %q", p, asked)
	}
	ms, err := p.Fetch(context.Background())
	if err != nil || len(ms) != 2 || ms[0].ID != "claude-sonnet-5" || ms[1].ID != CopilotAuto {
		t.Fatalf("models: %+v %v", ms, err)
	}
	req, _ := http.NewRequest("POST", p.Chat+"/chat/completions", nil)
	if err := p.Sign(context.Background(), req, Chat, []byte(`{"messages":[{"role":"user","content":"hi"}]}`)); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(req.URL.String(), api.URL) || req.Header.Get("Authorization") != "Bearer gho_cli" || req.Header.Get("Copilot-Integration-Id") != "copilot-developer-cli" {
		t.Fatalf("signed: %s %v", req.URL, req.Header)
	}

	// signed in with the token kept in plaintext: no keychain
	asked = ""
	os.WriteFile(filepath.Join(cli, "config.json"), []byte(`{"lastLoggedInUser":{"host":"https://github.com","login":"hubot"},"copilotTokens":{"https://github.com:hubot":"gho_plain"}}`), 0o600)
	if app, ok := copilotLogin(filepath.Join(home, ".config")); !ok || app.User != "hubot" || app.Token != "gho_plain" || asked != "" {
		t.Fatalf("plaintext: %+v asked %q", app, asked)
	}
}

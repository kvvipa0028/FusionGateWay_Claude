package provider

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Both WorkBuddy builds (the CodeBuddy plan) refuse a chat whose system
// prompt is Codex's or Claude Code's as "from an unapproved channel"
// (#182); the error says what to do about it, and other errors don't.
func TestWorkBuddyRefusedHint(t *testing.T) {
	home := signIn(t)
	future := time.Now().Add(24 * time.Hour).UnixMilli()
	for _, f := range []string{wbAuthFile(home), filepath.Join(filepath.Dir(wbAuthFile(home)), "workbuddy-desktop-ai.info")} {
		writeFile(t, f, map[string]any{
			"account": map[string]any{"uid": "u1", "nickname": "me"},
			"auth":    map[string]any{"accessToken": "a", "refreshToken": "r", "expiresAt": future, "refreshExpiresAt": future},
		})
	}
	refused := []byte(`{"code":11101,"msg":"Illegal API invocation from an unapproved channel"}`)
	for _, id := range []string{"workbuddy", WorkBuddyAIID} {
		p, ok := find(All(), id)
		if !ok {
			t.Fatalf("%s not signed in", id)
		}
		if got := p.Explain(p.Name+": Illegal API invocation from an unapproved channel", 400, refused); !strings.HasSuffix(got, " — "+WBRefusedHint) {
			t.Errorf("%s: %q", id, got)
		}
		if got := p.Explain(p.Name+": bad", 400, []byte(`{"code":1,"msg":"first message is not system prompt"}`)); got != p.Name+": bad" {
			t.Errorf("%s, another error: %q", id, got)
		}
	}
}

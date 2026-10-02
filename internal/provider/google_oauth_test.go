package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// A missing or unsafe private configuration must prevent HTTP and cached-token use.
func TestGoogleOAuthMissingOrUnsafeConfigRefusesCachedToken(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		mode       os.FileMode
	}{
		{"missing", "", 0600},
		{"empty", `{}`, 0600},
		{"partial", `{"schema_version":1,"clients":{"gemini":{"client_id":"fake-gemini-client"}}}`, 0600},
		{"invalid", `{"client_secret":"do-not-echo-this",`, 0600},
		{"public", `{"schema_version":1,"clients":{"gemini":{"client_id":"fake-gemini-client","client_secret":"fake-gemini-secret"}}}`, 0644},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeGoogle{}
			googleSandbox(t, f)
			if err := os.Remove(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "fusion-gateway", "google-oauth-clients.json")); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "fusion-gateway")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if tc.data != "" {
				if err := os.WriteFile(filepath.Join(dir, "google-oauth-clients.json"), []byte(tc.data), tc.mode); err != nil {
					t.Fatal(err)
				}
			}
			app, _ := googleAppOf("gemini")
			g := googleAccount{app: app, auth: googleAuth{AccessToken: "cached-token", RefreshToken: "fake-refresh", Expiry: time.Now().Add(time.Hour).UnixMilli()}}
			tok, err := g.token(context.Background())
			if err == nil || tok != "" {
				t.Errorf("unconfigured cached token was accepted")
			}
			if err != nil && strings.Contains(err.Error(), "do-not-echo-this") {
				t.Error("configuration value leaked in error")
			}
			if len(f.heads) != 0 {
				t.Error("unconfigured path contacted upstream")
			}
			_, _, err = googleExchange(context.Background(), app, "fake-code", "fake-verifier", "http://localhost/callback")
			if err == nil {
				t.Error("unconfigured code exchange accepted")
			}
			if len(f.heads) != 0 {
				t.Error("unconfigured exchange contacted upstream")
			}
		})
	}
}

func TestGoogleOAuthMissingConfigRefusesSignInAndImport(t *testing.T) {
	f := &fakeGoogle{}
	googleSandbox(t, f)
	if err := os.Remove(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "fusion-gateway", "google-oauth-clients.json")); err != nil {
		t.Fatal(err)
	}
	s := &signInFlow{st: SignInState{Agent: "gemini"}, done: make(chan struct{})}
	err := s.begin()
	defer s.finish(SignInState{State: "canceled"})
	if err == nil || s.st.URL != "" {
		t.Error("unconfigured sign-in opened authorization flow")
	}
	_, err = ImportGoogleAccounts(context.Background(), "antigravity", []string{`{"email":"fake@example.test","refresh_token":"fake-refresh"}`})
	if err == nil {
		t.Error("unconfigured import accepted")
	}
	if len(f.heads) != 0 {
		t.Error("unconfigured import contacted upstream")
	}
}

func writeGoogleOAuthFixture(t *testing.T, geminiID string) string {
	t.Helper()
	path := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "fusion-gateway", "google-oauth-clients.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	data := `{"schema_version":1,"clients":{"gemini":{"client_id":"` + geminiID + `","client_secret":"fake-gemini-secret"},"antigravity":{"client_id":"fake-antigravity-client","client_secret":"fake-antigravity-secret"}}}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestGoogleOAuthConfiguredClientAndChangeBoundary(t *testing.T) {
	f := &fakeGoogle{}
	googleSandbox(t, f)
	path := writeGoogleOAuthFixture(t, "fake-gemini-client")
	app, _ := googleAppOf("gemini")
	if app.clientID != "fake-gemini-client" || app.clientSecret != "fake-gemini-secret" {
		t.Error("private client was not selected")
	}
	if err := addGoogleLogin("gemini", "fake@example.test", "", googleAuth{AccessToken: "private-cached", RefreshToken: "fake-refresh", Expiry: time.Now().Add(time.Hour).UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	gs := googleLogins("gemini")
	if len(gs) != 1 {
		t.Fatal("saved client fixture missing")
	}
	g := gs[0].acct
	if tok, err := g.token(context.Background()); err != nil || tok != "private-cached" {
		t.Error("configured cached token refused")
	}
	writeGoogleOAuthFixture(t, "changed-gemini-client")
	if tok, err := g.token(context.Background()); err == nil || tok != "" {
		t.Error("changed client reused previous cached token")
	}
	newer, _ := googleAppOf("gemini")
	if _, _, err := googleExchange(context.Background(), newer, "fake-code", "fake-verifier", "http://localhost/callback"); err == nil {
		t.Error("client hot swap accepted")
	}
	if len(f.heads) != 0 {
		t.Error("changed client contacted upstream")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if tok, err := g.token(context.Background()); err == nil || tok != "" {
		t.Error("deleted config reused cached token")
	}
}

func TestGoogleOAuthLegacyClientOwnershipUnverified(t *testing.T) {
	f := &fakeGoogle{}
	googleSandbox(t, f)
	writeGoogleOAuthFixture(t, "fake-gemini-client")
	writeGeminiLogin(t, "")
	// Replace the native fixture with the historical format, lacking id_token.
	if err := os.WriteFile(filepath.Join(geminiDir(), "oauth_creds.json"), []byte(`{"access_token":"legacy","refresh_token":"legacy-refresh","expiry_date":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	g, ok := geminiOwnLogin()
	if !ok {
		t.Fatal("legacy account should remain visible")
	}
	if tok, err := g.token(context.Background()); err == nil || tok != "" {
		t.Error("native token with unknown client ownership used")
	}
	app, _ := googleAppOf("gemini")
	g = googleAccount{app: app, auth: googleAuth{AccessToken: "legacy", RefreshToken: "legacy-refresh", Expiry: time.Now().Add(time.Hour).UnixMilli()}}
	if tok, err := g.token(context.Background()); err == nil || tok != "" {
		t.Error("legacy saved token with unknown client ownership used")
	}
	if len(f.heads) != 0 {
		t.Error("unverified client contacted upstream")
	}
}

// The same refresh-token text must not share cached access across two clients.
func TestGoogleOAuthClientsAndTokenCacheAreSeparate(t *testing.T) {
	googleSandbox(t, &fakeGoogle{})
	var mu sync.Mutex
	forms := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Error("invalid token form")
				return
			}
			id, secret := r.Form.Get("client_id"), r.Form.Get("client_secret")
			token := ""
			switch {
			case id == "fake-gemini-client" && secret == "fake-gemini-secret":
				token = "at-gemini"
			case id == "fake-antigravity-client" && secret == "fake-antigravity-secret":
				token = "at-antigravity"
			default:
				t.Error("token request used an unregistered or mixed client")
				http.Error(w, "wrong client", 400)
				return
			}
			grant := r.Form.Get("grant_type")
			if grant != "refresh_token" && grant != "authorization_code" {
				t.Error("unexpected grant")
			}
			if grant == "authorization_code" && (r.Form.Get("code") != "fake-code" || r.Form.Get("code_verifier") != "fake-verifier") {
				t.Error("code exchange fields lost")
			}
			mu.Lock()
			forms = append(forms, id+":"+grant)
			mu.Unlock()
			json.NewEncoder(w).Encode(map[string]any{"access_token": token, "refresh_token": "shared-refresh", "expires_in": 3600})
		case "/userinfo":
			io.WriteString(w, `{"email":"fake@example.test"}`)
		default:
			io.WriteString(w, `{"cloudaicompanionProject":"fake-project"}`)
		}
	}))
	defer srv.Close()
	oldWho := googleUserInfoURL
	googleUserInfoURL = srv.URL + "/userinfo"
	t.Cleanup(func() { googleUserInfoURL = oldWho })
	googleTokenURL, codeAssistProd, codeAssistDaily = srv.URL+"/token", srv.URL+"/prod", srv.URL+"/daily"
	accounts := []googleAccount{}
	for _, tc := range []struct{ agent, want string }{{"gemini", "at-gemini"}, {"antigravity", "at-antigravity"}} {
		if err := addGoogleLogin(tc.agent, "fake@example.test", "", googleAuth{RefreshToken: "shared-refresh"}); err != nil {
			t.Fatal(err)
		}
		g := googleLogins(tc.agent)[0].acct
		tok, err := g.token(context.Background())
		if err != nil || tok != tc.want {
			t.Error("refresh used wrong client or cache")
		}
		app, _ := googleAppOf(tc.agent)
		exchanged, _, err := googleExchange(context.Background(), app, "fake-code", "fake-verifier", "http://localhost/callback")
		if err != nil || exchanged.auth.AccessToken != tc.want {
			t.Error("exchange used wrong client")
		}
		accounts = append(accounts, g)
	}
	if tok, err := accounts[0].token(context.Background()); err != nil || tok != "at-gemini" {
		t.Error("second client contaminated first client's cache")
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"fake-gemini-client:refresh_token", "fake-gemini-client:authorization_code", "fake-antigravity-client:refresh_token", "fake-antigravity-client:authorization_code"}
	if len(forms) != len(want) {
		t.Fatalf("token request count %d; want %d", len(forms), len(want))
	}
	for i := range want {
		if forms[i] != want[i] {
			t.Error("client/grant ordering mismatch")
		}
	}
}

func TestGoogleOAuthSignInCallbackRejectsChangedConfig(t *testing.T) {
	f := &fakeGoogle{}
	googleSandbox(t, f)
	s := &signInFlow{st: SignInState{Agent: "gemini", State: "waiting"}, state: "fake-state", verifier: "fake-verifier", done: make(chan struct{})}
	if err := s.begin(); err != nil {
		t.Fatal(err)
	}
	defer s.finish(SignInState{State: "canceled"})
	writeGoogleOAuthFixture(t, "changed-gemini-client")
	req := httptest.NewRequest("GET", "http://localhost/oauth2callback?state=fake-state&code=fake-code", nil)
	s.callback(httptest.NewRecorder(), req)
	if s.status().State != "failed" {
		t.Error("changed config callback was accepted")
	}
	if len(f.heads) != 0 {
		t.Error("changed config callback contacted upstream")
	}
	if len(readLogins()) != 0 {
		t.Error("failed callback persisted an account")
	}
}

func TestGoogleOAuthInvalidFileAndWrongAccountRevision(t *testing.T) {
	for _, tc := range []struct{ name, data string }{
		{"version", `{"schema_version":2,"clients":{}}`},
		{"unknown field", `{"schema_version":1,"clients":{},"private_value":"never-echo"}`},
		{"same client", `{"schema_version":1,"clients":{"gemini":{"client_id":"same","client_secret":"a"},"antigravity":{"client_id":"same","client_secret":"b"}}}`},
		{"trailing json", `{"schema_version":1,"clients":{}} {}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeGoogle{}
			googleSandbox(t, f)
			path := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "fusion-gateway", "google-oauth-clients.json")
			if err := os.WriteFile(path, []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			app, _ := googleAppOf("gemini")
			if err := app.requireOAuth(); err == nil {
				t.Error("invalid config accepted")
			} else if strings.Contains(err.Error(), "never-echo") {
				t.Error("private value echoed")
			}
			if len(f.heads) != 0 {
				t.Error("invalid config contacted upstream")
			}
		})
	}
	t.Run("symlink", func(t *testing.T) {
		f := &fakeGoogle{}
		googleSandbox(t, f)
		path := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "fusion-gateway", "google-oauth-clients.json")
		target := path + ".target"
		if err := os.Rename(path, target); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		app, _ := googleAppOf("gemini")
		if app.requireOAuth() == nil {
			t.Error("symlink config accepted")
		}
	})
	t.Run("old client revision", func(t *testing.T) {
		f := &fakeGoogle{}
		googleSandbox(t, f)
		app, _ := googleAppOf("gemini")
		auth := googleAuth{ClientRevision: "another-client", AccessToken: "cached", RefreshToken: "shared", Expiry: time.Now().Add(time.Hour).UnixMilli()}
		data, _ := json.Marshal(auth)
		g, ok := googleSaved("gemini", savedLogin{Agent: "gemini", User: "fake@example.test", Auth: data})
		if !ok {
			t.Fatal("old account should remain visible for reauthentication")
		}
		if tok, err := g.token(context.Background()); tok != "" || err == nil {
			t.Error("old client cached token accepted")
		}
		if _, err := g.project(context.Background()); err == nil {
			t.Error("old client project accepted")
		}
		if len(f.heads) != 0 {
			t.Error("old client contacted upstream")
		}
		// Matching saved metadata can execute; unknown revisions cannot be silently filled.
		auth.ClientRevision = app.clientRevision
		data, _ = json.Marshal(auth)
		g, ok = googleSaved("gemini", savedLogin{Agent: "gemini", User: "fake@example.test", Auth: data})
		if !ok {
			t.Fatal("matching account missing")
		}
		if tok, err := g.token(context.Background()); tok != "cached" || err != nil {
			t.Error("matching saved account refused")
		}
	})
}

func TestGoogleOAuthTokenErrorDoesNotExposeCredentials(t *testing.T) {
	googleSandbox(t, &fakeGoogle{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]string{"error_description": "fake-gemini-client fake-gemini-secret fake-code fake-verifier private-refresh"})
	}))
	defer srv.Close()
	googleTokenURL = srv.URL
	app, _ := googleAppOf("gemini")
	_, _, err := googleExchange(context.Background(), app, "fake-code", "fake-verifier", "http://localhost/callback")
	if err == nil {
		t.Fatal("expected upstream refusal")
	}
	for _, v := range []string{"fake-gemini-client", "fake-gemini-secret", "fake-code", "fake-verifier"} {
		if strings.Contains(err.Error(), v) {
			t.Error("exchange error exposed credential")
		}
	}
	if err := addGoogleLogin("gemini", "fake@example.test", "", googleAuth{RefreshToken: "private-refresh"}); err != nil {
		t.Fatal(err)
	}
	_, err = googleLogins("gemini")[0].acct.token(context.Background())
	if err == nil {
		t.Fatal("expected refresh refusal")
	}
	for _, v := range []string{"fake-gemini-client", "fake-gemini-secret", "private-refresh"} {
		if strings.Contains(err.Error(), v) {
			t.Error("refresh error exposed credential")
		}
	}
}

func TestGoogleOAuthObservedChangeStaysBlockedUntilRestart(t *testing.T) {
	googleSandbox(t, &fakeGoogle{})
	if err := addGoogleLogin("gemini", "fake@example.test", "", googleAuth{AccessToken: "cached", RefreshToken: "fake-refresh", Expiry: time.Now().Add(time.Hour).UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	g := googleLogins("gemini")[0].acct
	writeGoogleOAuthFixture(t, "changed-gemini-client")
	if _, err := g.token(context.Background()); err == nil {
		t.Fatal("changed config accepted")
	}
	writeGoogleOAuthFixture(t, "fake-gemini-client")
	if _, err := g.token(context.Background()); err == nil {
		t.Error("observed change was forgotten after file restoration")
	}
}

func TestGoogleOAuthImportRepairsUnverifiedAccount(t *testing.T) {
	googleSandbox(t, &fakeGoogle{})
	if err := addGoogleLogin("antigravity", "a@x.com", "", googleAuth{RefreshToken: "1//a"}); err != nil {
		t.Fatal(err)
	}
	err := editSideLogin("antigravity", "a@x.com", func(ls []savedLogin, i int) ([]savedLogin, error) {
		ls[i].Auth, _ = json.Marshal(googleAuth{RefreshToken: "1//a"})
		return ls, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeGoogleImport{users: map[string]string{"1//a": "a@x.com"}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	oldWho := googleUserInfoURL
	googleUserInfoURL = srv.URL + "/userinfo"
	t.Cleanup(func() { googleUserInfoURL = oldWho })
	googleTokenURL, codeAssistProd, codeAssistDaily = srv.URL+"/token", srv.URL+"/prod", srv.URL+"/daily"
	result, err := ImportGoogleAccounts(context.Background(), "antigravity", []string{`{"email":"a@x.com","refresh_token":"1//a"}`})
	if err != nil || len(result) != 1 || result[0].Status == "failed" {
		t.Fatal("explicit reimport failed")
	}
	if len(f.refreshed) != 1 {
		t.Error("unverified account bypassed explicit client verification")
	}
	g := googleLogins("antigravity")[0].acct
	if g.auth.ClientRevision == "" {
		t.Error("reimport did not bind client ownership")
	}
}

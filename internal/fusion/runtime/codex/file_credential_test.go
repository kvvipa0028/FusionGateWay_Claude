//go:build darwin || linux

package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/fusion/stageplan"
	"golang.org/x/sys/unix"
)

const cacheFixture = `{"auth_mode":"chatgpt","OPENAI_API_KEY":null,"tokens":{"id_token":"opaque-fixture-id-token","access_token":"opaque-fixture-access-token","refresh_token":"opaque-fixture-refresh-token","account_id":"fixture-upstream-account"},"last_refresh":"2026-10-05T10:00:00Z"}`

func codexCredentialFixture(t *testing.T) (string, FileCredentialScope, stageplan.ExecutionTarget) {
	t.Helper()
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range []string{root, filepath.Join(root, "codex"), filepath.Join(root, "codex/home"), filepath.Join(root, "codex/home/.codex")} {
		if e = os.MkdirAll(p, 0700); e != nil {
			t.Fatal(e)
		}
		if e = os.Chmod(p, 0700); e != nil {
			t.Fatal(e)
		}
	}
	path := filepath.Join(root, "codex/home/.codex/auth.json")
	if e = os.WriteFile(path, []byte(cacheFixture), 0600); e != nil {
		t.Fatal(e)
	}
	s := FileCredentialScope{Route: stageplan.RouteRef{ID: "codex-chatgpt", Revision: 1}, Account: "fixture-local-account", Workspace: "fixture-local-workspace", Identity: "fixture-credential-revision", UpstreamAccount: "fixture-upstream-account"}
	target := stageplan.ExecutionTarget{Route: s.Route, Account: s.Account, Workspace: s.Workspace, CredentialIdentity: s.Identity, BillingPath: "subscription", RuntimeVersion: CLIVersion}
	return path, s, target
}

func TestCodexPrivateCredentialExactBindingAndRedaction(t *testing.T) {
	p, s, target := codexCredentialFixture(t)
	c, e := NewFileCredential(p, s)
	if e != nil {
		t.Fatal(e)
	}
	got, e := c.Load(context.Background(), target)
	if e != nil || got.Identity != s.Identity || got.accessToken != "opaque-fixture-access-token" || got.accountID != s.UpstreamAccount {
		t.Fatal("private OAuth cache not loaded")
	}
	// Opaque fixture tokens deliberately are not JWTs: no unverified claims may
	// provide account, entitlement, billing, region, expiry or quota authority.
	for _, v := range []any{c, *c, got, &got} {
		b, e := json.Marshal(v)
		if e != nil {
			t.Fatal("safe credential metadata marshal failed")
		}
		text := string(b) + fmt.Sprintf("%v %+v %#v", v, v, v)
		for _, secret := range []string{"opaque-fixture", p, "fixture-upstream-account"} {
			if strings.Contains(text, secret) {
				t.Fatal("private credential metadata disclosed")
			}
		}
	}
	for _, field := range []string{"account", "workspace", "identity", "route", "revision", "billing", "runtime", "plugin", "cancel", "nil-context"} {
		t.Run(field, func(t *testing.T) {
			bad := target
			ctx := context.Background()
			switch field {
			case "account":
				bad.Account = "other"
			case "workspace":
				bad.Workspace = "other"
			case "identity":
				bad.CredentialIdentity = "other"
			case "route":
				bad.Route.ID = "grok-subscription"
			case "revision":
				bad.Route.Revision++
			case "billing":
				bad.BillingPath = "api"
			case "runtime":
				bad.RuntimeVersion = "other"
			case "plugin":
				bad.PluginVersion = new(string)
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "nil-context":
				ctx = nil
			}
			if _, e := c.Load(ctx, bad); !errors.Is(e, ErrIdentity) {
				t.Fatal("wrong frozen scope accepted")
			}
		})
	}
}

func TestCodexPrivateCredentialOfficialDeviceCache(t *testing.T) {
	// rust-v0.160.0 login/src/server.rs persist_tokens_async writes chatgpt,
	// null OPENAI_API_KEY and an optional account_id; storage.rs omits empty
	// alternate modes. A null account cannot satisfy our independent binding.
	for _, variant := range []string{"pretty-device-cache", "nullable-alternatives", "missing-refresh-time"} {
		t.Run(variant, func(t *testing.T) {
			p, s, target := codexCredentialFixture(t)
			var cache map[string]any
			if e := json.Unmarshal([]byte(cacheFixture), &cache); e != nil {
				t.Fatal(e)
			}
			switch variant {
			case "pretty-device-cache":
				cache["last_refresh"] = "2026-10-05T10:00:00.123456789+08:00"
			case "nullable-alternatives":
				for _, key := range []string{"agent_identity", "personal_access_token", "bedrock_api_key", "bedrock_access_keys"} {
					cache[key] = nil
				}
			case "missing-refresh-time":
				delete(cache, "last_refresh")
			}
			raw, e := json.MarshalIndent(cache, "", "  ")
			if e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(p, append(raw, '\n'), 0600); e != nil {
				t.Fatal(e)
			}
			c, e := NewFileCredential(p, s)
			if e != nil {
				t.Fatal("official OAuth cache shape rejected")
			}
			got, e := c.Load(context.Background(), target)
			if e != nil || got.accountID != s.UpstreamAccount || got.idToken != "opaque-fixture-id-token" || got.refreshToken != "opaque-fixture-refresh-token" {
				t.Fatal("official-shaped cache lost private token binding")
			}
		})
	}
}

func TestCodexPrivateCredentialIncompleteCacheCannotProveBinding(t *testing.T) {
	for _, variant := range []string{"null-mode", "mode-type", "null-account", "missing-account", "account-type", "null-tokens", "token-type", "null-refresh", "token-control", "token-oversize", "timestamp-type", "timestamp-invalid", "bedrock-access", "top-alias", "token-case-duplicate"} {
		t.Run(variant, func(t *testing.T) {
			p, s, _ := codexCredentialFixture(t)
			b := cacheFixture
			switch variant {
			case "null-mode":
				b = strings.Replace(b, `"auth_mode":"chatgpt"`, `"auth_mode":null`, 1)
			case "mode-type":
				b = strings.Replace(b, `"auth_mode":"chatgpt"`, `"auth_mode":{}`, 1)
			case "null-account":
				b = strings.Replace(b, `"account_id":"fixture-upstream-account"`, `"account_id":null`, 1)
			case "missing-account":
				b = strings.Replace(b, `,"account_id":"fixture-upstream-account"`, "", 1)
			case "account-type":
				b = strings.Replace(b, `"account_id":"fixture-upstream-account"`, `"account_id":17`, 1)
			case "null-tokens":
				b = `{"auth_mode":"chatgpt","OPENAI_API_KEY":null,"tokens":null}`
			case "token-type":
				b = strings.Replace(b, `"access_token":"opaque-fixture-access-token"`, `"access_token":[]`, 1)
			case "null-refresh":
				b = strings.Replace(b, `"refresh_token":"opaque-fixture-refresh-token"`, `"refresh_token":null`, 1)
			case "token-control":
				b = strings.Replace(b, "opaque-fixture-access-token", `bad\r\ntoken`, 1)
			case "token-oversize":
				b = strings.Replace(b, "opaque-fixture-access-token", strings.Repeat("x", (16<<10)+1), 1)
			case "timestamp-type":
				b = strings.Replace(b, `"last_refresh":"2026-10-05T10:00:00Z"`, `"last_refresh":19`, 1)
			case "timestamp-invalid":
				b = strings.Replace(b, "2026-10-05T10:00:00Z", "not-a-timestamp", 1)
			case "bedrock-access":
				b = strings.TrimSuffix(b, "}") + `,"bedrock_access_keys":{"access_key_id":"fixture-key"}}`
			case "top-alias":
				b = strings.Replace(b, `"OPENAI_API_KEY"`, `"openai_api_key"`, 1)
			case "token-case-duplicate":
				b = strings.Replace(b, `"access_token":"opaque-fixture-access-token"`, `"access_token":"opaque-fixture-access-token","Access_Token":"other"`, 1)
			}
			if e := os.WriteFile(p, []byte(b), 0600); e != nil {
				t.Fatal(e)
			}
			if _, e := NewFileCredential(p, s); !errors.Is(e, ErrIdentity) {
				t.Fatal("incomplete or alternate credential source accepted")
			}
		})
	}
}

func TestCodexPrivateCredentialRejectsAlternateModesAndAmbiguousJSON(t *testing.T) {
	for _, mode := range []string{"apikey", "headers", "chatgptAuthTokens", "agentIdentity", "personalAccessToken", "bedrockApiKey", "bedrockAccessKeys", "absent-mode", "api-key", "pat", "agent", "bedrock", "account", "empty-token", "token-space", "alias", "duplicate", "token-duplicate", "unknown", "trailing", "null", "invalid-utf8", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			p, s, _ := codexCredentialFixture(t)
			b := cacheFixture
			switch mode {
			case "absent-mode":
				b = strings.Replace(b, `"auth_mode":"chatgpt",`, "", 1)
			case "api-key":
				b = strings.Replace(b, `"OPENAI_API_KEY":null`, `"OPENAI_API_KEY":"fixture-api-key"`, 1)
			case "pat", "agent", "bedrock":
				name := map[string]string{"pat": "personal_access_token", "agent": "agent_identity", "bedrock": "bedrock_api_key"}[mode]
				b = strings.TrimSuffix(b, "}") + `,"` + name + `":"fixture-alternate-secret"}`
			case "account":
				s.UpstreamAccount = "another-upstream"
			case "empty-token":
				b = strings.Replace(b, "opaque-fixture-access-token", "", 1)
			case "token-space":
				b = strings.Replace(b, "opaque-fixture-access-token", "token bad", 1)
			case "alias":
				b = strings.Replace(b, `"access_token"`, `"Access_Token"`, 1)
			case "duplicate":
				b = strings.Replace(b, `"auth_mode":"chatgpt"`, `"auth_mode":"chatgpt","auth_mode":"apikey"`, 1)
			case "token-duplicate":
				b = strings.Replace(b, `"access_token":"opaque-fixture-access-token"`, `"access_token":"opaque-fixture-access-token","access_token":"other"`, 1)
			case "unknown":
				b = strings.TrimSuffix(b, "}") + `,"auth_provider_command":"fixture-command"}`
			case "trailing":
				b += " {}"
			case "null":
				b = "null"
			case "invalid-utf8":
				b = string([]byte{0xff})
			case "oversize":
				b = strings.Repeat("x", (256<<10)+1)
			default:
				b = strings.Replace(b, `"chatgpt"`, `"`+mode+`"`, 1)
			}
			if e := os.WriteFile(p, []byte(b), 0600); e != nil {
				t.Fatal(e)
			}
			if _, e := NewFileCredential(p, s); e == nil {
				t.Fatal("unapproved credential schema/mode accepted")
			} else if strings.Contains(e.Error(), "fixture-") || strings.Contains(e.Error(), p) {
				t.Fatal("unsafe credential error")
			}
		})
	}
}

func TestCodexPrivateCredentialRejectsFileAndDirectoryDrift(t *testing.T) {
	for _, mode := range []string{"content", "identical-inode-replacement", "file-mode", "executable-cache", "hardlink", "symlink", "parent-replacement", "parent-mode", "git-parent", "missing"} {
		t.Run(mode, func(t *testing.T) {
			p, s, target := codexCredentialFixture(t)
			c, e := NewFileCredential(p, s)
			if e != nil {
				t.Fatal(e)
			}
			switch mode {
			case "content":
				e = os.WriteFile(p, []byte(strings.Replace(cacheFixture, "access-token", "another-token", 1)), 0600)
			case "identical-inode-replacement":
				e = os.Rename(p, p+".old")
				if e == nil {
					e = os.WriteFile(p, []byte(cacheFixture), 0600)
				}
			case "file-mode":
				e = os.Chmod(p, 0644)
			case "executable-cache":
				e = os.Chmod(p, 0700)
			case "hardlink":
				e = os.Link(p, p+".link")
			case "symlink":
				e = os.Rename(p, p+".old")
				if e == nil {
					e = os.Symlink(p+".old", p)
				}
			case "parent-replacement":
				parent := filepath.Dir(p)
				e = os.Rename(parent, parent+".old")
				if e == nil {
					e = os.Mkdir(parent, 0700)
				}
				if e == nil {
					e = os.WriteFile(p, []byte(cacheFixture), 0600)
				}
			case "parent-mode":
				e = os.Chmod(filepath.Dir(filepath.Dir(p)), 0755)
			case "git-parent":
				e = os.Mkdir(filepath.Join(filepath.Dir(p), ".git"), 0700)
			case "missing":
				e = os.Remove(p)
			}
			if e != nil {
				t.Fatal(e)
			}
			if _, e := c.Load(context.Background(), target); e == nil {
				t.Fatal("changed credential source accepted")
			}
		})
	}
}

func TestCodexPrivateCredentialRefusesUnsafeInitialSources(t *testing.T) {
	for _, mode := range []string{"relative", "wrong-name", "daily-home", "file-mode", "executable-cache", "parent-mode", "hardlink", "symlink", "fifo", "git-ancestor", "route", "revision", "identity", "empty"} {
		t.Run(mode, func(t *testing.T) {
			p, s, _ := codexCredentialFixture(t)
			var e error
			switch mode {
			case "relative":
				p = "auth.json"
			case "wrong-name":
				p = filepath.Join(filepath.Dir(p), "other.json")
			case "daily-home":
				p = filepath.Join(filepath.Dir(filepath.Dir(p)), "auth.json")
			case "file-mode":
				e = os.Chmod(p, 0644)
			case "executable-cache":
				e = os.Chmod(p, 0700)
			case "parent-mode":
				e = os.Chmod(filepath.Dir(p), 0755)
			case "hardlink":
				e = os.Link(p, p+".link")
			case "symlink":
				e = os.Rename(p, p+".old")
				if e == nil {
					e = os.Symlink(p+".old", p)
				}
			case "fifo":
				e = os.Remove(p)
				if e == nil {
					e = unix.Mkfifo(p, 0600)
				}
			case "git-ancestor":
				e = os.Mkdir(filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(p))), ".git"), 0700)
			case "route":
				s.Route.ID = ""
			case "revision":
				s.Route.Revision = 0
			case "identity":
				s.Identity = "invalid identity"
			case "empty":
				e = os.WriteFile(p, nil, 0600)
			}
			if e != nil {
				t.Fatal(e)
			}
			if _, e := NewFileCredential(p, s); e == nil {
				t.Fatal("unsafe credential source accepted")
			}
		})
	}
}

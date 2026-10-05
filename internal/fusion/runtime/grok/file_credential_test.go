//go:build darwin || linux

package grok

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

const grokCacheFixture = `{"https://accounts.x.ai/sign-in":{"key":"opaque-fixture-grok-bearer","expires_at":"2026-10-12T10:00:00Z","scopes":["offline_access","api:access","grok-cli:access"]}}`

func grokCredentialFixture(t *testing.T) (string, FileCredentialScope, stageplan.ExecutionTarget) {
	t.Helper()
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range []string{root, filepath.Join(root, "grok"), filepath.Join(root, "grok/home"), filepath.Join(root, "grok/home/.grok")} {
		if e = os.MkdirAll(p, 0700); e != nil {
			t.Fatal(e)
		}
		if e = os.Chmod(p, 0700); e != nil {
			t.Fatal(e)
		}
	}
	path := filepath.Join(root, "grok/home/.grok/auth.json")
	if e = os.WriteFile(path, []byte(grokCacheFixture), 0600); e != nil {
		t.Fatal(e)
	}
	s := FileCredentialScope{Route: stageplan.RouteRef{ID: "grok-subscription", Revision: 1}, Account: "fixture-local-account", Workspace: "fixture-local-workspace", Identity: "fixture-credential-revision", Issuer: "https://accounts.x.ai/sign-in"}
	target := stageplan.ExecutionTarget{Route: s.Route, Account: s.Account, Workspace: s.Workspace, CredentialIdentity: s.Identity, BillingPath: "subscription", RuntimeVersion: CLIVersion}
	return path, s, target
}

func TestGrokPrivateCredentialExactBindingAndRedaction(t *testing.T) {
	p, s, target := grokCredentialFixture(t)
	c, e := NewFileCredential(p, s)
	if e != nil {
		t.Fatal(e)
	}
	got, e := c.Load(context.Background(), target)
	if e != nil || got.Identity != s.Identity || got.bearer != "opaque-fixture-grok-bearer" {
		t.Fatal("private OAuth cache not loaded", e)
	}
	// Opaque fixture tokens are not parsed for claims: no account, tier,
	// region, billing or quota authority may come from this file.
	for _, v := range []any{c, *c, got, &got} {
		b, e := json.Marshal(v)
		if e != nil {
			t.Fatal("safe credential metadata marshal failed")
		}
		text := string(b) + fmt.Sprintf("%v %+v %#v", v, v, v)
		for _, secret := range []string{"opaque-fixture-grok-bearer", p, "accounts.x.ai"} {
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
				bad.Route.ID = "codex-chatgpt"
			case "revision":
				bad.Route.Revision++
			case "billing":
				bad.BillingPath = "api_key"
			case "runtime":
				bad.RuntimeVersion = "0.0.0"
			case "plugin":
				v := "1"
				bad.PluginVersion = &v
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "nil-context":
				ctx = nil
			}
			if _, e := c.Load(ctx, bad); !errors.Is(e, ErrIdentity) {
				t.Fatal("drifted target accepted", field, e)
			}
		})
	}
}

func TestGrokPrivateCredentialSchema(t *testing.T) {
	for _, mode := range []string{"two-issuers", "wrong-issuer", "missing-key", "blank-key", "oversized-key", "control-char-key", "entry-not-object", "api-key-mixed-in", "duplicate-json-key", "trailing-content", "oversized-file", "empty-file"} {
		t.Run(mode, func(t *testing.T) {
			p, s, _ := grokCredentialFixture(t)
			switch mode {
			case "two-issuers":
				if e := os.WriteFile(p, []byte(`{"https://accounts.x.ai/sign-in":{"key":"opaque-fixture-grok-bearer"},"https://auth.x.ai/oauth":{"key":"other"}}`), 0600); e != nil {
					t.Fatal(e)
				}
			case "wrong-issuer":
				if e := os.WriteFile(p, []byte(`{"https://auth.x.ai":{"key":"opaque-fixture-grok-bearer"}}`), 0600); e != nil {
					t.Fatal(e)
				}
			case "missing-key":
				if e := os.WriteFile(p, []byte(`{"https://accounts.x.ai/sign-in":{"expires_at":"2026-10-12T10:00:00Z"}}`), 0600); e != nil {
					t.Fatal(e)
				}
			case "blank-key":
				if e := os.WriteFile(p, []byte(`{"https://accounts.x.ai/sign-in":{"key":""}}`), 0600); e != nil {
					t.Fatal(e)
				}
			case "oversized-key":
				if e := os.WriteFile(p, []byte(`{"https://accounts.x.ai/sign-in":{"key":"`+strings.Repeat("k", (16<<10)+1)+`"}}`), 0600); e != nil {
					t.Fatal(e)
				}
			case "control-char-key":
				if e := os.WriteFile(p, []byte("{\"https://accounts.x.ai/sign-in\":{\"key\":\"bad\\u0001key\"}}"), 0600); e != nil {
					t.Fatal(e)
				}
			case "entry-not-object":
				if e := os.WriteFile(p, []byte(`{"https://accounts.x.ai/sign-in":"opaque"}`), 0600); e != nil {
					t.Fatal(e)
				}
			case "api-key-mixed-in":
				if e := os.WriteFile(p, []byte(`{"https://accounts.x.ai/sign-in":{"key":"opaque-fixture-grok-bearer","api_key":"xai-fixture"}}`), 0600); e != nil {
					t.Fatal(e)
				}
			case "duplicate-json-key":
				if e := os.WriteFile(p, []byte(`{"https://accounts.x.ai/sign-in":{"key":"a","key":"b"}}`), 0600); e != nil {
					t.Fatal(e)
				}
			case "trailing-content":
				if e := os.WriteFile(p, []byte(grokCacheFixture+`{}`), 0600); e != nil {
					t.Fatal(e)
				}
			case "oversized-file":
				if e := os.WriteFile(p, append([]byte(`{"https://accounts.x.ai/sign-in":{"key":"`), make([]byte, 256<<10)...), 0600); e != nil {
					t.Fatal(e)
				}
			case "empty-file":
				if e := os.WriteFile(p, nil, 0600); e != nil {
					t.Fatal(e)
				}
			}
			c, e := NewFileCredential(p, s)
			if e == nil {
				if _, e = c.Load(context.Background(), stageplan.ExecutionTarget{Route: s.Route, Account: s.Account, Workspace: s.Workspace, CredentialIdentity: s.Identity, BillingPath: "subscription", RuntimeVersion: CLIVersion}); e == nil {
					t.Fatal("invalid cache accepted")
				}
			}
			if e != nil && !errors.Is(e, ErrIdentity) {
				t.Fatal("unexpected error", e)
			}
		})
	}
}

func TestGrokPrivateCredentialFileSafety(t *testing.T) {
	for _, mode := range []string{"rotated", "file-permissions", "dir-permissions", "symlink-file", "symlink-dir", "hardlink", "shallow-path", "not-auth-json"} {
		t.Run(mode, func(t *testing.T) {
			p, s, target := grokCredentialFixture(t)
			if mode == "rotated" {
				c, e := NewFileCredential(p, s)
				if e != nil {
					t.Fatal("fixture must register before rotation", e)
				}
				if e := os.WriteFile(p, []byte(`{"https://accounts.x.ai/sign-in":{"key":"opaque-fixture-rotated"}}`), 0600); e != nil {
					t.Fatal(e)
				}
				if _, e = c.Load(context.Background(), target); !errors.Is(e, ErrIdentity) {
					t.Fatal("rotated credential accepted", e)
				}
				return
			}
			switch mode {
			case "file-permissions":
				if e := os.Chmod(p, 0644); e != nil {
					t.Fatal(e)
				}
			case "dir-permissions":
				if e := os.Chmod(filepath.Dir(p), 0755); e != nil {
					t.Fatal(e)
				}
			case "symlink-file":
				if e := os.Rename(p, p+".real"); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink(p+".real", p); e != nil {
					t.Fatal(e)
				}
			case "symlink-dir":
				if e := os.Rename(filepath.Dir(p), filepath.Dir(p)+".real"); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink(filepath.Dir(p)+".real", filepath.Dir(p)); e != nil {
					t.Fatal(e)
				}
			case "hardlink":
				if e := os.Link(p, p+".alias"); e != nil {
					t.Fatal(e)
				}
			case "shallow-path":
				p = filepath.Join(filepath.Dir(filepath.Dir(p)), "auth.json")
				if e := os.WriteFile(p, []byte(grokCacheFixture), 0600); e != nil {
					t.Fatal(e)
				}
			case "not-auth-json":
				p = p + ".bak"
				if e := os.WriteFile(p, []byte(grokCacheFixture), 0600); e != nil {
					t.Fatal(e)
				}
			}
			c, e := NewFileCredential(p, s)
			if e == nil {
				if _, e = c.Load(context.Background(), target); e == nil {
					t.Fatal("unsafe file accepted", mode)
				}
			}
			if e != nil && !errors.Is(e, ErrIdentity) {
				t.Fatal("unexpected error", mode, e)
			}
		})
	}
	_ = unix.O_NOFOLLOW
}

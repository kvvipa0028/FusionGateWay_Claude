package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/provider"
)

// Run the public command in a fresh process, with only a fake account,
// fake /usage and an on-disk snapshot in a temporary home.
func TestAccountsCachedReading(t *testing.T) {
	if mode := os.Getenv("MAGPIE_TEST_ACCOUNTS_READING"); mode != "" {
		provider.UsageClaudeVia(func(context.Context) (string, error) {
			if strings.HasPrefix(mode, "fresh") {
				return "Current session: 25% used", nil
			}
			return "You are currently using your subscription to power your Claude Code usage.", nil
		})
		args := []string{"accounts", "claude"}
		if strings.HasSuffix(mode, "json") {
			args = append(args, "--json")
		}
		if err := accountsCmd(args); err != nil {
			t.Fatal(err)
		}
		os.Exit(0)
	}
	for _, mode := range []string{"cached-text", "cached-json", "fresh-text", "fresh-json"} {
		t.Run(mode, func(t *testing.T) {
			home := t.TempDir()
			config := filepath.Join(home, ".config", "magpie")
			at := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
			reset := at.Add(time.Hour)
			write := func(path string, value any) {
				t.Helper()
				b, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, b, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			write(filepath.Join(home, ".claude", ".credentials.json"), map[string]any{"claudeAiOauth": map[string]any{
				"accessToken": "sk-ant-oat01-test", "refreshToken": "sk-ant-ort01-test", "expiresAt": time.Now().Add(time.Hour).UnixMilli(),
				"subscriptionType": "max", "scopes": []string{"user:inference", "user:profile"},
			}})
			write(filepath.Join(home, ".claude", ".claude.json"), map[string]any{"oauthAccount": map[string]any{"emailAddress": "a@example.com"}})
			write(filepath.Join(config, "quotas.json"), map[string]any{"claude/a@example.com": map[string]any{
				"at": at, "quota": provider.SubscriptionQuota{Provider: "claude", User: "a@example.com", Windows: []provider.QuotaWindow{{Name: "5 hours", Used: 100, ResetsAt: &reset}}},
			}})
			self, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(self, "-test.run=^TestAccountsCachedReading$")
			// A minimal environment prevents discovering real sign-ins or tools.
			cmd.Env = []string{"MAGPIE_TEST_ACCOUNTS_READING=" + mode, "HOME=" + home, "USERPROFILE=" + home,
				"XDG_CONFIG_HOME=" + filepath.Dir(config), "XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
				"CLAUDE_CONFIG_DIR=" + filepath.Join(home, ".claude"), "CODEX_HOME=" + filepath.Join(home, ".codex"), "PATH=" + home,
				"SystemRoot=" + os.Getenv("SystemRoot"), "USER=magpie-test", "NO_COLOR=1"}
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("accounts: %v: %s", err, out)
			}
			cached := strings.HasPrefix(mode, "cached")
			if strings.HasSuffix(mode, "json") {
				var rows []struct {
					AsOf    *time.Time           `json:"asOf"`
					Windows []provider.QuotaSpan `json:"windows"`
				}
				if err := json.Unmarshal(out, &rows); err != nil || len(rows) != 1 || len(rows[0].Windows) != 1 {
					t.Fatalf("accounts JSON: %s (%v)", out, err)
				}
				q := rows[0]
				if cached {
					if q.AsOf == nil || !q.AsOf.Equal(at) || q.Windows[0].Used != 100 || q.Windows[0].ResetsAt == nil || !q.Windows[0].ResetsAt.Equal(reset) {
						t.Fatalf("dated historical reading lost: %s", out)
					}
				} else if q.AsOf != nil || q.Windows[0].Used != 25 {
					t.Fatalf("fresh reading still marked cached: %s", out)
				}
			} else if cached {
				for _, want := range []string{"100%", "as of", "(cached)", "reset time passed", "current allowance unknown"} {
					if !strings.Contains(string(out), want) {
						t.Errorf("accounts missing %q: %s", want, out)
					}
				}
			} else if !strings.Contains(string(out), "25%") || strings.Contains(string(out), "(cached)") {
				t.Fatalf("fresh accounts output: %s", out)
			}
		})
	}
}

func TestUntilShort(t *testing.T) {
	for d, want := range map[time.Duration]string{
		-time.Minute:                  "now",
		45 * time.Minute:              "45m",
		2*time.Hour + 13*time.Minute:  "2h13m",
		76*time.Hour + 30*time.Minute: "3d4h",
	} {
		if got := untilShort(d); got != want {
			t.Errorf("%v: %q, want %q", d, got, want)
		}
	}
}

func TestQuotaCell(t *testing.T) {
	at := time.Now().Add(2*time.Hour + 13*time.Minute + 30*time.Second)
	got := quotaCell(quotaSpan{Name: "5 hours", Used: 42, ResetsAt: &at})
	if !strings.HasPrefix(got, "5h 42%") || !strings.Contains(got, "↻2h13m "+provider.ResetClock(at, time.Now())) {
		t.Fatalf("%q", got)
	}
}

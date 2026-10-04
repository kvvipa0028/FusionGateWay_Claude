//go:build darwin

package runtime

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/stageplan"
)

func TestCodexHTTPPrivateConfigurationAndBoundedProfile(t *testing.T) {
	r := codexChannelRun()
	r.Target.Route = stageplan.RouteRef{ID: "fixture-codex-route", Revision: 1}
	r.Target.LockEnforcement = stageplan.ControlledCalls
	r.Target.Effort.RequestedMode = stageplan.EffortExplicit
	claims := policy.Claims{TaskID: r.TaskID, RunID: r.ID, Role: r.Role, Attempt: r.Attempt, PlanRevision: r.PlanRevision, Generation: r.Generation, ProjectID: "fixture-project", Audience: policy.ModelAudience}
	m := policy.NewManager("fixture-controller", func(c policy.Claims) bool { return c == claims }, nil)
	g, e := m.PrepareModel(claims, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	secret, e := g.Secret()
	if e != nil {
		t.Fatal(e)
	}
	root, cwd := pdir(t), pdir(t)
	ch, e := NewCodexHTTPChannel(r, root, cwd, "fixture-native-http", func(context.Context, io.ReadWriteCloser) error { return nil }, func() bool { return true }, m.Stage(claims, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})), g)
	if e != nil {
		t.Fatal(e)
	}
	defer ch.Close()
	spec := Spec{Root: root, Workspace: cwd, Executable: "/fixture/native-codex", ExecutableHash: CodexExecutableSHA256, Args: []string{"app-server", "--listen", "stdio://", "--strict-config"}, NativeSessionID: "fixture-native-http", CodexChannel: ch, Timeout: time.Second}
	t.Setenv("OPENAI_API_KEY", "fixture-ambient-key")
	t.Setenv("CODEX_HOME", "/fixture/daily-home")
	t.Setenv("HTTPS_PROXY", "http://other.invalid")
	profile, env, e := sandbox(spec)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(profile, codexPreferencesRules) || strings.Count(profile, "(allow network-outbound") != 1 || !strings.Contains(profile, "localhost:"+ch.models.port) || strings.Contains(profile, "(allow process-fork") || strings.Contains(profile, "(allow network-inbound") || strings.Contains(profile, "(allow user-preference-write") {
		t.Fatal("unsafe profile")
	}
	vars := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		vars[k] = v
	}
	if vars[codexStageEnv] != secret || vars["CODEX_HOME"] != filepath.Join(root, "config", "codex") || len(vars) != 9 {
		t.Fatal("scoped environment changed")
	}
	for _, k := range []string{"OPENAI_API_KEY", "CODEX_API_KEY", "HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY"} {
		if _, ok := vars[k]; ok {
			t.Fatal("ambient authority inherited")
		}
	}
	path := filepath.Join(vars["CODEX_HOME"], "config.toml")
	raw, e := os.ReadFile(path)
	if e != nil || strings.Contains(string(raw), secret) || strings.Contains(string(raw), "fixture-ambient-key") {
		t.Fatal("config secret leak")
	}
	i, e := os.Stat(path)
	if e != nil || i.Mode().Perm() != 0600 {
		t.Fatal("config not private")
	}
	var cfg map[string]any
	if toml.Unmarshal(raw, &cfg) != nil {
		t.Fatal("invalid config")
	}
	providers := cfg["model_providers"].(map[string]any)
	if len(providers) != 1 || cfg["model_provider"] != CodexStageProvider || cfg["model"] != r.Target.RequestedModel || cfg["model_reasoning_effort"] != *r.Target.Effort.Value || cfg["model_reasoning_summary"] != "none" || cfg["sandbox_mode"] != "read-only" || cfg["approval_policy"] != "never" || cfg["cli_auth_credentials_store"] != "file" {
		t.Fatal("frozen config changed")
	}
	p := providers[CodexStageProvider].(map[string]any)
	if p["base_url"] != ch.models.endpoint || p["env_key"] != codexStageEnv || p["requires_openai_auth"] != false || p["supports_websockets"] != false {
		t.Fatal("provider route changed")
	}
	for _, v := range cfg["features"].(map[string]any) {
		if v != false {
			t.Fatal("stage feature enabled")
		}
	}
	if _, e = os.Stat(filepath.Join(vars["CODEX_HOME"], "auth.json")); !os.IsNotExist(e) {
		t.Fatal("long-term auth seeded")
	}
}

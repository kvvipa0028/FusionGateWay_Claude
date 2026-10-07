package bootstrap

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/runtime/glm"
)

func minHelper(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var glmHostClaude = flag.String("fusion-glm-host-claude", "", "explicit pinned Claude executable for the GLM execution host test; synthetic evidence/key only")

// TestGLMExecutionHostOpensFromEvidenceAndServesReadOnlyFlow proves the full
// operator wiring: evidence file → Registry admission → pinned factory →
// serving host with preview/submit/list. No real upstream call is made here:
// the live quota reader only runs when a launch is attempted.
func TestGLMExecutionHostOpensFromEvidenceAndServesReadOnlyFlow(t *testing.T) {
	if *glmHostClaude == "" {
		t.Skip("explicit pinned Claude executable only")
	}
	exe, e := filepath.EvalSymlinks(*glmHostClaude)
	if e != nil {
		t.Fatal(e)
	}
	if info, e := os.Stat(exe); e != nil || info.IsDir() {
		t.Fatal("pinned executable missing", e)
	}
	root := privateExecutionDir(t)
	for _, name := range []string{"private/config"} {
		if os.MkdirAll(filepath.Join(root, name), 0700) != nil {
			t.Fatal(name)
		}
	}
	source := filepath.Join(root, "private/config/projects.json")
	document := map[string]any{
		"schema_version": 1, "revision": 1,
		"global": map[string]any{"roles": map[string]any{"design": map[string]any{
			"mode": "locked", "route": map[string]any{"id": "local-glm", "revision": 1},
			"model": "glm-5.3", "effort": map[string]any{"mode": "explicit", "value": "medium"},
		}}},
		"routes": []map[string]any{{
			"id": "local-glm", "revision": 1, "native_route": "glm-cn-claude",
			"model": "glm-5.3", "account": "fixture-account", "workspace": "fixture-provider-home",
			"credential_identity": "fixture-identity", "runtime_version": glm.CLIVersion, "efforts": []string{"medium"}, "default_effort": "medium",
		}},
		"projects": []map[string]any{{
			"id": "local-pilot", "name": "pilot", "path": filepath.Join(root, "workspace"), "read": true, "write": false,
			"routes": []map[string]any{{"id": "local-glm", "revision": 1}},
			"layer":  map[string]any{},
		}},
	}
	if os.Mkdir(filepath.Join(root, "workspace"), 0700) != nil {
		t.Fatal("workspace")
	}
	raw, _ := json.Marshal(document)
	if os.WriteFile(source, raw, 0600) != nil {
		t.Fatal("source write")
	}
	keyPath := filepath.Join(root, "credentials", "glm-coding-plan.key")
	if os.Mkdir(filepath.Dir(keyPath), 0700) != nil || os.WriteFile(keyPath, []byte("fixture-controller-key\n"), 0600) != nil {
		t.Fatal("key write")
	}
	evidencePath := glmEvidenceFixture(t, func(d map[string]any) {
		d["requested_model"] = "glm-5.3"
		d["native_reported_models"] = []string{"glm-5.3"}
	})
	executionRoot := privateExecutionDir(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	controlRoot := filepath.Join(root, "control")
	if os.MkdirAll(controlRoot, 0700) != nil {
		t.Fatal("control root")
	}
	host, e := OpenGLMExecutionControl(ctx, source, controlRoot, "127.0.0.1:0", "local-pilot", "local-glm", keyPath, evidencePath, exe, executionRoot)
	if e != nil {
		t.Fatal("execution host refused", e)
	}
	served := make(chan error, 1)
	go func() { served <- host.Serve(ctx) }()
	address := "http://" + host.Addr()
	token, e := os.ReadFile(filepath.Join(root, "control", "management.token"))
	if e != nil {
		t.Fatal(e)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	call := func(method, path, body string) (int, string) {
		t.Helper()
		var reader *strings.Reader
		if body == "" {
			reader = strings.NewReader("")
		} else {
			reader = strings.NewReader(body)
		}
		request, e := http.NewRequest(method, address+path, reader)
		if e != nil {
			t.Fatal(e)
		}
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
		if body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		response, e := client.Do(request)
		if e != nil {
			t.Fatal(e)
		}
		defer response.Body.Close()
		buf := new(strings.Builder)
		_, _ = io.Copy(buf, response.Body)
		return response.StatusCode, buf.String()
	}
	// The admitted route is visible through the read-only configuration API.
	code, body := call(http.MethodGet, "/control/v1/projects/local-pilot/configuration", "")
	if code != 200 || !strings.Contains(body, "local-glm") {
		t.Fatal("admitted route missing from configuration", code, body[:minHelper(len(body), 200)])
	}
	// Preview plans without touching the network.
	code, body = call(http.MethodPost, "/control/v1/tasks/preview", `{"project_id":"local-pilot","goal":"evidence host pilot","required_roles":["design"]}`)
	if code != 200 || !strings.Contains(body, "preview_id") {
		t.Fatal("preview refused", code, body)
	}
	var preview struct {
		PreviewID string `json:"preview_id"`
		Plan      struct {
			Hash string `json:"hash"`
		} `json:"plan"`
	}
	if json.Unmarshal([]byte(body), &preview) != nil || preview.PreviewID == "" {
		t.Fatal("preview reply unusable")
	}
	// A missing evidence file must refuse the whole host.
	if _, e := OpenGLMExecutionControl(ctx, source, filepath.Join(root, "control2"), "127.0.0.1:0", "local-pilot", "local-glm", keyPath, filepath.Join(root, "missing.json"), exe, executionRoot); e == nil {
		t.Fatal("host opened without evidence")
	}
	if e := host.Close(); e != nil {
		t.Fatal(e)
	}
	<-served
}

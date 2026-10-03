package testsupport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProviderMarkersAndQuotaUnknown(t *testing.T) {
	s := NewProvider(Scenario{Mode: "quota_unknown", Model: "fixture-model-a", Account: "fixture-account-a", Revision: 1})
	defer s.Close()
	tr := NewTransport(s.URL)
	c := &http.Client{Transport: tr}
	r, e := c.Post(s.URL+"/chat", "application/json", strings.NewReader(`{"model":"fixture-model-a","account":"fixture-account-a","revision":1}`))
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	var got map[string]any
	if e = json.NewDecoder(r.Body).Decode(&got); e != nil {
		t.Fatal(e)
	}
	if got["model"] != "fixture-model-a" || got["account"] != "fixture-account-a" || got["quota"] != "unknown" || tr.Outbound() != 1 || s.Requests() != 1 {
		t.Fatalf("wrong fixture result: %v", got)
	}
}
func TestDeniedRequestsHaveZeroOutbound(t *testing.T) {
	tr := NewTransport("http://127.0.0.1:1")
	for _, url := range []string{"https://example.invalid/api", "http://localhost:1/", "http://127.0.0.1:2/"} {
		r, _ := http.NewRequest("GET", url, nil)
		if _, e := tr.RoundTrip(r); !errors.Is(e, ErrOutboundDenied) {
			t.Fatalf("not denied %s: %v", url, e)
		}
	}
	if tr.Outbound() != 0 || tr.Denied() != 3 {
		t.Fatal("denied path reached transport")
	}
}
func TestProviderFaultScenarios(t *testing.T) {
	for _, tc := range []struct {
		mode string
		code int
	}{{"rate_limit", 429}, {"error", 503}, {"config_changed", 409}} {
		t.Run(tc.mode, func(t *testing.T) {
			s := NewProvider(Scenario{Mode: tc.mode, Model: "fixture-model-a", Account: "fixture-account-a", Revision: 2})
			defer s.Close()
			r, e := (&http.Client{Transport: NewTransport(s.URL)}).Post(s.URL+"/chat", "application/json", strings.NewReader(`{"model":"fixture-model-a","account":"fixture-account-a","revision":1}`))
			if e != nil {
				t.Fatal(e)
			}
			defer r.Body.Close()
			if r.StatusCode != tc.code {
				t.Fatalf("status=%d", r.StatusCode)
			}
		})
	}
}
func TestProviderStreamToolAndInterruption(t *testing.T) {
	for _, mode := range []string{"stream", "broken_stream"} {
		t.Run(mode, func(t *testing.T) {
			s := NewProvider(Scenario{Mode: mode, Model: "fixture-model-a", Account: "fixture-account-a", Revision: 1})
			defer s.Close()
			r, e := (&http.Client{Transport: NewTransport(s.URL)}).Post(s.URL+"/chat", "application/json", strings.NewReader(`{"model":"fixture-model-a","account":"fixture-account-a","revision":1}`))
			if e != nil {
				t.Fatal(e)
			}
			defer r.Body.Close()
			b, e := io.ReadAll(r.Body)
			if !bytes.Contains(b, []byte("fixture-tool")) {
				t.Fatalf("tool marker absent: %q", b)
			}
			if mode == "broken_stream" {
				if e == nil || bytes.Contains(b, []byte("[DONE]")) {
					t.Fatal("interrupted stream appeared complete")
				}
			} else if e != nil || !bytes.Contains(b, []byte("[DONE]")) {
				t.Fatal("missing complete stream")
			}
		})
	}
}
func TestProviderDelayCanBeCancelled(t *testing.T) {
	s := NewProvider(Scenario{Mode: "ok", Model: "fixture-model-a", Account: "fixture-account-a", Delay: time.Second, Revision: 1})
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, "POST", s.URL+"/chat", strings.NewReader(`{"model":"fixture-model-a","account":"fixture-account-a","revision":1}`))
	_, e := (&http.Client{Transport: NewTransport(s.URL)}).Do(r)
	if !errors.Is(e, context.DeadlineExceeded) {
		t.Fatalf("delay not cancellable: %v", e)
	}
}
func TestProviderRefusesRealNamesAndAuthentication(t *testing.T) {
	s := NewProvider(Scenario{Mode: "ok", Model: "fixture-model-a", Account: "fixture-account-a", Revision: 1})
	defer s.Close()
	c := &http.Client{Transport: NewTransport(s.URL)}
	for _, tc := range []struct{ body, auth string }{{`{"model":"real-model","account":"fixture-account-a","revision":1}`, ""}, {`{"model":"fixture-model-a","account":"fixture-account-a","revision":1}`, "Bearer fixture-never-real"}} {
		r, _ := http.NewRequest("POST", s.URL+"/chat", strings.NewReader(tc.body))
		r.Header.Set("Authorization", tc.auth)
		res, e := c.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		res.Body.Close()
		if res.StatusCode != 400 {
			t.Fatal("unsafe fixture input accepted")
		}
	}
}
func TestRuntimeRPCMarkersToolsAndWriteLoss(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	if e := PrepareFixtureWorkspace(root); e != nil {
		t.Fatal(e)
	}
	for _, mode := range []string{"ok", "rpc", "tool", "write_loss", "crash", "config_changed", "quota_unknown", "rate_limit"} {
		t.Run(mode, func(t *testing.T) {
			req := RuntimeRequest{Model: "fixture-model-a", Account: "fixture-account-a", Mode: mode, Revision: 1, Workspace: root}
			input, _ := json.Marshal(req)
			var out bytes.Buffer
			code := ServeRuntime(bytes.NewReader(input), &out, nil)
			if mode == "write_loss" || mode == "crash" {
				if code == 0 || bytes.Contains(out.Bytes(), []byte(`"kind":"completed"`)) {
					t.Fatal("lost runtime completed")
				}
			} else if code != 0 {
				t.Fatalf("exit=%d %s", code, out.String())
			}
			if mode == "write_loss" {
				b, e := os.ReadFile(filepath.Join(root, "write-marker.txt"))
				if e != nil || string(b) != "fixture-written\n" {
					t.Fatal("write-loss absent")
				}
			}
			if mode == "tool" && !bytes.Contains(out.Bytes(), []byte("fixture-tool")) {
				t.Fatal("tool event absent")
			}
			if mode == "ok" || mode == "rpc" {
				if !bytes.Contains(out.Bytes(), []byte("fixture-model-a")) || !bytes.Contains(out.Bytes(), []byte("fixture-account-a")) {
					t.Fatal("identities absent")
				}
			}
		})
	}
}
func TestRuntimeRefusesUnsafeInput(t *testing.T) {
	for _, input := range []string{`{"model":"real","account":"fixture-account-a","mode":"ok","revision":1}`, `{"model":"fixture-model-a","account":"fixture-account-a","mode":"unknown","revision":1}`, `{"model":"fixture-model-a","account":"fixture-account-a","mode":"write_loss","revision":1,"workspace":"/"}`} {
		var out bytes.Buffer
		if code := ServeRuntime(strings.NewReader(input), &out, nil); code == 0 {
			t.Fatal("invalid fixture input accepted")
		}
	}
}
func TestFixtureEnvironmentDropsAmbientSecrets(t *testing.T) {
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "fixture-parent-secret")
	t.Setenv("OPENAI_API_KEY", "fixture-parent-secret")
	t.Setenv("HOME", "/fixture-parent-home")
	env, e := RuntimeEnvironment(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	all := strings.Join(env, "\n")
	if strings.Contains(all, "fixture-parent") || strings.Contains(all, "AUTH_TOKEN") || strings.Contains(all, "API_KEY") {
		t.Fatal("inherited sensitive environment")
	}
	if !strings.Contains(all, "FUSION_FIXTURE_RUNTIME=1") || !strings.Contains(all, "HOME=") {
		t.Fatal("missing isolation marker")
	}
}
func TestSyntheticRepoKnownBugAndEscapeFixtures(t *testing.T) {
	template := filepath.Join("..", "..", "..", "tests", "fusion", "fixtures", "synthetic-repo")
	root := filepath.Join(t.TempDir(), "repo")
	if e := CreateSyntheticRepo(template, root); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(root, "calc.go"))
	if e != nil || !bytes.Contains(b, []byte("return a - b")) {
		t.Fatal("bug absent")
	}
	info, e := os.Stat(filepath.Join(root, "readonly.txt"))
	if e != nil || info.Mode().Perm()&0222 != 0 {
		t.Fatal("read-only marker writable")
	}
	target, e := os.Readlink(filepath.Join(root, "escape-link"))
	if e != nil || !strings.Contains(target, "outside") {
		t.Fatal("escape fixture absent")
	}
	if e := CreateSyntheticRepo(template, root); e == nil {
		t.Fatal("existing repo overwritten")
	}
}

func TestFixtureEnvironmentRejectsLinkedRoot(t *testing.T) {
	parent := t.TempDir()
	link := filepath.Join(parent, "linked")
	if e := os.Symlink(t.TempDir(), link); e != nil {
		t.Fatal(e)
	}
	if _, e := RuntimeEnvironment(link); e == nil {
		t.Fatal("linked fixture root accepted")
	}
}
func TestProviderRejectsTrailingAndOversizedInput(t *testing.T) {
	s := NewProvider(Scenario{Mode: "ok", Model: "fixture-model-a", Account: "fixture-account-a", Revision: 1})
	defer s.Close()
	c := &http.Client{Transport: NewTransport(s.URL)}
	for _, body := range []string{`{"model":"fixture-model-a","account":"fixture-account-a","revision":1}{}`, `{"model":"fixture-model-a","account":"fixture-account-a","revision":1}` + strings.Repeat(" ", 5000)} {
		r, e := c.Post(s.URL+"/chat", "application/json", strings.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		r.Body.Close()
		if r.StatusCode != 400 {
			t.Fatal("unbounded/trailing request accepted")
		}
	}
}

//go:build darwin || linux

package bootstrap

import (
	"bufio"
	"context"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestControlHostAuthenticatedLoopbackAndRestart(t *testing.T) {
	path, _ := sourceFixture(t)
	root := filepath.Join(filepath.Dir(path), "control")
	h, e := OpenControl(path, root, "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	token, e := os.ReadFile(filepath.Join(root, "management.token"))
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(h.String(), string(token)) {
		t.Fatal("secret printed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.Serve(ctx) }()
	defer h.Close()
	request := func(secret, host, origin, path string) (int, string) {
		t.Helper()
		r, _ := http.NewRequest("GET", "http://"+h.Addr()+path, nil)
		r.Header.Set("Authorization", "Bearer "+secret)
		if host != "" {
			r.Host = host
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		resp, e := (&http.Client{Timeout: 3 * time.Second}).Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	for _, tc := range []struct {
		secret, host, origin string
		code                 int
	}{{"", "", "", 401}, {"wrong", "", "", 401}, {"fgs_stage", "", "", 403}, {strings.TrimSpace(string(token)), "evil.example", "", 403}, {strings.TrimSpace(string(token)), "", "https://evil.example", 403}, {strings.TrimSpace(string(token)), "", "http://" + h.Addr(), 200}} {
		code, _ := request(tc.secret, tc.host, tc.origin, "/control/v1/projects/fixture-project/configuration")
		if code != tc.code {
			t.Fatal("boundary", code, tc.code)
		}
	}
	code, body := request(strings.TrimSpace(string(token)), "", "", "/control/v1/projects/fixture-project/configuration")
	if code != 200 || !strings.Contains(body, `"admitted":false`) {
		t.Fatal("configuration", code)
	}
	code, _ = request(strings.TrimSpace(string(token)), "", "", "/v1/messages")
	if code != 404 {
		t.Fatal("legacy model exit", code)
	}
	cancel()
	select {
	case e = <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve failed to exit")
	}
	if e = h.Close(); e != nil {
		t.Fatal(e)
	}
	next, e := OpenControl(path, root, "127.0.0.1:0")
	if e != nil {
		t.Fatal("restart", e)
	}
	next.Close()
	after, _ := os.ReadFile(filepath.Join(root, "management.token"))
	if string(after) != string(token) {
		t.Fatal("restart silently rotated management")
	}
}

func TestControlHostPrivateLocationAndSourceRefusal(t *testing.T) {
	for _, kind := range []string{"invalid-source", "lan", "hostname", "port-overflow", "project-root", "public-root", "linked-root", "public-token", "stage-token"} {
		t.Run(kind, func(t *testing.T) {
			path, d := sourceFixture(t)
			root := filepath.Join(filepath.Dir(path), "control")
			addr := "127.0.0.1:0"
			switch kind {
			case "invalid-source":
				path += "missing"
			case "lan":
				addr = "0.0.0.0:0"
			case "hostname":
				addr = "localhost:0"
			case "port-overflow":
				addr = "127.0.0.1:65536"
			case "project-root":
				root = d.Projects[0].Path
			case "public-root":
				os.Mkdir(root, 0755)
			case "linked-root":
				os.Symlink(d.Projects[0].Path, root)
			case "public-token", "stage-token":
				os.Mkdir(root, 0700)
				token := strings.Repeat("a", 47)
				mode := os.FileMode(0644)
				if kind == "stage-token" {
					token = "fgs_" + strings.Repeat("a", 43)
					mode = 0600
				}
				os.WriteFile(filepath.Join(root, "management.token"), []byte(token), mode)
			}
			h, e := OpenControl(path, root, addr)
			if h != nil {
				h.Close()
			}
			if e != ErrControlHost {
				t.Fatal("refusal", e)
			}
		})
	}
}

func TestControlHostRevokesSourceAndTokenRotation(t *testing.T) {
	for _, kind := range []string{"source", "folder", "tasks", "token", "token-permissions"} {
		t.Run(kind, func(t *testing.T) {
			path, d := sourceFixture(t)
			root := filepath.Join(filepath.Dir(path), "control")
			h, e := OpenControl(path, root, "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			defer h.Close()
			token, _ := os.ReadFile(filepath.Join(root, "management.token"))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- h.Serve(ctx) }()
			switch kind {
			case "source":
				d.Revision++
				writeSource(t, path, d)
			case "folder":
				os.Rename(d.Projects[0].Path, d.Projects[0].Path+"old")
				os.Mkdir(d.Projects[0].Path, 0700)
			case "tasks":
				os.Rename(filepath.Join(root, "tasks"), filepath.Join(root, "old-tasks"))
				os.Mkdir(filepath.Join(root, "tasks"), 0700)
			case "token":
				os.Rename(filepath.Join(root, "management.token"), filepath.Join(root, "old.token"))
				os.WriteFile(filepath.Join(root, "management.token"), token, 0600)
			case "token-permissions":
				os.Chmod(filepath.Join(root, "management.token"), 0644)
			}
			req, _ := http.NewRequest("GET", "http://"+h.Addr()+"/control/v1/projects/fixture-project/configuration", nil)
			req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
			resp, e := (&http.Client{Timeout: 3 * time.Second}).Do(req)
			if e != nil {
				t.Fatal(e)
			}
			resp.Body.Close()
			if resp.StatusCode != 503 {
				t.Fatal("stale source accepted", resp.StatusCode)
			}
			cancel()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("stop")
			}
		})
	}
}

func TestControlHostWatcherRevokesExistingSSE(t *testing.T) {
	path, _ := sourceFixture(t)
	root := filepath.Join(filepath.Dir(path), "control")
	h, e := OpenControl(path, root, "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	p, _ := h.source.Project("fixture-project")
	c := p.Configuration
	// A synthetic persisted history fixture; the public registry stays unadmitted.
	c.Routes[0].Admitted = true
	c.Routes[0].BillingKnown = true
	c.Routes[0].LockEnforcement = stageplan.ControlledCalls
	plan, e := stageplan.Compile(1, []stageplan.Role{stageplan.Design}, c.Global, c.Project, stageplan.Layer{}, c.Routes)
	if e != nil {
		t.Fatal(e)
	}
	task, e := h.store.Create("fixture-history", store.CreateRequest{ProjectID: p.ID, Goal: "fixture history", Plan: plan})
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.Serve(ctx) }()
	token, _ := os.ReadFile(filepath.Join(root, "management.token"))
	req, _ := http.NewRequest("GET", "http://"+h.Addr()+"/agent/v1/tasks/"+task.ID+"/events", nil)
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	resp, e := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal("SSE unavailable", resp.StatusCode)
	}
	scanner := bufio.NewScanner(resp.Body)
	observed := false
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "data: ") {
			observed = true
			break
		}
	}
	if !observed {
		t.Fatal("missing persisted event")
	}
	if e = os.Chmod(filepath.Join(root, "management.token"), 0644); e != nil {
		t.Fatal(e)
	}
	end := make(chan struct{})
	go func() {
		for scanner.Scan() {
		}
		close(end)
	}()
	select {
	case <-end:
	case <-time.After(3 * time.Second):
		t.Fatal("existing SSE retained stale authority")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("serve stop")
	}
}

func TestControlHostExclusiveOwnershipAndClosedServe(t *testing.T) {
	path, _ := sourceFixture(t)
	root := filepath.Join(filepath.Dir(path), "control")
	h, e := OpenControl(path, root, "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	if other, e := OpenControl(path, root, "127.0.0.1:0"); e != ErrControlHost {
		if other != nil {
			other.Close()
		}
		t.Fatal("two controllers opened same store", e)
	}
	if e = h.Close(); e != nil {
		t.Fatal(e)
	}
	if e = h.Serve(context.Background()); e != ErrControlHost {
		t.Fatal("closed host served", e)
	}
	if e = h.Close(); e != nil {
		t.Fatal("repeat close", e)
	}
}

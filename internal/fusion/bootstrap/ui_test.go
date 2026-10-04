//go:build darwin || linux

package bootstrap

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestControlHostStageUIAssetsAreManagementProtected(t *testing.T) {
	path, _ := sourceFixture(t)
	h, err := OpenControl(path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serveExecutionHost(t, h)
	for _, path := range []string{"/fusion/", "/fusion/index.html", "/fusion/editor.mjs", "/fusion/workbench.mjs", "/fusion/model.mjs", "/fusion/editor.css", "/fusion/app.css"} {
		code, b, headers := hostHTTP(t, h, "GET", path, "", "", "")
		if code != 200 || len(b) == 0 || headers.Get("Cache-Control") != "no-store" || headers.Get("Content-Security-Policy") == "" {
			t.Fatal("authenticated stage asset", path, code)
		}
		if strings.Contains(string(b), "fgm_") {
			t.Fatal("credential in stage assets")
		}
	}
	if code, _, _ := hostHTTP(t, h, "GET", "/fusion/unknown.js", "", "", ""); code != 404 {
		t.Fatal("arbitrary asset", code)
	}
	if code, _, _ := hostHTTP(t, h, "GET", "/fusion/?key=fixture", "", "", ""); code != 403 {
		t.Fatal("query credential", code)
	}
	if code, _, _ := hostHTTP(t, h, "GET", "/fusion/?mode=window", "", "", ""); code != 400 {
		t.Fatal("asset query", code)
	}
	if code, _, _ := hostHTTP(t, h, "POST", "/fusion/", "{}", "", ""); code != 405 {
		t.Fatal("asset mutation", code)
	}
}

func TestControlHostStageUIRejectsOtherAuthorities(t *testing.T) {
	path, _ := sourceFixture(t)
	h, err := OpenControl(path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serveExecutionHost(t, h)
	token, err := os.ReadFile(filepath.Join(h.root, "management.token"))
	if err != nil {
		t.Fatal(err)
	}
	for _, asset := range []string{"/fusion/", "/fusion/editor.mjs", "/fusion/workbench.mjs", "/fusion/model.mjs", "/fusion/editor.css", "/fusion/app.css"} {
		for _, tc := range []struct {
			name, secret, host, origin, fetchSite string
			code                                  int
		}{
			{"missing", "", "", "", "", 401},
			{"wrong", "fixture-wrong", "", "", "", 401},
			{"stage", "fgs_fixture", "", "", "", 403},
			{"host", strings.TrimSpace(string(token)), "evil.example", "", "", 403},
			{"origin", strings.TrimSpace(string(token)), "", "https://evil.example", "", 403},
			{"fetch-site", strings.TrimSpace(string(token)), "", "", "cross-site", 403},
		} {
			t.Run(asset+"/"+tc.name, func(t *testing.T) {
				r, err := http.NewRequest("GET", "http://"+h.Addr()+asset, nil)
				if err != nil {
					t.Fatal(err)
				}
				if tc.secret != "" {
					r.Header.Set("Authorization", "Bearer "+tc.secret)
				}
				if tc.host != "" {
					r.Host = tc.host
				}
				if tc.origin != "" {
					r.Header.Set("Origin", tc.origin)
				}
				if tc.fetchSite != "" {
					r.Header.Set("Sec-Fetch-Site", tc.fetchSite)
				}
				response, err := (&http.Client{Timeout: 3 * time.Second}).Do(r)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				body, err := io.ReadAll(response.Body)
				if err != nil {
					t.Fatal(err)
				}
				if response.StatusCode != tc.code {
					t.Fatal("asset authority", response.StatusCode, tc.code)
				}
				if strings.Contains(string(body), strings.TrimSpace(string(token))) || strings.Contains(string(body), "阶段模型配置") {
					t.Fatal("asset or credential leaked")
				}
			})
		}
	}
}

func TestControlHostStageUIRevokesPrivateSource(t *testing.T) {
	for _, kind := range []string{"source", "token-permissions"} {
		t.Run(kind, func(t *testing.T) {
			path, d := sourceFixture(t)
			h, err := OpenControl(path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			serveExecutionHost(t, h)
			if code, _, _ := hostHTTP(t, h, "GET", "/fusion/", "", "", ""); code != 200 {
				t.Fatal("stage host was not ready", code)
			}
			if kind == "source" {
				d.Revision++
				writeSource(t, path, d)
			} else if err = os.Chmod(filepath.Join(h.root, "management.token"), 0644); err != nil {
				t.Fatal(err)
			}
			for _, asset := range []string{"/fusion/", "/fusion/editor.mjs", "/fusion/workbench.mjs", "/fusion/model.mjs", "/fusion/editor.css", "/fusion/app.css"} {
				if code, _, _ := hostHTTP(t, h, "GET", asset, "", "", ""); code != 503 {
					t.Fatal("stale asset source", code)
				}
			}
		})
	}
}

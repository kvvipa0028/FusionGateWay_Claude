//go:build darwin || linux

package bootstrap

import (
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

func nativeRequest(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, "wails://localhost"+path, strings.NewReader(body))
	r.RemoteAddr = "192.0.2.1:1234"
	r.URL.Scheme = ""
	r.URL.Host = ""
	r.Header.Set("X-Wails-Window-Id", "1")
	r.Header.Set("X-Wails-Window-Name", "fusion-stage-editor")
	return r
}

func TestNativeStageBridgeAcceptsSDKWindowHeaders(t *testing.T) {
	path, _ := sourceFixture(t)
	h, err := OpenControl(path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serveExecutionHost(t, h)
	bridge, err := newNativeTestBridge(t, h)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	r := nativeRequest("GET", "/fusion/", "")
	r.Header.Set("X-Wails-Window-Id", "1")
	r.Header.Set("X-Wails-Window-Name", "fusion-stage-editor")
	w := httptest.NewRecorder()
	bridge.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("real SDK window metadata rejected", w.Code)
	}
}

func newNativeTestBridge(t *testing.T, h *ControlHost) (*NativeStageBridge, error) {
	t.Helper()
	b, err := NewNativeStageBridge(h)
	if err == nil {
		err = b.BindNativeWindow(1)
	}
	return b, err
}

func TestNativeStageBridgeFreezesOwnedWindow(t *testing.T) {
	path, _ := sourceFixture(t)
	h, err := OpenControl(path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serveExecutionHost(t, h)
	b, err := NewNativeStageBridge(h)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	w := httptest.NewRecorder()
	b.ServeHTTP(w, nativeRequest("GET", "/fusion/", ""))
	if w.Code != 403 {
		t.Fatal("unbound bridge accepted request", w.Code)
	}
	if b.BindNativeWindow(0) == nil || b.BindNativeWindow(1) != nil || b.BindNativeWindow(2) == nil {
		t.Fatal("window binding did not freeze")
	}
	for _, tc := range []struct {
		name   string
		change func(*http.Request)
	}{
		{"missing-id", func(r *http.Request) { r.Header.Del("X-Wails-Window-Id") }},
		{"other-id", func(r *http.Request) { r.Header.Set("X-Wails-Window-Id", "2") }},
		{"missing-name", func(r *http.Request) { r.Header.Del("X-Wails-Window-Name") }},
		{"other-name", func(r *http.Request) { r.Header.Set("X-Wails-Window-Name", "other-window") }},
		{"duplicate-id", func(r *http.Request) { r.Header.Add("X-Wails-Window-Id", "1") }},
		{"duplicate-name", func(r *http.Request) { r.Header.Add("X-Wails-Window-Name", "fusion-stage-editor") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := nativeRequest("GET", "/fusion/", "")
			tc.change(r)
			w := httptest.NewRecorder()
			b.ServeHTTP(w, r)
			if w.Code != 403 {
				t.Fatal("window metadata expansion", w.Code)
			}
		})
	}
	b.Close()
	if b.BindNativeWindow(1) == nil {
		t.Fatal("closed bridge rebound")
	}
}

type nativeBodyReader struct {
	*io.PipeReader
	started chan struct{}
	once    sync.Once
}

func (r *nativeBodyReader) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.started) })
	return r.PipeReader.Read(p)
}

func TestNativeStageBridgeCloseCancelsAnUnfinishedBody(t *testing.T) {
	path, _ := sourceFixture(t)
	h, e := OpenControl(path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	serveExecutionHost(t, h)
	b, e := newNativeTestBridge(t, h)
	if e != nil {
		t.Fatal(e)
	}
	reader, writer := io.Pipe()
	defer writer.Close()
	defer reader.Close()
	started := make(chan struct{})
	r := nativeRequest("PUT", "/control/v1/defaults/global", "")
	r.Body = &nativeBodyReader{PipeReader: reader, started: started}
	r.Header.Set("If-Match", `"0"`)
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { b.ServeHTTP(w, r); close(done) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		writer.Close()
		<-done
		b.Close()
		t.Fatal("native body read did not start")
	}
	closed := make(chan struct{})
	go func() { b.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		writer.Close()
		<-closed
		t.Fatal("bridge close blocked on Native request body")
	}
	<-done
	if w.Code != 503 {
		t.Fatal("cancelled body", w.Code)
	}
	if code, body, _ := hostHTTP(t, h, "GET", "/control/v1/defaults/global", "", "", ""); code != 200 || !strings.Contains(string(body), `"revision":0`) {
		t.Fatal("cancelled request mutated Store", code)
	}
}

func TestNativeStageBridgeUsesOwnedAuthenticatedHost(t *testing.T) {
	path, _ := sourceFixture(t)
	h, e := OpenControl(path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	serveExecutionHost(t, h)
	bridge, e := newNativeTestBridge(t, h)
	if e != nil {
		t.Fatal(e)
	}
	defer bridge.Close()
	token, e := os.ReadFile(filepath.Join(h.root, "management.token"))
	if e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"/fusion/", "/fusion/editor.mjs", "/fusion/app.css", "/control/v1/projects", "/control/v1/projects/fixture-project/configuration", "/control/v1/defaults/global", "/control/v1/projects/fixture-project/presets"} {
		r := nativeRequest("GET", path, "")
		w := httptest.NewRecorder()
		bridge.ServeHTTP(w, r)
		if w.Code != 200 || w.Body.Len() == 0 {
			t.Fatal("native resource", path, w.Code)
		}
		if strings.Contains(w.Body.String(), strings.TrimSpace(string(token))) || strings.Contains(w.Header().Get("Location"), strings.TrimSpace(string(token))) {
			t.Fatal("authority leaked")
		}
		if r.Header.Get("Authorization") != "" {
			t.Fatal("caller request mutated")
		}
	}
	// Empty drafts are valid; retain the existing server validator for model input.
	body := `{"layer":{}}`
	r := nativeRequest("PUT", "/control/v1/projects/fixture-project/defaults", body)
	r.Header.Set("If-Match", `"0"`)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	bridge.ServeHTTP(w, r)
	if w.Code != 201 || w.Header().Get("ETag") != `"1"` {
		t.Fatal("native save", w.Code)
	}
	code, b, _ := hostHTTP(t, h, "GET", "/control/v1/projects/fixture-project/defaults", "", "", "")
	if code != 200 || !strings.Contains(string(b), `"revision":1`) {
		t.Fatal("persistent readback", code)
	}
	if strings.Contains(bridge.String(), strings.TrimSpace(string(token))) {
		t.Fatal("bridge string credential")
	}
}

func TestNativeStageBridgeRejectsNetworkAndAuthorityExpansion(t *testing.T) {
	path, _ := sourceFixture(t)
	h, e := OpenControl(path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	serveExecutionHost(t, h)
	bridge, e := newNativeTestBridge(t, h)
	if e != nil {
		t.Fatal(e)
	}
	defer bridge.Close()
	for _, tc := range []struct {
		name   string
		change func(*http.Request)
	}{
		{"real-network-peer", func(r *http.Request) { r.RemoteAddr = "127.0.0.1:9876" }},
		{"foreign-host", func(r *http.Request) { r.Host = "evil.example" }},
		{"foreign-origin", func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }},
		{"null-origin", func(r *http.Request) { r.Header.Set("Origin", "null") }},
		{"foreign-referer", func(r *http.Request) { r.Header.Set("Referer", "https://evil.example/") }},
		{"cross-site", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }},
		{"authority", func(r *http.Request) { r.Header.Set("Authorization", "Bearer fixture-untrusted") }},
		{"cookie", func(r *http.Request) { r.Header.Set("Cookie", "auth=fixture") }},
		{"forwarding", func(r *http.Request) { r.Header.Set("X-Forwarded-Host", "localhost") }},
		{"query", func(r *http.Request) { r.URL.RawQuery = "token=fixture" }},
		{"escaped-path", func(r *http.Request) { r.URL.RawPath = "/fusion/%69ndex.html" }},
		{"absolute-request", func(r *http.Request) { r.URL.Scheme = "https"; r.URL.Host = "evil.example" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := nativeRequest("GET", "/fusion/", "")
			tc.change(r)
			w := httptest.NewRecorder()
			bridge.ServeHTTP(w, r)
			if w.Code != 403 {
				t.Fatal("unsafe native request", w.Code)
			}
		})
	}
	for _, tc := range []struct{ method, path string }{{"POST", "/fusion/"}, {"GET", "/api/state"}, {"GET", "/wails/runtime.js"}, {"GET", "/v1/messages"}, {"GET", "/control/v1/tasks"}, {"POST", "/control/v1/tasks"}, {"POST", "/control/v1/tasks/fixture/stages/design/start"}, {"GET", "/control/v1/projects/unknown/configuration"}, {"DELETE", "/control/v1/defaults/global"}, {"GET", "/fusion/embed.go"}} {
		w := httptest.NewRecorder()
		bridge.ServeHTTP(w, nativeRequest(tc.method, tc.path, ""))
		if w.Code != 404 {
			t.Fatal("outside stage bridge scope", tc.method, tc.path, w.Code)
		}
	}
	r := nativeRequest("PUT", "/control/v1/defaults/global", strings.Repeat("x", (128<<10)+1))
	w := httptest.NewRecorder()
	bridge.ServeHTTP(w, r)
	if w.Code != 413 {
		t.Fatal("oversize", w.Code)
	}
	if code, body, _ := hostHTTP(t, h, "GET", "/control/v1/defaults/global", "", "", ""); code != 200 || !strings.Contains(string(body), `"revision":0`) {
		t.Fatal("rejected request mutated defaults", code)
	}
}

func TestNativeStageBridgeSavesVersionedPresetOnly(t *testing.T) {
	path, _ := sourceFixture(t)
	h, err := OpenControl(path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serveExecutionHost(t, h)
	b, err := newNativeTestBridge(t, h)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	resource := "/control/v1/projects/fixture-project/presets/native-preset"
	r := nativeRequest("PUT", resource, `{"name":"Native fixture","layer":{}}`)
	r.Header.Set("If-Match", `"0"`)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	if w.Code != 201 || w.Header().Get("ETag") != `"1"` || w.Header().Get("Location") != "" {
		t.Fatal("Native preset save", w.Code)
	}
	for _, p := range []string{resource, resource + "/versions/1"} {
		w := httptest.NewRecorder()
		b.ServeHTTP(w, nativeRequest("GET", p, ""))
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"revision":1`) {
			t.Fatal("preset readback", w.Code)
		}
	}
	if code, body, _ := hostHTTP(t, h, "GET", resource, "", "", ""); code != 200 || !strings.Contains(string(body), `"revision":1`) {
		t.Fatal("persistent preset", code)
	}
	for _, tc := range []struct{ method, path string }{
		{"DELETE", resource}, {"POST", resource}, {"PUT", resource + "/versions/1"}, {"PUT", "/control/v1/projects/unknown/presets/native-preset"}, {"PUT", "/control/v1/projects/fixture-project/presets"},
	} {
		w := httptest.NewRecorder()
		b.ServeHTTP(w, nativeRequest(tc.method, tc.path, `{}`))
		if w.Code != 404 {
			t.Fatal("preset authority expansion", tc.method, tc.path, w.Code)
		}
	}
}

func TestNativeStageBridgeRevocationAndClose(t *testing.T) {
	for _, kind := range []string{"source", "token", "bridge-close", "host-close"} {
		t.Run(kind, func(t *testing.T) {
			path, d := sourceFixture(t)
			h, e := OpenControl(path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			serveExecutionHost(t, h)
			bridge, e := newNativeTestBridge(t, h)
			if e != nil {
				t.Fatal(e)
			}
			defer bridge.Close()
			ready := httptest.NewRecorder()
			bridge.ServeHTTP(ready, nativeRequest("GET", "/fusion/", ""))
			if ready.Code != 200 {
				t.Fatal("Native host was not ready", ready.Code)
			}
			switch kind {
			case "source":
				d.Revision++
				writeSource(t, path, d)
			case "token":
				if e = os.Chmod(filepath.Join(h.root, "management.token"), 0644); e != nil {
					t.Fatal(e)
				}
			case "bridge-close":
				bridge.Close()
			case "host-close":
				h.Close()
			}
			w := httptest.NewRecorder()
			bridge.ServeHTTP(w, nativeRequest("GET", "/fusion/", ""))
			if w.Code != 503 {
				t.Fatal("revoked native bridge", w.Code)
			}
			if strings.Contains(w.Body.String(), "阶段模型配置") {
				t.Fatal("revoked assets served")
			}
		})
	}
	if _, e := NewNativeStageBridge(nil); e == nil {
		t.Fatal("nil host accepted")
	}
}

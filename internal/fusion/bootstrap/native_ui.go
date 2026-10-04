//go:build darwin || linux

package bootstrap

import (
	"context"
	"crypto/sha256"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// NativeStageBridge belongs only to a Wails virtual asset callback. It must
// never be mounted on an HTTP listener. It conveys no credential to the page.
type NativeStageBridge struct {
	mu        sync.Mutex
	closed    bool
	once      sync.Once
	active    sync.WaitGroup
	host      *ControlHost
	secret    string
	windowID  string
	client    *http.Client
	transport *http.Transport
	ctx       context.Context
	cancel    context.CancelFunc
}

func (*NativeStageBridge) String() string   { return "Fusion Native stage bridge (redacted)" }
func (*NativeStageBridge) GoString() string { return "NativeStageBridge(<redacted>)" }

// BindNativeWindow freezes the one window whose SDK metadata is accepted.
// The bridge remains inert until this trusted startup call succeeds.
func (b *NativeStageBridge) BindNativeWindow(id uint) error {
	if b == nil || id == 0 {
		return ErrControlHost
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.windowID != "" {
		return ErrControlHost
	}
	b.windowID = strconv.FormatUint(uint64(id), 10)
	return nil
}

func NewNativeStageBridge(h *ControlHost) (*NativeStageBridge, error) {
	if h == nil {
		return nil, ErrControlHost
	}
	h.mu.Lock()
	closed := h.closed
	h.mu.Unlock()
	if closed || !h.current() {
		return nil, ErrControlHost
	}
	record, e := readSource(filepath.Join(h.root, "management.token"))
	if e != nil || record.fileIdentity != h.tokenIdentity || sha256.Sum256(record.raw) != h.tokenDigest {
		return nil, ErrControlHost
	}
	ctx, cancel := context.WithCancel(context.Background())
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext, ResponseHeaderTimeout: 5 * time.Second, MaxConnsPerHost: 8}
	client := &http.Client{Transport: transport, Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &NativeStageBridge{host: h, secret: strings.TrimSuffix(string(record.raw), "\n"), client: client, transport: transport, ctx: ctx, cancel: cancel}, nil
}

func (b *NativeStageBridge) Close() {
	if b == nil {
		return
	}
	b.once.Do(func() {
		b.mu.Lock()
		b.closed = true
		b.secret = ""
		b.cancel()
		b.mu.Unlock()
		b.active.Wait()
		b.transport.CloseIdleConnections()
	})
}

func nativeStageRequest(r *http.Request) bool {
	if r == nil || r.URL == nil || r.RemoteAddr != "192.0.2.1:1234" || r.Host != "localhost" || r.URL.Scheme != "" || r.URL.Host != "" || r.URL.User != nil || r.URL.RawQuery != "" || r.URL.RawPath != "" || r.URL.Fragment != "" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != "wails://localhost" {
		return false
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	if referer := r.Header.Get("Referer"); referer != "" {
		u, e := url.Parse(referer)
		if e != nil || u.Scheme != "wails" || u.Host != "localhost" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "/fusion/" && u.Path != "/fusion/index.html") {
			return false
		}
	}
	for key := range r.Header {
		lower := strings.ToLower(key)
		if lower == "x-wails-window-id" || lower == "x-wails-window-name" {
			continue
		}
		if lower == "authorization" || lower == "cookie" || lower == "forwarded" || strings.HasPrefix(lower, "proxy-") || strings.HasPrefix(lower, "x-") {
			return false
		}
	}
	return true
}

func (b *NativeStageBridge) allowed(method, path string) bool {
	if method == "GET" || method == "HEAD" {
		switch path {
		case "/fusion/", "/fusion/index.html", "/fusion/editor.mjs", "/fusion/model.mjs", "/fusion/editor.css":
			return true
		}
	}
	if path == "/control/v1/projects" {
		return method == "GET"
	}
	if path == "/control/v1/defaults/global" {
		return method == "GET" || method == "PUT"
	}
	if path == "/control/v1/tasks/preview" {
		return method == "POST"
	}
	parts := strings.Split(strings.TrimPrefix(path, "/control/v1/projects/"), "/")
	if !strings.HasPrefix(path, "/control/v1/projects/") || len(parts) < 2 {
		return false
	}
	if _, ok := b.host.source.projects[parts[0]]; !ok {
		return false
	}
	if len(parts) == 2 {
		switch parts[1] {
		case "configuration", "presets", "tasks":
			return method == "GET"
		case "defaults":
			return method == "GET" || method == "PUT"
		}
	}
	if len(parts) == 4 && parts[1] == "tasks" && parts[2] == "before" && opaque(parts[3]) {
		return method == "GET"
	}
	if len(parts) == 3 && parts[1] == "presets" && parts[2] != "" {
		return method == "GET" || method == "PUT"
	}
	if len(parts) == 5 && parts[1] == "presets" && parts[2] != "" && parts[3] == "versions" && parts[4] != "" {
		return method == "GET"
	}
	return false
}

func (b *NativeStageBridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if b == nil {
		http.Error(w, "Service Unavailable", 503)
		return
	}
	if !nativeStageRequest(r) {
		http.Error(w, "Forbidden", 403)
		return
	}
	if !b.allowed(r.Method, r.URL.Path) {
		http.NotFound(w, r)
		return
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		http.Error(w, "Service Unavailable", 503)
		return
	}
	if b.windowID == "" || len(r.Header.Values("X-Wails-Window-Id")) != 1 || r.Header.Get("X-Wails-Window-Id") != b.windowID || len(r.Header.Values("X-Wails-Window-Name")) != 1 || r.Header.Get("X-Wails-Window-Name") != "fusion-stage-editor" {
		b.mu.Unlock()
		http.Error(w, "Forbidden", 403)
		return
	}
	b.active.Add(1)
	secret := b.secret
	b.mu.Unlock()
	defer b.active.Done()
	b.host.mu.Lock()
	closed := b.host.closed
	b.host.mu.Unlock()
	if closed || !b.host.current() {
		http.Error(w, "Service Unavailable", 503)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	stop := context.AfterFunc(b.ctx, cancel)
	defer stop()
	if r.Body != nil {
		closeBody := context.AfterFunc(ctx, func() { r.Body.Close() })
		defer closeBody()
	}
	var body []byte
	var e error
	if r.Body != nil {
		body, e = io.ReadAll(io.LimitReader(r.Body, (128<<10)+1))
	}
	if ctx.Err() != nil {
		http.Error(w, "Service Unavailable", 503)
		return
	}
	if e != nil {
		http.Error(w, "Bad Request", 400)
		return
	}
	if len(body) > 128<<10 {
		http.Error(w, "Request Entity Too Large", 413)
		return
	}
	request, e := http.NewRequestWithContext(ctx, r.Method, "http://"+b.host.Addr()+r.URL.Path, strings.NewReader(string(body)))
	if e != nil {
		http.Error(w, "Bad Request", 400)
		return
	}
	request.Header.Set("Authorization", "Bearer "+secret)
	for _, key := range []string{"Content-Type", "If-Match"} {
		for _, value := range r.Header.Values(key) {
			request.Header.Add(key, value)
		}
	}
	response, e := b.client.Do(request)
	if e != nil {
		http.Error(w, "Service Unavailable", 503)
		return
	}
	defer response.Body.Close()
	result, e := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if e != nil || len(result) > 2<<20 || response.StatusCode >= 300 && response.StatusCode < 400 {
		http.Error(w, "Service Unavailable", 503)
		return
	}
	for _, key := range []string{"Content-Type", "Cache-Control", "ETag", "X-Content-Type-Options", "Referrer-Policy", "Content-Security-Policy", "Allow"} {
		if value := response.Header.Get(key); value != "" {
			w.Header().Set(key, value)
		}
	}
	w.WriteHeader(response.StatusCode)
	if r.Method != "HEAD" {
		w.Write(result)
	}
}

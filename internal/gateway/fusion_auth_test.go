//go:build fusion

package gateway

import (
	"github.com/yetone/magpie/internal/fusion/testsupport"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFusionLegacyIngressDoesNotTrustLoopback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := New()
	tr := testsupport.NewTransport("http://127.0.0.1:1")
	s.client = &http.Client{Transport: tr}
	handler := s.Handler()
	for _, path := range []string{"/", "/v1/models", "/models", "/api/hello", "/v1/magpie/quotas", "/v1/magpie/route", "/v1/magpie/concurrency", "/v1/chat/completions", "/chat/completions", "/v1/responses", "/responses", "/v1/messages", "/messages", "/v1/systemone", "/v1/messages/count_tokens", "/v1/images/generations", "/images/edits", "/v1/videos", "/videos/fixture/content", "/_magpie/claude-mcp/fixture", "/v1beta/models/fixture:generateContent", "/v1/models/fixture"} {
		method := "GET"
		if strings.Contains(path, "messages") || strings.Contains(path, "responses") || strings.Contains(path, "completions") || strings.Contains(path, "systemone") || strings.Contains(path, "generations") || strings.Contains(path, "edits") || strings.Contains(path, "claude-mcp") || strings.Contains(path, "generateContent") {
			method = "POST"
		}
		r := httptest.NewRequest(method, "http://127.0.0.1:3426"+path, strings.NewReader(`{"model":"fixture-model-a","messages":[]}`))
		r.RemoteAddr = "127.0.0.1:40000"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Errorf("%s %s unauthenticated status=%d", method, path, w.Code)
		}
	}

	r := httptest.NewRequest("GET", "http://127.0.0.1:3426"+CodexPath+"/responses", nil)
	r.RemoteAddr = "127.0.0.1:40000"
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Connection", "Upgrade")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("WebSocket upgrade reached legacy backend")
	}
	if tr.Outbound() != 0 || tr.Denied() != 0 {
		t.Fatal("unauthenticated ingress reached model transport")
	}
}

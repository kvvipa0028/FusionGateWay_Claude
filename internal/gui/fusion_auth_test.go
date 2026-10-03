//go:build fusion

package gui

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFusionNativeAndGUIIngressDoesNotTrustLoopback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	h := Handler(nil, nil)
	for _, path := range []string{"/", "/boot.js", "/api/state", "/api/settings", "/api/set", "/api/sync", "/api/providers", "/api/agents/cli", "/api/window/quit", "/api/caller-keys", "/api/session/fixture", "/api/backup", "/api/fusion/tasks/fixture/events"} {
		r := httptest.NewRequest("GET", "http://127.0.0.1"+path, strings.NewReader(`{}`))
		r.RemoteAddr = "127.0.0.1:4000"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Errorf("native/GUI %s status=%d", path, w.Code)
		}
	}
}
func TestFusionOldWebEntryIsRefusedBeforeListeningOrQueryKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if web, e := StartWeb("127.0.0.1:0", "fixture-version"); e == nil {
		if web != nil {
			web.srv.Close()
		}
		t.Fatal("old query-key web listener started")
	}
}

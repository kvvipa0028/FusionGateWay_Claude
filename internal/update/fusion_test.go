//go:build fusion

package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestFusionCannotFetchUpstreamUpdate(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"version":"9.9.9","assets":{}}`))
	}))
	defer srv.Close()
	t.Setenv("MAGPIE_UPDATE_FEED", srv.URL)
	if _, err := Latest(context.Background()); err == nil {
		t.Error("upstream update was accepted")
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("denied update made %d outbound calls", got)
	}
}

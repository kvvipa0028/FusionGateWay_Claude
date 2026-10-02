package stats

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/settings"
)

// One event a day, under the same id, with nothing but the id, the version
// and the system; none once the user turns it off.
func TestSend(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("DO_NOT_TRACK", "")
	t.Setenv("MAGPIE_NO_STATS", "")
	var got []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var e map[string]any
		json.NewDecoder(r.Body).Decode(&e)
		if r.URL.Path != "/i/v0/e/" || e["api_key"] != Key {
			t.Errorf("sent %s %v", r.URL.Path, e)
		}
		got = append(got, e)
	}))
	defer srv.Close()
	t.Setenv("MAGPIE_STATS_HOST", srv.URL)
	day := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	for _, at := range []time.Time{day, day.Add(3 * time.Hour), day.Add(24 * time.Hour)} {
		if err := Send(context.Background(), "0.1.300", "app", at); err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != 2 {
		t.Fatalf("%d events for two days", len(got))
	}
	if got[0]["distinct_id"] != got[1]["distinct_id"] || len(got[0]["distinct_id"].(string)) != 32 {
		t.Fatalf("ids: %v %v", got[0]["distinct_id"], got[1]["distinct_id"])
	}
	p := got[0]["properties"].(map[string]any)
	if p["version"] != "0.1.300" || p["kind"] != "app" || len(p) != 5 {
		t.Fatalf("properties: %v", p)
	}
	settings.Save(settings.Settings{NoStats: true})
	Send(context.Background(), "0.1.300", "app", day.Add(48*time.Hour))
	t.Setenv("MAGPIE_NO_STATS", "1")
	settings.Save(settings.Settings{})
	Send(context.Background(), "0.1.300", "app", day.Add(72*time.Hour))
	if len(got) != 2 {
		t.Fatalf("sent while off: %d", len(got))
	}
}

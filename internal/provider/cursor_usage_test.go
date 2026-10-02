package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Cursor's current period comes back as its two pools and the total, each
// resetting when the billing cycle ends.
func TestCursorWindows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/aiserver.v1.DashboardService/GetCurrentPeriodUsage" || r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(rw, "no", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(rw).Encode(map[string]any{
			"billingCycleEnd": "1792833042000",
			"planUsage":       map[string]any{"autoPercentUsed": 12.5, "apiPercentUsed": 40, "totalPercentUsed": 20},
		})
	}))
	defer srv.Close()
	old := cursorBase
	cursorBase = srv.URL
	defer func() { cursorBase = old }()

	ws, err := cursorWindows(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 3 || ws[0].Used != 12.5 || ws[1].Used != 40 || ws[2].Used != 20 {
		t.Fatalf("windows = %+v", ws)
	}
	if ws[0].Name != "Cursor Models" || ws[1].Name != "Other Models" || ws[2].Name != "Total" {
		t.Fatalf("pool names = %+v", ws)
	}
	if ws[0].ResetsAt == nil || ws[0].ResetsAt.UnixMilli() != 1792833042000 {
		t.Fatalf("resets = %v", ws[0].ResetsAt)
	}
	if _, err := cursorWindows(context.Background(), "bad"); err == nil {
		t.Fatal("a refused token is an error")
	}
}

func TestCursorAllowanceUsesModelPool(t *testing.T) {
	now := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	resets := time.UnixMilli(1792833042000)
	for _, tc := range []struct {
		name                 string
		cursor, other, total float64
		models               []string
	}{
		{"other pool full", 25, 100, 100, []string{"default", "composer-2.5", "cursor-grok-4.5-high", "future-first-party", "Grok-4.8"}},
		{"cursor pool full", 100, 25, 100, nil},
		{"only aggregate full", 25, 40, 100, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"billingCycleEnd":  "1792833042000",
					"planUsage":        map[string]any{"autoPercentUsed": tc.cursor, "apiPercentUsed": tc.other, "totalPercentUsed": tc.total},
					"autoBucketModels": tc.models,
				})
			}))
			defer srv.Close()
			old := cursorBase
			cursorBase = srv.URL
			t.Cleanup(func() { cursorBase = old })
			ws, err := cursorWindows(t.Context(), "tok")
			if err != nil {
				t.Fatal(err)
			}
			a := allowanceOf(ws, now)
			firstParty := []string{"grok-4.7-xhigh-fast", "cursor-grok-4.7-high-fast", "cursor-grok-4.6-high-fast", "grok-4.5-fast-high", "auto", "default", "composer-2.5", "COMPOSER-2.5-FAST", "composer"}
			if tc.models != nil {
				firstParty = append(firstParty, "future-first-party", "grok-4.8-high", "cursor-grok-4.8-xhigh-fast")
			}
			for _, pool := range []struct {
				models []string
				used   float64
			}{
				{firstParty, tc.cursor},
				{[]string{"claude-opus-5-5", "gpt-5.6-sol", "gemini-3.1-pro", "grok-3", "grok-4.70", "grok-4.80-high", "unknown-model"}, tc.other},
			} {
				for _, model := range pool.models {
					if used, renews := a.For(model, now); used != pool.used || len(renews) != 1 || !renews[0].Equal(resets) {
						t.Errorf("%s: used %g, renews %v; want %g and one pool reset", model, used, renews, pool.used)
					}
					until := a.Full(model, 98, now)
					if pool.used < 98 && !until.IsZero() || pool.used >= 98 && !until.Equal(resets) {
						t.Errorf("%s: full until %v with %g%% used", model, until, pool.used)
					}
					if used, _ := a.For(model, resets); used != 0 || !a.Full(model, 98, resets).IsZero() {
						t.Errorf("%s: quota did not renew at its reset", model)
					}
				}
			}
		})
	}
}

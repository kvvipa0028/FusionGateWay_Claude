//go:build darwin || linux

package bootstrap

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/api"
	"github.com/yetone/magpie/internal/fusion/quota"
	"github.com/yetone/magpie/internal/fusion/runtime/glm"
)

func TestNativeQuotaUsesRegisteredScopeAndManualRefresh(t *testing.T) {
	path, _ := sourceFixture(t)
	key := quotaHostKey(t, path)
	var calls atomic.Int64
	h, e := OpenQuotaControl(context.Background(), path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0", glmQuotaFactory("fixture-project", "fixture-glm", key, func(c glm.QuotaReaderConfig) (quota.Fetch, error) {
		return func(ctx context.Context, i quota.Identity) (quota.Snapshot, error) {
			calls.Add(1)
			used := float64(10)
			now := time.Now().UTC()
			return quota.Snapshot{Identity: i, Source: "synthetic-native-quota", ObservedAt: &now, ReceivedAt: now, Status: quota.Unverified, Windows: []quota.Window{{Kind: "coding_plan", Unit: "percent", UsedPercent: &used}}}, nil
		}, nil
	}))
	if e != nil {
		t.Fatal(e)
	}
	serveExecutionHost(t, h)
	b, e := newNativeTestBridge(t, h)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	base := "/control/v1/projects/fixture-project/quota"
	w := nativeControlRequest(b, "GET", base, "", "", "")
	var reply api.QuotaReply
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.Routes[0].Status != quota.Unknown || calls.Load() != 0 {
		t.Fatal("cache path not wired", w.Code)
	}
	w = nativeControlRequest(b, "POST", base+"/fixture-glm/refresh", "{}", "", "")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.Routes[0].Snapshot == nil || reply.Routes[0].Status != quota.Unverified || calls.Load() != 1 {
		t.Fatal("manual refresh not wired", w.Code)
	}
	for _, tc := range []struct{ method, path string }{{"HEAD", base}, {"POST", base}, {"GET", base + "/fixture-glm/refresh"}, {"POST", base + "/unknown/refresh"}, {"POST", base + "/fixture-glm/refresh/extra"}, {"GET", "/control/v1/projects/unknown/quota"}} {
		w := nativeControlRequest(b, tc.method, tc.path, "{}", "", "")
		if w.Code != 404 {
			t.Fatal("quota authority widened", tc.method, tc.path, w.Code)
		}
	}
	w = nativeControlRequest(b, "POST", base+"/fixture-glm/refresh", `{"url":"https://untrusted.invalid"}`, "", "")
	if w.Code != 400 || calls.Load() != 1 {
		t.Fatal("query body selected upstream", w.Code)
	}
	if h.controller != nil {
		t.Fatal("quota bridge enabled Controller")
	}
	w = nativeControlRequest(b, "GET", "/fusion/quota.mjs", "", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "createQuotaView") {
		t.Fatal("Native quota resource unavailable", w.Code)
	}
}

func TestNativeQuotaDoesNotDiscloseOutsideOwnedWindowOrAfterSourceChange(t *testing.T) {
	path, source := sourceFixture(t)
	key := quotaHostKey(t, path)
	var calls atomic.Int64
	h, e := OpenQuotaControl(context.Background(), path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0", glmQuotaFactory("fixture-project", "fixture-glm", key, func(glm.QuotaReaderConfig) (quota.Fetch, error) {
		return func(_ context.Context, i quota.Identity) (quota.Snapshot, error) {
			calls.Add(1)
			now := time.Now().UTC()
			return quota.Snapshot{Identity: i, Source: "private-quota-observation", ObservedAt: &now, ReceivedAt: now, Status: quota.Unverified}, nil
		}, nil
	}))
	if e != nil {
		t.Fatal(e)
	}
	serveExecutionHost(t, h)
	b, e := newNativeTestBridge(t, h)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	pathRefresh := "/control/v1/projects/fixture-project/quota/fixture-glm/refresh"
	r := nativeRequest("POST", pathRefresh, "{}")
	r.Header.Set("X-Wails-Window-Id", "2")
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	if w.Code != 403 || calls.Load() != 0 {
		t.Fatal("foreign window queried quota", w.Code)
	}
	b.client.Transport = journalTrip{base: b.transport, after: func() { source.Revision++; writeSource(t, path, source) }}
	w = nativeControlRequest(b, "POST", pathRefresh, "{}", "", "")
	if w.Code != 503 || calls.Load() != 1 || strings.Contains(w.Body.String(), "private-quota-observation") {
		t.Fatal("late revoked quota disclosed", w.Code)
	}
}

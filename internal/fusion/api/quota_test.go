package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/quota"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

type quotaFixture struct {
	s      *Server
	h      http.Handler
	now    time.Time
	source QuotaSource
	live   atomic.Bool
	calls  atomic.Int64
	fail   atomic.Bool
}

func setupQuotaAPI(t *testing.T) *quotaFixture {
	t.Helper()
	s, _, h := setup(t)
	f := &quotaFixture{s: s, h: h, now: time.Now().UTC()}
	f.live.Store(true)
	i := quota.Identity{Provider: "fixture-provider", Account: "fixture-account", Workspace: "fixture-workspace", Region: "CN", Generation: 1}
	f.source = QuotaSource{Route: stageplan.RouteRef{ID: "fixture-route", Revision: 1}, Identity: i, Current: func(context.Context, quota.Identity) bool { return f.live.Load() }, Fetch: func(context.Context, quota.Identity) (quota.Snapshot, error) {
		f.calls.Add(1)
		if f.fail.Load() {
			return quota.Snapshot{}, errors.New("fixture-secret-query-error")
		}
		used := float64(30)
		return quota.Snapshot{Identity: i, Source: "fixture-quota", ObservedAt: &f.now, ReceivedAt: f.now, Pool: quota.Pool{ID: "fixture-pool", Provider: i.Provider, Region: i.Region, Scope: "account", Owner: i.Account, Verified: true}, Complete: true, Status: quota.Available, Windows: []quota.Window{{Name: "five-hour", Kind: "coding_plan", Unit: "percent", UsedPercent: &used, Raw: json.RawMessage(`{"used_percent":30}`)}}}, nil
	}}
	if e := s.SetQuotaSources("fixture-project", []QuotaSource{f.source}); e != nil {
		t.Fatal(e)
	}
	return f
}

const quotaPath = "/control/v1/projects/fixture-project/quota"
const quotaRefreshPath = quotaPath + "/fixture-route/refresh"

func quotaDecode(t *testing.T, body []byte) QuotaReply {
	t.Helper()
	var v QuotaReply
	if e := json.Unmarshal(body, &v); e != nil {
		t.Fatal(e)
	}
	return v
}
func TestQuotaAPICacheReadHasNoOutboundAndRefreshPreservesSourceAge(t *testing.T) {
	f := setupQuotaAPI(t)
	w := request(f.h, "GET", quotaPath, "", "", "fixture-management")
	v := quotaDecode(t, w.Body.Bytes())
	if w.Code != 200 || len(v.Routes) != 1 || v.Pools == nil || v.Routes[0].Status != quota.Unknown || v.Routes[0].Snapshot != nil || f.calls.Load() != 0 {
		t.Fatal("GET fetched or fabricated quota", w.Code, v)
	}
	w = request(f.h, "POST", quotaRefreshPath, `{}`, "", "fixture-management")
	v = quotaDecode(t, w.Body.Bytes())
	if w.Code != 200 || f.calls.Load() != 1 || v.Routes[0].Status != quota.Available || v.Routes[0].Snapshot.ObservedAt == nil || !v.Routes[0].Snapshot.ObservedAt.Equal(f.now) {
		t.Fatal("refresh lost source identity/time", w.Code, v)
	}
	f.s.now = func() time.Time { return f.now.Add(2 * time.Minute) }
	w = request(f.h, "GET", quotaPath, "", "", "fixture-management")
	v = quotaDecode(t, w.Body.Bytes())
	if w.Code != 200 || v.Routes[0].Status != quota.Stale || v.Routes[0].Snapshot.Status != quota.Stale || v.Pools[0].Snapshot.Status != quota.Stale || f.calls.Load() != 1 {
		t.Fatal("cache rejuvenated stale capacity", v)
	}
}
func TestQuotaAPIRejectsUnsafeCallsBeforeFetch(t *testing.T) {
	f := setupQuotaAPI(t)
	for _, x := range []struct {
		path, body, credential string
		code                   int
	}{
		{quotaRefreshPath, `{}`, "", 401}, {quotaRefreshPath, `{}`, "wrong", 401}, {quotaRefreshPath, `{}`, "fgs_fixture", 403},
		{quotaRefreshPath, `{"token":"fixture-secret"}`, "fixture-management", 400}, {quotaRefreshPath, `{"workspace":"/tmp"}`, "fixture-management", 400}, {quotaRefreshPath, `{"route":"other"}`, "fixture-management", 400},
		{quotaPath + "/missing/refresh", `{}`, "fixture-management", 404}, {strings.Replace(quotaRefreshPath, "fixture-project", "missing", 1), `{}`, "fixture-management", 404}, {quotaRefreshPath + "?token=fixture", `{}`, "fixture-management", 403},
	} {
		if w := request(f.h, "POST", x.path, x.body, "", x.credential); w.Code != x.code {
			t.Errorf("expected %d got %d", x.code, w.Code)
		}
	}
	if f.calls.Load() != 0 {
		t.Fatal("denied request fetched quota")
	}
}
func TestQuotaAPIRevokedSourceAndConfigurationCannotReturnCachedAvailability(t *testing.T) {
	f := setupQuotaAPI(t)
	if w := request(f.h, "POST", quotaRefreshPath, `{}`, "", "fixture-management"); w.Code != 200 {
		t.Fatal(w.Code)
	}
	f.live.Store(false)
	w := request(f.h, "GET", quotaPath, "", "", "fixture-management")
	v := quotaDecode(t, w.Body.Bytes())
	if w.Code != 200 || v.Routes[0].Status != quota.Unverified || v.Routes[0].Snapshot != nil {
		t.Fatal("revoked identity retained availability", v)
	}
	if w = request(f.h, "POST", quotaRefreshPath, `{}`, "", "fixture-management"); w.Code != 409 || f.calls.Load() != 1 {
		t.Fatal("revoked source queried", w.Code)
	}
	f.live.Store(true)
	if e := f.s.SetProject("fixture-project", configuration()); e != nil {
		t.Fatal(e)
	}
	w = request(f.h, "GET", quotaPath, "", "", "fixture-management")
	v = quotaDecode(t, w.Body.Bytes())
	if w.Code != 200 || v.ConfigurationRevision != 2 || v.Routes[0].Status != quota.Unverified || v.Routes[0].Snapshot != nil {
		t.Fatal("new project borrowed old query authority", v)
	}
	if w = request(f.h, "POST", quotaRefreshPath, `{}`, "", "fixture-management"); w.Code != 409 || f.calls.Load() != 1 {
		t.Fatal("stale registration queried", w.Code)
	}
}
func TestQuotaAPIFailedRefreshNeverLeaksErrorsOrFreshensCache(t *testing.T) {
	f := setupQuotaAPI(t)
	request(f.h, "POST", quotaRefreshPath, `{}`, "", "fixture-management")
	f.fail.Store(true)
	w := request(f.h, "POST", quotaRefreshPath, `{}`, "", "fixture-management")
	v := quotaDecode(t, w.Body.Bytes())
	if w.Code != 200 || v.Routes[0].Status != quota.Stale || strings.Contains(w.Body.String(), "fixture-secret") || v.Routes[0].Error == nil {
		t.Fatal("failed refresh looked fresh or exposed error", w.Code, v)
	}
	w = request(f.h, "GET", quotaPath, "", "", "fixture-management")
	if v = quotaDecode(t, w.Body.Bytes()); v.Routes[0].Status != quota.Stale || f.calls.Load() != 2 {
		t.Fatal("GET retried failed query", v)
	}
}
func TestQuotaAPIConcurrentRefreshIsCoalescedAndRevocationDiscardsResult(t *testing.T) {
	f := setupQuotaAPI(t)
	entered, release := make(chan struct{}), make(chan struct{})
	source := f.source
	var calls atomic.Int64
	source.Fetch = func(ctx context.Context, i quota.Identity) (quota.Snapshot, error) {
		calls.Add(1)
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			return quota.Snapshot{}, ctx.Err()
		}
		return f.source.Fetch(ctx, i)
	}
	if e := f.s.SetProject("fixture-project", configuration()); e != nil {
		t.Fatal(e)
	}
	if e := f.s.SetQuotaSources("fixture-project", []QuotaSource{source}); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	codes := make(chan int, 8)
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- request(f.h, "POST", quotaRefreshPath, `{}`, "", "fixture-management").Code
		}()
	}
	<-entered
	f.s.auth.RevokeManagement()
	close(release)
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != 401 {
			t.Fatal("revoked management received quota", code)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("same identity refresh not coalesced", calls.Load())
	}
}

func TestQuotaAPIFirstFailureRetainsAuthStatusWithoutSnapshot(t *testing.T) {
	f := setupQuotaAPI(t)
	source := f.source
	source.Fetch = func(context.Context, quota.Identity) (quota.Snapshot, error) {
		return quota.Snapshot{}, &quota.QueryError{Status: quota.AuthRequired}
	}
	if e := f.s.SetProject("fixture-project", configuration()); e != nil {
		t.Fatal(e)
	}
	if e := f.s.SetQuotaSources("fixture-project", []QuotaSource{source}); e != nil {
		t.Fatal(e)
	}
	for _, method := range []string{"POST", "GET"} {
		path, body := quotaPath, ""
		if method == "POST" {
			path, body = quotaRefreshPath, `{}`
		}
		w := request(f.h, method, path, body, "", "fixture-management")
		v := quotaDecode(t, w.Body.Bytes())
		if w.Code != 200 || v.Routes[0].Status != quota.AuthRequired || v.Routes[0].Snapshot != nil {
			t.Fatal("missing first observation hid auth failure", v)
		}
	}
}

func TestQuotaAPIRegistrationRequiresExactRouteAndCannotBeHotReplaced(t *testing.T) {
	f := setupQuotaAPI(t)
	if e := f.s.SetQuotaSources("fixture-project", []QuotaSource{f.source}); !errors.Is(e, store.ErrConflict) {
		t.Fatal("source hot replaced", e)
	}
	if e := f.s.SetProject("fixture-project", configuration()); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*QuotaSource){
		func(q *QuotaSource) { q.Route.Revision++ }, func(q *QuotaSource) { q.Identity.Account = "other" }, func(q *QuotaSource) { q.Identity.Workspace = "other" }, func(q *QuotaSource) { q.Identity.Generation = 0 }, func(q *QuotaSource) { q.Current = nil },
	} {
		source := f.source
		change(&source)
		if e := f.s.SetQuotaSources("fixture-project", []QuotaSource{source}); e == nil {
			t.Fatal("unsafe quota source registered")
		}
	}
	if e := f.s.SetQuotaSources("fixture-project", []QuotaSource{f.source, f.source}); e == nil {
		t.Fatal("duplicate source registered")
	}
	source := f.source
	source.Fetch = nil
	if e := f.s.SetQuotaSources("fixture-project", []QuotaSource{source}); e != nil {
		t.Fatal(e)
	}
	w := request(f.h, "GET", quotaPath, "", "", "fixture-management")
	v := quotaDecode(t, w.Body.Bytes())
	if v.Routes[0].Status != quota.Unsupported || v.Routes[0].Snapshot != nil {
		t.Fatal("unsupported query appeared available", v)
	}
	w = request(f.h, "POST", quotaRefreshPath, `{}`, "", "fixture-management")
	if w.Code != 503 || f.calls.Load() != 0 || !strings.Contains(w.Body.String(), "quota_query_unsupported") {
		t.Fatal("unsupported source called", w.Code)
	}
}

func TestQuotaAPIServiceSlotsRemainOccupiedUntilFetchActuallyReturns(t *testing.T) {
	f := setupQuotaAPI(t)
	entered, release := make(chan struct{}), make(chan struct{})
	source := f.source
	source.Fetch = func(ctx context.Context, i quota.Identity) (quota.Snapshot, error) {
		close(entered)
		<-release
		return f.source.Fetch(ctx, i)
	}
	if e := f.s.SetProject("fixture-project", configuration()); e != nil {
		t.Fatal(e)
	}
	if e := f.s.SetQuotaSources("fixture-project", []QuotaSource{source}); e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("POST", quotaRefreshPath, strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer fixture-management")
	ctx, cancel := context.WithCancel(r.Context())
	r = r.WithContext(ctx)
	done := make(chan struct{})
	go func() { f.h.ServeHTTP(httptest.NewRecorder(), r); close(done) }()
	<-entered
	cancel()
	<-done
	if len(f.s.quotaSlots) != 1 {
		t.Fatal("HTTP cancellation released a live fetch slot")
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for len(f.s.quotaSlots) != 0 && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if len(f.s.quotaSlots) != 0 {
		t.Fatal("finished fetch retained its slot")
	}
}

func TestQuotaAPIServiceCapacityRejectsBeforeFetch(t *testing.T) {
	f := setupQuotaAPI(t)
	for n := 0; n < cap(f.s.quotaSlots); n++ {
		f.s.quotaSlots <- struct{}{}
	}
	w := request(f.h, "POST", quotaRefreshPath, `{}`, "", "fixture-management")
	if w.Code != 429 || f.calls.Load() != 0 || !strings.Contains(w.Body.String(), "quota_refresh_capacity_reached") {
		t.Fatal("service capacity not enforced", w.Code)
	}
	for len(f.s.quotaSlots) > 0 {
		<-f.s.quotaSlots
	}
	w = request(f.h, "POST", quotaRefreshPath, `{}`, "", "fixture-management")
	if w.Code != 200 || f.calls.Load() != 1 {
		t.Fatal("capacity refusal poisoned future reads", w.Code)
	}
}

func TestQuotaAPISharedPoolAliasesAreNotAddedAndPartialNewerReadWins(t *testing.T) {
	f := setupQuotaAPI(t)
	c := configuration()
	route := c.Routes[0]
	route.ID = "fixture-route-b"
	route.Model = "fixture-model-b"
	c.Routes = append(c.Routes, route)
	if e := f.s.SetProject("fixture-project", c); e != nil {
		t.Fatal(e)
	}
	b := f.source
	b.Route.ID = route.ID
	b.Fetch = func(ctx context.Context, i quota.Identity) (quota.Snapshot, error) {
		snapshot, e := f.source.Fetch(ctx, i)
		later := f.now.Add(time.Second)
		snapshot.ObservedAt = &later
		snapshot.ReceivedAt = later
		snapshot.Complete = false
		return snapshot, e
	}
	if e := f.s.SetQuotaSources("fixture-project", []QuotaSource{f.source, b}); e != nil {
		t.Fatal(e)
	}
	f.s.now = func() time.Time { return f.now.Add(2 * time.Second) }
	for _, path := range []string{quotaRefreshPath, quotaPath + "/fixture-route-b/refresh"} {
		if w := request(f.h, "POST", path, `{}`, "", "fixture-management"); w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	w := request(f.h, "GET", quotaPath, "", "", "fixture-management")
	v := quotaDecode(t, w.Body.Bytes())
	if w.Code != 200 || len(v.Routes) != 2 || len(v.Pools) != 1 || len(v.Pools[0].Aliases) != 2 || v.Pools[0].Snapshot.Status != quota.Unverified || *v.Pools[0].Snapshot.Windows[0].UsedPercent != 30 {
		t.Fatal("aliases summed or incomplete data hidden", v)
	}
}

func TestQuotaAPIActualHTTPRefreshUsesRegisteredSource(t *testing.T) {
	f := setupQuotaAPI(t)
	server := httptest.NewServer(f.h)
	defer server.Close()
	r, e := http.NewRequest("POST", server.URL+quotaRefreshPath, strings.NewReader(`{}`))
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer fixture-management")
	res, e := server.Client().Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	var v QuotaReply
	if e = json.NewDecoder(res.Body).Decode(&v); e != nil {
		t.Fatal(e)
	}
	if res.StatusCode != 200 || v.Routes[0].Status != quota.Available || f.calls.Load() != 1 || res.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("actual HTTP refresh mismatch", res.StatusCode, v)
	}
}

func TestQuotaAPIWorkerPreservesIssuerContextAndBoundedDeadline(t *testing.T) {
	f := setupQuotaAPI(t)
	source := f.source
	source.Fetch = func(ctx context.Context, i quota.Identity) (quota.Snapshot, error) {
		deadline, ok := ctx.Deadline()
		if !f.s.auth.ManagementCurrent(ctx) || !ok || time.Until(deadline) > 10*time.Second {
			return quota.Snapshot{}, errors.New("fixture missing bounded query authority")
		}
		return f.source.Fetch(ctx, i)
	}
	if e := f.s.SetProject("fixture-project", configuration()); e != nil {
		t.Fatal(e)
	}
	if e := f.s.SetQuotaSources("fixture-project", []QuotaSource{source}); e != nil {
		t.Fatal(e)
	}
	w := request(f.h, "POST", quotaRefreshPath, `{}`, "", "fixture-management")
	v := quotaDecode(t, w.Body.Bytes())
	if w.Code != 200 || v.Routes[0].Status != quota.Available || f.calls.Load() != 1 {
		t.Fatal("worker lost local issuer grant or deadline", v)
	}
}

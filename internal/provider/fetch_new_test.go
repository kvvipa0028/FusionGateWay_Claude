package provider

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/catalog"
)

// An account signed in while magpie runs gets its vendor's list the next
// time the providers are listed, not only after Refresh (#204: WorkBuddy's
// models incomplete when first added, all of them after Refresh). One whose
// list couldn't be had isn't asked again on each listing.
func TestFetchNewAccount(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	wbTokens.Lock()
	wbTokens.m = map[string]wbCreds{}
	wbTokens.Unlock()
	newFetches.Lock()
	newFetches.m = map[string]time.Time{}
	newFetches.Unlock()

	var asked atomic.Int32
	var down atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/config" {
			w.WriteHeader(404)
			return
		}
		asked.Add(1)
		if down.Load() {
			w.WriteHeader(502)
			return
		}
		w.Write([]byte(`{"code":0,"data":{"agents":[{"name":"cli","models":["hy4-preview","gpt-6-astra","glm-5.3"]}],
			"models":[{"id":"hy4-preview","name":"Hy4 preview"},{"id":"gpt-6-astra","name":"GPT-6-Astra"},{"id":"glm-5.3","name":"GLM-5.3"}]}}`))
	}))
	defer srv.Close()
	old := wbEndpoint
	wbEndpoint = srv.URL
	defer func() { wbEndpoint = old }()

	ids := func() map[string]bool {
		p, err := Find("workbuddy")
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, m := range p.Available() {
			out[m.ID] = true
		}
		return out
	}

	// nothing signed in yet: nothing asked
	FetchNew(time.Second)
	if n := asked.Load(); n != 0 {
		t.Fatalf("asked %d times with no account", n)
	}

	// WorkBuddy signs itself in while magpie runs
	future := time.Now().Add(24 * time.Hour).UnixMilli()
	writeFile(t, wbAuthFile(home), map[string]any{
		"account": map[string]any{"uid": "u1", "nickname": "旅行者"},
		"auth": map[string]any{"accessToken": "a", "refreshToken": "r",
			"expiresAt": future, "refreshExpiresAt": future, "domain": "www.codebuddy.cn"},
	})
	forgetAccountCaches()
	if ids()["hy4-preview"] {
		t.Fatal("the vendor's model listed before its list was fetched")
	}
	FetchNew(5 * time.Second)
	got := ids()
	if !got["hy4-preview"] || !got["gpt-6-astra"] || len(got) != 3 {
		t.Fatalf("after FetchNew: %v", got)
	}
	FetchNew(5 * time.Second)
	if n := asked.Load(); n != 1 {
		t.Fatalf("a fetched list asked for again: %d", n)
	}

	// a list that can't be had is asked for once, and again only later
	if err := catalog.SaveLive("workbuddy", "", nil); err != nil {
		t.Fatal(err)
	}
	newFetches.Lock()
	newFetches.m = map[string]time.Time{}
	newFetches.Unlock()
	down.Store(true)
	FetchNew(5 * time.Second)
	FetchNew(5 * time.Second)
	if n := asked.Load(); n != 2 {
		t.Fatalf("a failing vendor asked %d times, want once more", n-1)
	}
	oldRetry := newFetchRetry
	newFetchRetry = 0
	defer func() { newFetchRetry = oldRetry }()
	down.Store(false)
	FetchNew(5 * time.Second)
	if n := asked.Load(); n != 3 || !ids()["hy4-preview"] {
		t.Fatalf("not asked again after the retry time: %d %v", n, ids())
	}
}

package policy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// Store workflow writes recheck management authority while holding Store.mu.
// Stage validation can hold Manager.mu while waiting for that same Store. The
// management recheck must not wait for the stage validator to finish.
func TestManagementCurrentDoesNotWaitForStageStoreValidation(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	m := NewManager("fixture-management", func(c Claims) bool {
		close(entered)
		<-release
		return c == fixtureClaims()
	}, nil)
	var saved context.Context
	h := m.Management(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		saved = r.Context()
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer fixture-management")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent || saved == nil {
		t.Fatal("management fixture did not authenticate")
	}
	issued := make(chan error, 1)
	go func() {
		_, err := m.Issue(fixtureClaims(), time.Minute)
		issued <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("stage validator did not enter")
	}
	current := make(chan bool, 1)
	go func() { current <- m.ManagementCurrent(saved) }()
	completed := false
	select {
	case allowed := <-current:
		completed = true
		if !allowed {
			t.Error("active authenticated management context rejected")
		}
	case <-time.After(time.Second):
		t.Error("management recheck waited on stage validator; Store/Manager locks can deadlock")
	}
	unblock()
	select {
	case err := <-issued:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stage issuance did not finish after release")
	}
	if !completed {
		select {
		case <-current:
		case <-time.After(2 * time.Second):
			t.Fatal("management recheck did not finish after release")
		}
	}
	m.RevokeManagement()
	if m.ManagementCurrent(saved) {
		t.Fatal("revocation did not invalidate management context")
	}
}

package policy

import (
	"context"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func fixtureClaims() Claims {
	return Claims{TaskID: "fixture-task", RunID: "fixture-run", Role: stageplan.Design, Attempt: 1, PlanRevision: 1, Generation: 1, ProjectID: "fixture-project", Audience: ModelAudience}
}

func TestExecutionEnabledIsRevocationStateAndCannotAuthenticateManagement(t *testing.T) {
	m := NewManager("fixture-management", nil, nil)
	if !m.ExecutionEnabled() || m.ManagementCurrent(context.Background()) {
		t.Fatal("enabled state granted management context")
	}
	calls := 0
	h := m.Management(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/", nil))
	if calls != 0 || w.Code < 400 {
		t.Fatal("enabled state bypassed HTTP authentication")
	}
	m.RevokeManagement()
	if m.ExecutionEnabled() {
		t.Fatal("revocation did not disable continuation")
	}
}
func TestStageCredentialScopeRevocationAndExpiry(t *testing.T) {
	valid := true
	m := NewManager("fixture-management-secret", func(c Claims) bool { return valid && c == fixtureClaims() }, []string{"http://127.0.0.1:3426"})
	raw, e := m.Issue(fixtureClaims(), time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	for _, edit := range []func(*Claims){func(c *Claims) { c.TaskID = "other" }, func(c *Claims) { c.RunID = "other" }, func(c *Claims) { c.Role = stageplan.Acceptance }, func(c *Claims) { c.Attempt++ }, func(c *Claims) { c.PlanRevision++ }, func(c *Claims) { c.Generation++ }, func(c *Claims) { c.ProjectID = "other" }, func(c *Claims) { c.Audience = EventsAudience }} {
		scope := fixtureClaims()
		edit(&scope)
		if _, e = m.AuthenticateStage(raw, scope); e == nil {
			t.Fatal("cross-scope stage credential accepted")
		}
	}
	if _, e = m.AuthenticateStage(raw, fixtureClaims()); e != nil {
		t.Fatal(e)
	}
	valid = false
	if _, e = m.AuthenticateStage(raw, fixtureClaims()); e == nil {
		t.Fatal("invalidated runtime scope remained valid")
	}
	valid = true
	m.RevokeRun("fixture-run")
	if _, e = m.AuthenticateStage(raw, fixtureClaims()); e == nil {
		t.Fatal("revoked credential accepted")
	}
	if _, e = m.Issue(fixtureClaims(), time.Minute); e == nil {
		t.Fatal("revoked run reissued a credential")
	}
	m = NewManager("fixture-management-secret", func(Claims) bool { return true }, nil)
	raw, e = m.Issue(fixtureClaims(), time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	m.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	if _, e = m.AuthenticateStage(raw, fixtureClaims()); e == nil {
		t.Fatal("expired credential accepted")
	}
	fresh := NewManager("fixture-management-secret", func(Claims) bool { return true }, nil)
	if _, e = fresh.AuthenticateStage(raw, fixtureClaims()); e == nil {
		t.Fatal("credential survived issuer restart")
	}
}
func TestWorkerCannotFallBackToManagementOrChangeRoleHeaders(t *testing.T) {
	m := NewManager("fixture-management-secret", func(c Claims) bool { return c == fixtureClaims() }, nil)
	raw, e := m.Issue(fixtureClaims(), time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	manage := m.Management(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(204) }))
	for _, token := range []string{"", raw, "wrong"} {
		r := httptest.NewRequest("POST", "http://127.0.0.1/api/tasks", nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		manage.ServeHTTP(w, r)
		if w.Code != 401 && w.Code != 403 {
			t.Fatal("worker reached management")
		}
	}
	if calls != 0 {
		t.Fatal("denied request reached downstream")
	}
	r := httptest.NewRequest("POST", "http://127.0.0.1/api/tasks", nil)
	r.Header.Set("Authorization", "Bearer fixture-management-secret")
	w := httptest.NewRecorder()
	manage.ServeHTTP(w, r)
	if w.Code != 204 || calls != 1 {
		t.Fatal("management fixture cannot authenticate")
	}
	stage := m.Stage(fixtureClaims(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, ok := FromContext(r.Context())
		if !ok || c != fixtureClaims() {
			t.Fatal("server identity absent")
		}
		for _, header := range []string{"X-Magpie-Account", "X-Fusion-Role", "X-Fusion-Stage", "X-Fusion-Task"} {
			if r.Header.Get(header) != "" {
				t.Fatal("client authority header propagated")
			}
		}
		w.WriteHeader(204)
	}))
	r = httptest.NewRequest("POST", "http://127.0.0.1/v1/messages", nil)
	r.Header.Set("Authorization", "Bearer "+raw)
	r.Header.Set("X-Magpie-Account", "fixture-forged")
	r.Header.Set("X-Fusion-Role", "acceptance")
	w = httptest.NewRecorder()
	stage.ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatal("server-scoped request refused")
	}
	r = httptest.NewRequest("POST", "http://127.0.0.1/v1/messages", nil)
	r.Header.Set("Authorization", "Bearer fixture-management-secret")
	w = httptest.NewRecorder()
	stage.ServeHTTP(w, r)
	if w.Code != 401 && w.Code != 403 {
		t.Fatal("management impersonated worker")
	}
}
func TestQueryKeyAndCrossOriginRequestsNeverReachHandler(t *testing.T) {
	m := NewManager("fixture-management-secret", func(Claims) bool { return true }, []string{"http://127.0.0.1:3426"})
	calls := 0
	h := m.Management(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(204) }))
	for _, url := range []string{"http://127.0.0.1/api?key=fixture-management-secret", "http://127.0.0.1/api?k=fixture-management-secret", "http://127.0.0.1/api?access_token=fixture-management-secret", "http://127.0.0.1/api?TOKEN=fixture-management-secret"} {
		r := httptest.NewRequest("POST", url, nil)
		r.Header.Set("Authorization", "Bearer fixture-management-secret")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("query credential accepted")
		}
	}
	for _, origin := range []string{"https://example.invalid", "null", "http://127.0.0.1:3426.evil.invalid", "http://localhost:3426"} {
		r := httptest.NewRequest("POST", "http://127.0.0.1/api", nil)
		r.Header.Set("Authorization", "Bearer fixture-management-secret")
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("cross-origin management call accepted")
		}
	}
	if calls != 0 {
		t.Fatal("forbidden request reached handler")
	}
}
func TestCredentialIssuanceRejectsUnknownScopesAndUnsafeTTL(t *testing.T) {
	m := NewManager("fixture-management-secret", func(Claims) bool { return true }, nil)
	for _, edit := range []func(*Claims){func(c *Claims) { c.RunID = "" }, func(c *Claims) { c.ProjectID = "" }, func(c *Claims) { c.Attempt = 0 }, func(c *Claims) { c.Generation = 0 }, func(c *Claims) { c.PlanRevision = 0 }, func(c *Claims) { c.Role = "sixth" }, func(c *Claims) { c.Audience = "management" }} {
		c := fixtureClaims()
		edit(&c)
		if _, e := m.Issue(c, time.Minute); e == nil {
			t.Fatal("invalid scope issued")
		}
	}
	for _, ttl := range []time.Duration{0, -time.Second, 6 * time.Minute} {
		if _, e := m.Issue(fixtureClaims(), ttl); e == nil {
			t.Fatal("unsafe TTL accepted")
		}
	}
	deny := NewManager("fixture-management-secret", nil, nil)
	if _, e := deny.Issue(fixtureClaims(), time.Minute); e == nil {
		t.Fatal("scope issued without trusted validator")
	}
}

func TestManagementRevocationAndFailuresDoNotEchoCredentials(t *testing.T) {
	secret := "fixture-management-never-echo"
	m := NewManager(secret, func(Claims) bool { return true }, nil)
	calls := 0
	h := m.Management(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(204) }))
	m.RevokeManagement()
	for _, target := range []string{"http://127.0.0.1/api", "http://127.0.0.1/api?key=" + secret} {
		r := httptest.NewRequest("POST", target, nil)
		r.Header.Set("Authorization", "Bearer "+secret)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 401 && w.Code != 403 {
			t.Fatal("revoked management credential accepted")
		}
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("failure echoed credential")
		}
	}
	if calls != 0 {
		t.Fatal("revoked key reached downstream")
	}
}
func TestStageDoesNotForwardNoncanonicalAuthorityHeaders(t *testing.T) {
	m := NewManager("fixture-management-secret", func(c Claims) bool { return c == fixtureClaims() }, nil)
	raw, e := m.Issue(fixtureClaims(), time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	leaked := false
	h := m.Stage(fixtureClaims(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for key := range r.Header {
			if strings.HasPrefix(strings.ToLower(key), "x-fusion-") || strings.EqualFold(key, "x-api-key") || strings.EqualFold(key, "x-magpie-account") {
				leaked = true
			}
		}
		w.WriteHeader(204)
	}))
	r := httptest.NewRequest("POST", "http://127.0.0.1/v1/messages", nil)
	r.Header.Set("Authorization", "Bearer "+raw)
	r.Header["x-api-key"] = []string{"fixture-forged"}
	r.Header["x-magpie-account"] = []string{"fixture-forged"}
	r.Header["x-fusion-route"] = []string{"fixture-forged"}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 204 || leaked {
		t.Fatal("noncanonical authority header leaked")
	}
}

func TestManagementContextRemainsIssuerBoundAndRevocable(t *testing.T) {
	m := NewManager("fixture-management", nil, nil)
	other := NewManager("fixture-management", nil, nil)
	var saved context.Context
	h := m.Management(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		saved = r.Context()
		if r.Header.Get("Authorization") != "" {
			t.Fatal("management secret forwarded")
		}
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer fixture-management")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 || saved == nil || !m.ManagementCurrent(saved) {
		t.Fatal("missing authenticated management context")
	}
	if m.ManagementCurrent(nil) || m.ManagementCurrent(context.Background()) || other.ManagementCurrent(saved) {
		t.Fatal("management context forged or crossed issuer")
	}
	closed, cancel := context.WithCancel(saved)
	cancel()
	if m.ManagementCurrent(closed) {
		t.Fatal("cancelled request context retained management authority")
	}
	m.RevokeManagement()
	if m.ManagementCurrent(saved) {
		t.Fatal("revoked connection remained authenticated")
	}
}

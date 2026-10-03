// Package policy enforces server-issued identities before an execution exit.
package policy

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"
)

type Audience string

const (
	ModelAudience  Audience = "model"
	EventsAudience Audience = "worker_events"
)

type Claims struct {
	TaskID       string         `json:"task_id"`
	RunID        string         `json:"run_id"`
	Role         stageplan.Role `json:"role"`
	Attempt      int64          `json:"attempt"`
	PlanRevision int64          `json:"plan_revision"`
	Generation   int64          `json:"generation"`
	ProjectID    string         `json:"project_id"`
	Audience     Audience       `json:"audience"`
}

var (
	ErrUnauthenticated = errors.New("authentication required")
	ErrForbidden       = errors.New("credential scope forbidden")
)

type grant struct {
	claims Claims
	expiry time.Time
}
type Manager struct {
	mu           sync.Mutex
	admin        [32]byte
	adminEnabled bool
	grants       map[[32]byte]grant
	revokedRuns  map[string]bool
	validate     func(Claims) bool
	origins      map[string]bool
	now          func() time.Time
}

// validate must consult current server task/run generation, lease and admission;
// it must not reenter Manager. Restarting an issuer revokes all stage secrets.
func NewManager(managementSecret string, validate func(Claims) bool, origins []string) *Manager {
	m := &Manager{admin: sha256.Sum256([]byte(managementSecret)), adminEnabled: managementSecret != "" && !strings.HasPrefix(managementSecret, "fgs_"), grants: map[[32]byte]grant{}, revokedRuns: map[string]bool{}, validate: validate, origins: map[string]bool{}, now: time.Now}
	for _, origin := range origins {
		u, e := url.Parse(origin)
		if e == nil && u.Scheme != "" && u.Host != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" {
			m.origins[origin] = true
		}
	}
	return m
}
func validScope(c Claims) bool {
	for _, v := range []string{c.TaskID, c.RunID, c.ProjectID} {
		if v == "" || len(v) > 256 || strings.ContainsFunc(v, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) {
			return false
		}
	}
	role := false
	for _, r := range stageplan.AllRoles() {
		if c.Role == r {
			role = true
		}
	}
	return role && c.Attempt > 0 && c.PlanRevision > 0 && c.Generation > 0 && (c.Audience == ModelAudience || c.Audience == EventsAudience)
}
func (m *Manager) Issue(c Claims, ttl time.Duration) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.adminEnabled || !validScope(c) || ttl <= 0 || ttl > 5*time.Minute || m.validate == nil || m.revokedRuns[c.RunID] || !m.validate(c) {
		return "", ErrForbidden
	}
	now := m.now()
	for key, g := range m.grants {
		if !now.Before(g.expiry) {
			delete(m.grants, key)
		}
	}
	if len(m.grants) >= 4096 {
		return "", ErrForbidden
	}
	var entropy [32]byte
	if _, e := rand.Read(entropy[:]); e != nil {
		return "", e
	}
	raw := "fgs_" + base64.RawURLEncoding.EncodeToString(entropy[:])
	m.grants[sha256.Sum256([]byte(raw))] = grant{claims: c, expiry: now.Add(ttl)}
	return raw, nil
}

func (m *Manager) RevokeManagement() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.adminEnabled = false
	m.grants = map[[32]byte]grant{}
}
func (m *Manager) RevokeRun(runID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.revokedRuns[runID] = true
	for key, g := range m.grants {
		if g.claims.RunID == runID {
			delete(m.grants, key)
		}
	}
}
func (m *Manager) AuthenticateStage(raw string, expected Claims) (Claims, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !strings.HasPrefix(raw, "fgs_") {
		return Claims{}, ErrUnauthenticated
	}
	key := sha256.Sum256([]byte(raw))
	g, ok := m.grants[key]
	if !ok || !m.now().Before(g.expiry) || m.revokedRuns[g.claims.RunID] {
		delete(m.grants, key)
		return Claims{}, ErrUnauthenticated
	}
	if !validScope(expected) || g.claims != expected || m.validate == nil || !m.validate(g.claims) {
		return Claims{}, ErrForbidden
	}
	return g.claims, nil
}

type claimsKey struct{}

func FromContext(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(claimsKey{}).(Claims)
	return c, ok
}
func bearer(r *http.Request) (string, error) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return "", ErrUnauthenticated
	}
	raw := strings.TrimPrefix(values[0], "Bearer ")
	if raw == "" || len(raw) > 256 || strings.ContainsAny(raw, " \t\r\n,") {
		return "", ErrUnauthenticated
	}
	return raw, nil
}
func queryCredential(r *http.Request) bool {
	q, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		return true
	}
	for key := range q {
		switch strings.ToLower(key) {
		case "key", "k", "token", "access_token", "api_key", "api-key", "apikey", "authorization", "stage_token":
			return true
		}
	}
	return false
}
func (m *Manager) requestBoundary(r *http.Request) error {
	if queryCredential(r) {
		return ErrForbidden
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return ErrForbidden
	}
	if origin := r.Header.Get("Origin"); origin != "" && !m.origins[origin] {
		return ErrForbidden
	}
	return nil
}
func authFailure(w http.ResponseWriter, e error) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	status := http.StatusUnauthorized
	if errors.Is(e, ErrForbidden) {
		status = http.StatusForbidden
	}
	http.Error(w, http.StatusText(status), status)
}
func (m *Manager) Management(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e := m.requestBoundary(r); e != nil {
			authFailure(w, e)
			return
		}
		raw, e := bearer(r)
		if e != nil {
			authFailure(w, e)
			return
		}
		if strings.HasPrefix(raw, "fgs_") {
			authFailure(w, ErrForbidden)
			return
		}
		h := sha256.Sum256([]byte(raw))
		m.mu.Lock()
		ok := m.adminEnabled && subtle.ConstantTimeCompare(h[:], m.admin[:]) == 1
		m.mu.Unlock()
		if !ok {
			authFailure(w, ErrUnauthenticated)
			return
		}
		clean := r.Clone(r.Context())
		stripAuthority(clean)
		next.ServeHTTP(w, clean)
	})
}
func (m *Manager) Stage(expected Claims, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e := m.requestBoundary(r); e != nil {
			authFailure(w, e)
			return
		}
		raw, e := bearer(r)
		if e != nil {
			authFailure(w, e)
			return
		}
		c, e := m.AuthenticateStage(raw, expected)
		if e != nil {
			authFailure(w, e)
			return
		}
		clean := r.Clone(context.WithValue(r.Context(), claimsKey{}, c))
		stripAuthority(clean)
		next.ServeHTTP(w, clean)
	})
}
func stripAuthority(r *http.Request) {
	for key := range r.Header {
		lower := strings.ToLower(key)
		if lower == "authorization" || lower == "x-api-key" || lower == "x-goog-api-key" || lower == "x-magpie-account" || strings.HasPrefix(lower, "x-fusion-") {
			delete(r.Header, key)
		}
	}
}

// DisabledIngress closes every legacy path, including unknown paths and native
// assets. It is replaced only by an explicitly authenticated Fusion handler.
// There is no loopback, URL query, cookie or named-Magpie-key exception.
func DisabledIngress() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if queryCredential(r) {
			authFailure(w, ErrForbidden)
			return
		}
		authFailure(w, ErrUnauthenticated)
	})
}

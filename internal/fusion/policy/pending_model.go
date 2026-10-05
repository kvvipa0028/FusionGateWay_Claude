package policy

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"time"
)

type preparedModel struct {
	claims Claims
	expiry time.Time
	ttl    time.Duration
}

// PendingModelGrant holds preallocated entropy, not an authorized credential.
// Activate must run only after ConfirmStarted; it uses the unchanged current
// StoreValidator. No HTTP request can construct or activate this facade.
type PendingModelGrant struct {
	manager *Manager
	hash    [32]byte
	claims  Claims
	secret  string
}

func (*PendingModelGrant) String() string   { return "pending model grant (redacted)" }
func (*PendingModelGrant) GoString() string { return "PendingModelGrant(<redacted>)" }
func (p *PendingModelGrant) Claims() Claims {
	if p == nil {
		return Claims{}
	}
	return p.claims
}

// PreparedFor permits trusted launch preparation only; it never authenticates
// a caller. Activation TTL must cover the complete bounded process lifetime.
func (p *PendingModelGrant) PreparedFor(expected Claims, lifetime time.Duration) bool {
	if p == nil || p.manager == nil || lifetime <= 0 || lifetime > 4*time.Minute {
		return false
	}
	m := p.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	prepared, ok := m.prepared[p.hash]
	return ok && prepared.claims == expected && p.claims == expected && m.adminEnabled.Load() && !m.revokedRuns[expected.RunID] && m.now().Before(prepared.expiry) && prepared.ttl >= lifetime+20*time.Second
}

// PrepareModel may precede process startup. Its output is unknown to stage
// authentication until activation, expires after one minute if unactivated,
// and shares the issuer's capacity bound with already active capabilities.
func (m *Manager) PrepareModel(c Claims, ttl time.Duration) (*PendingModelGrant, error) {
	if m == nil {
		return nil, ErrForbidden
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.adminEnabled.Load() || m.validate == nil || !validScope(c) || c.Audience != ModelAudience || m.revokedRuns[c.RunID] || ttl <= 0 || ttl > 5*time.Minute {
		return nil, ErrForbidden
	}
	now := m.now()
	for key, g := range m.grants {
		if !now.Before(g.expiry) {
			delete(m.grants, key)
		}
	}
	for key, p := range m.prepared {
		if !now.Before(p.expiry) {
			delete(m.prepared, key)
		}
	}
	if len(m.grants)+len(m.prepared) >= 4096 {
		return nil, ErrForbidden
	}
	var entropy [32]byte
	if _, e := rand.Read(entropy[:]); e != nil {
		return nil, e
	}
	raw := "fgs_" + base64.RawURLEncoding.EncodeToString(entropy[:])
	hash := sha256.Sum256([]byte(raw))
	if m.prepared == nil {
		m.prepared = map[[32]byte]preparedModel{}
	}
	m.prepared[hash] = preparedModel{claims: c, expiry: now.Add(time.Minute), ttl: ttl}
	return &PendingModelGrant{manager: m, hash: hash, claims: c, secret: raw}, nil
}

// Secret is for a trusted Native launcher only. Delivery before activation
// does not authorize a call; the active grant map remains the sole authority.
func (p *PendingModelGrant) Secret() (string, error) {
	if p == nil || p.manager == nil {
		return "", ErrUnauthenticated
	}
	m := p.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.adminEnabled.Load() || m.revokedRuns[p.claims.RunID] {
		return "", ErrUnauthenticated
	}
	if prepared, ok := m.prepared[p.hash]; ok {
		if prepared.claims == p.claims && m.now().Before(prepared.expiry) {
			return p.secret, nil
		}
		delete(m.prepared, p.hash)
	}
	if active, ok := m.grants[p.hash]; ok && active.claims == p.claims && m.now().Before(active.expiry) && m.validate != nil && m.validate(p.claims) {
		return p.secret, nil
	}
	return "", ErrUnauthenticated
}

// Activate consumes the preparation once. Replaying this operation never
// renews a TTL. A failed current-state check grants no authority.
func (p *PendingModelGrant) Activate() error {
	if p == nil || p.manager == nil {
		return ErrForbidden
	}
	m := p.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	prepared, ok := m.prepared[p.hash]
	if !ok || prepared.claims != p.claims || !m.adminEnabled.Load() || m.revokedRuns[p.claims.RunID] || m.validate == nil {
		return ErrForbidden
	}
	now := m.now()
	if !now.Before(prepared.expiry) {
		delete(m.prepared, p.hash)
		return ErrForbidden
	}
	if !m.validate(p.claims) {
		return ErrForbidden
	}
	delete(m.prepared, p.hash)
	m.grants[p.hash] = grant{claims: p.claims, expiry: now.Add(prepared.ttl)}
	return nil
}

func (p *PendingModelGrant) Cancel() {
	if p == nil || p.manager == nil {
		return
	}
	m := p.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.prepared, p.hash)
	delete(m.grants, p.hash)
	p.secret = ""
}

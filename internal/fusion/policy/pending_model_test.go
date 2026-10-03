package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPreparedModelSecretHasNoAuthorityBeforeCurrentActivation(t *testing.T) {
	running := false
	c := fixtureClaims()
	m := NewManager("fixture-management", func(got Claims) bool { return running && got == c }, nil)
	p, e := m.PrepareModel(c, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := p.Secret()
	if e != nil || !strings.HasPrefix(raw, "fgs_") {
		t.Fatal("prepared entropy unavailable", e)
	}
	if _, e = m.AuthenticateStage(raw, c); e == nil {
		t.Fatal("prepared entropy already authorized")
	}
	if e = p.Activate(); e == nil {
		t.Fatal("starting run granted model authority")
	}
	if _, e = m.AuthenticateStage(raw, c); e == nil {
		t.Fatal("failed activation issued authority")
	}
	running = true
	if e = p.Activate(); e != nil {
		t.Fatal(e)
	}
	ctx, e := m.WithStage(context.Background(), raw, c)
	if e != nil || !m.ModelCurrent(ctx, c) {
		t.Fatal("confirmed activation not current", e)
	}
	if e = p.Activate(); e == nil {
		t.Fatal("activation replay extended grant")
	}
	if p.Claims() != c {
		t.Fatal("prepared scope changed")
	}
	for _, s := range []string{fmt.Sprint(p), fmt.Sprintf("%#v", p), fmt.Sprintf("%+v", p)} {
		if strings.Contains(s, raw) {
			t.Fatal("pending facade formatting leaked entropy")
		}
	}
	b, e := json.Marshal(p)
	if e != nil || strings.Contains(string(b), raw) {
		t.Fatal("pending facade JSON leaked entropy")
	}
	p.Cancel()
	if m.ModelCurrent(ctx, c) {
		t.Fatal("cancelled capability remained valid")
	}
	if _, e = p.Secret(); e == nil {
		t.Fatal("cancelled entropy remained deliverable")
	}
}

func TestPreparedModelActivationExpiryRevocationAndIssuerIsolation(t *testing.T) {
	for _, mode := range []string{"prepare_expired", "run_revoked", "manager_revoked", "cancelled", "active_expired", "identity"} {
		t.Run(mode, func(t *testing.T) {
			valid := true
			c := fixtureClaims()
			m := NewManager("fixture-management", func(got Claims) bool { return valid && got == c }, nil)
			now := time.Now()
			m.now = func() time.Time { return now }
			p, e := m.PrepareModel(c, time.Minute)
			if e != nil {
				t.Fatal(e)
			}
			raw, e := p.Secret()
			if e != nil {
				t.Fatal(e)
			}
			switch mode {
			case "prepare_expired":
				now = now.Add(time.Minute)
			case "run_revoked":
				m.RevokeRun(c.RunID)
			case "manager_revoked":
				m.RevokeManagement()
			case "cancelled":
				p.Cancel()
			case "identity":
				valid = false
			case "active_expired":
				if e = p.Activate(); e != nil {
					t.Fatal(e)
				}
				now = now.Add(time.Minute)
			}
			if e = p.Activate(); e == nil {
				t.Fatal("stale/invalid pending secret activated", mode)
			}
			if _, e = m.AuthenticateStage(raw, c); e == nil {
				t.Fatal("stale/invalid capability authorized", mode)
			}
			fresh := NewManager("fixture-management", func(Claims) bool { return true }, nil)
			if _, e = fresh.AuthenticateStage(raw, c); e == nil {
				t.Fatal("pending secret reconstructed at another issuer")
			}
		})
	}
}

func TestPreparedModelRejectsMissingScopeUnsafeTTLAndBoundedCapacity(t *testing.T) {
	c := fixtureClaims()
	m := NewManager("fixture-management", func(Claims) bool { return true }, nil)
	for _, edit := range []func(*Claims){func(c *Claims) { c.Audience = EventsAudience }, func(c *Claims) { c.RunID = "" }, func(c *Claims) { c.Generation = 0 }, func(c *Claims) { c.Attempt = 0 }} {
		bad := c
		edit(&bad)
		if _, e := m.PrepareModel(bad, time.Minute); e == nil {
			t.Fatal("invalid preparation scope accepted")
		}
	}
	for _, ttl := range []time.Duration{0, -time.Second, 6 * time.Minute} {
		if _, e := m.PrepareModel(c, ttl); e == nil {
			t.Fatal("unsafe ttl accepted")
		}
	}
	for i := 0; i < 4096; i++ {
		if _, e := m.PrepareModel(c, time.Minute); e != nil {
			t.Fatal("capacity too small", i, e)
		}
	}
	if _, e := m.PrepareModel(c, time.Minute); e == nil {
		t.Fatal("unbounded prepared secret storage")
	}
	if _, e := m.Issue(c, time.Minute); e == nil {
		t.Fatal("Issue bypassed shared issuer capacity")
	}
	m.RevokeRun(c.RunID)
	if len(m.prepared) != 0 {
		t.Fatal("revoked pending entropy retained")
	}
}

// Package testsupport contains synthetic fixtures, never production adapters.
package testsupport

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

var ErrOutboundDenied = errors.New("fixture transport denied outbound request")

type Transport struct {
	endpoint string
	inner    *http.Transport
	outbound atomic.Int64
	denied   atomic.Int64
}

// NewTransport admits exactly one explicit HTTP loopback listener. No DNS, proxy,
// ambient credential lookup, or fallback transport is used.
func NewTransport(endpoint string) *Transport {
	u, e := url.Parse(endpoint)
	allowed := ""
	if e == nil && u.Scheme == "http" && u.User == nil && u.Port() != "" && net.ParseIP(u.Hostname()) != nil && net.ParseIP(u.Hostname()).IsLoopback() {
		allowed = u.Scheme + "://" + u.Host
	}
	return &Transport{endpoint: allowed, inner: &http.Transport{Proxy: nil}}
}
func (t *Transport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r == nil || r.URL == nil || t.endpoint == "" || r.URL.User != nil || r.URL.Scheme+"://"+r.URL.Host != t.endpoint {
		t.denied.Add(1)
		return nil, ErrOutboundDenied
	}
	t.outbound.Add(1)
	return t.inner.RoundTrip(r)
}
func (t *Transport) Outbound() int64       { return t.outbound.Load() }
func (t *Transport) Denied() int64         { return t.denied.Load() }
func (t *Transport) CloseIdleConnections() { t.inner.CloseIdleConnections() }

type Scenario struct {
	Mode, Model, Account string
	Revision             int
	Delay                time.Duration
}
type Provider struct {
	*httptest.Server
	requests atomic.Int64
}

func (p *Provider) Requests() int64 { return p.requests.Load() }
func fixtureID(s string) bool {
	return strings.HasPrefix(s, "fixture-") && len(s) <= 80 && !strings.ContainsAny(s, "\r\n\t ")
}
func NewProvider(s Scenario) *Provider {
	p := &Provider{}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.requests.Add(1)
		if r.Method != "POST" || r.URL.Path != "/chat" {
			http.Error(w, "fixture endpoint only", 404)
			return
		}
		var in struct {
			Model, Account string
			Revision       int
		}
		body, readErr := io.ReadAll(io.LimitReader(r.Body, 4097))
		d := json.NewDecoder(bytes.NewReader(body))
		d.DisallowUnknownFields()
		decodeErr := d.Decode(&in)
		var trailing any
		trailingErr := d.Decode(&trailing)
		if readErr != nil || len(body) > 4096 || decodeErr != nil || trailingErr != io.EOF || !fixtureID(s.Model) || !fixtureID(s.Account) || !fixtureID(in.Model) || !fixtureID(in.Account) || r.Header.Get("Authorization") != "" || r.Header.Get("X-Api-Key") != "" {
			http.Error(w, "unsafe fixture input", 400)
			return
		}
		if s.Delay > 0 {
			select {
			case <-time.After(s.Delay):
			case <-r.Context().Done():
				return
			}
		}
		if s.Mode == "rate_limit" {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "fixture rate limit", 429)
			return
		}
		if s.Mode == "error" {
			http.Error(w, "fixture upstream unavailable", 503)
			return
		}
		if in.Model != s.Model || in.Account != s.Account || in.Revision != s.Revision || s.Mode == "config_changed" {
			http.Error(w, "fixture revision conflict", 409)
			return
		}
		if s.Mode == "stream" || s.Mode == "broken_stream" {
			w.Header().Set("Content-Type", "text/event-stream")
			data := fmt.Sprintf("data: {\"model\":%q,\"account\":%q,\"delta\":\"fixture-part-1\"}\n\ndata: {\"tool\":\"fixture-tool\",\"arguments\":{\"path\":\"fixture.txt\"}}\n\n", s.Model, s.Account)
			if s.Mode == "broken_stream" {
				w.Header().Set("Content-Length", fmt.Sprint(len(data)+100))
			}
			io.WriteString(w, data)
			w.(http.Flusher).Flush()
			if s.Mode == "broken_stream" {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err == nil {
					conn.Close()
				}
				return
			}
			io.WriteString(w, "data: {\"delta\":\"fixture-part-2\"}\n\ndata: [DONE]\n\n")
			return
		}
		if s.Mode != "ok" && s.Mode != "quota_unknown" {
			http.Error(w, "unknown fixture scenario", 400)
			return
		}
		quota := "fixture-available"
		if s.Mode == "quota_unknown" {
			quota = "unknown"
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"model": s.Model, "account": s.Account, "revision": s.Revision, "quota": quota, "result": "fixture-only"})
	}))
	return p
}

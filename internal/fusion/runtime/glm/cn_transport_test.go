package glm

import (
	"context"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func cnRequest(t *testing.T) *http.Request {
	t.Helper()
	r, e := http.NewRequestWithContext(context.Background(), http.MethodPost, Endpoint+"/v1/messages?beta=true", strings.NewReader(`{"model":"glm-5.3"}`))
	if e != nil {
		t.Fatal(e)
	}
	r.GetBody = nil
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "text/event-stream")
	r.Header.Set("Anthropic-Version", "2023-06-01")
	r.Header.Set("Authorization", "Bearer fixture-controller-key")
	r.Header.Set("X-Api-Key", "fixture-controller-key")
	return r
}

func TestGLMCNTransportRejectsChangedDestinationAndReplayBeforeDial(t *testing.T) {
	for _, mode := range []string{"scheme", "host", "port", "path", "raw_path", "query", "userinfo", "fragment", "method", "host_header", "replay", "empty_body", "large_body", "cookie", "duplicate_auth", "auth_mismatch", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			r := cnRequest(t)
			switch mode {
			case "scheme":
				r.URL.Scheme = "http"
			case "host":
				r.URL.Host = "api.z.ai"
			case "port":
				r.URL.Host += ":443"
			case "path":
				r.URL.Path = "/api/paas/v4/chat/completions"
			case "raw_path":
				r.URL.RawPath = "/api/anthropic/v1/%6dessages"
			case "query":
				r.URL.RawQuery = "other=1"
			case "userinfo":
				r.URL.User = url.User("fixture")
			case "fragment":
				r.URL.Fragment = "fixture"
			case "method":
				r.Method = http.MethodGet
			case "host_header":
				r.Host = "other.example"
			case "replay":
				r.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("fixture")), nil }
			case "empty_body":
				r.Body = nil
			case "large_body":
				r.ContentLength = (1 << 20) + 1
			case "cookie":
				r.Header.Set("Cookie", "fixture")
			case "duplicate_auth":
				r.Header.Add("Authorization", "Bearer other")
			case "auth_mismatch":
				r.Header.Set("X-Api-Key", "other")
			case "cancelled":
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				r = r.WithContext(ctx)
			}
			tr := NewCNTransport()
			var dials atomic.Int64
			tr.transport.DialContext = func(context.Context, string, string) (net.Conn, error) { dials.Add(1); return nil, ErrIdentity }
			if _, e := tr.RoundTrip(r); e == nil || dials.Load() != 0 || strings.Contains(e.Error(), "fixture-controller-key") {
				t.Fatal("unsafe upstream request dialed or disclosed")
			}
		})
	}
}

func localCNTransport(t *testing.T, handler http.Handler) (*CNTransport, *httptest.Server) {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	tr := NewCNTransport()
	if tr.transport.Proxy != nil || !tr.transport.DisableKeepAlives || !tr.transport.DisableCompression || tr.transport.ForceAttemptHTTP2 || len(tr.transport.TLSNextProto) != 0 || tr.transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("production transport boundary weakened")
	}
	local, _ := url.Parse(srv.URL)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	tr.transport.TLSClientConfig.RootCAs, tr.transport.TLSClientConfig.ServerName = pool, local.Hostname()
	tr.transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != "open.bigmodel.cn:443" {
			t.Error("destination drift")
		}
		return (&net.Dialer{}).DialContext(ctx, network, local.Host)
	}
	return tr, srv
}

func TestGLMCNTransportFreshTLSHasNoHiddenRetryOrRedirect(t *testing.T) {
	for _, mode := range []string{"success", "redirect", "closed_connection", "rate_limit"} {
		t.Run(mode, func(t *testing.T) {
			var sends atomic.Int64
			tr, _ := localCNTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				if r.Method != http.MethodPost || r.URL.RequestURI() != "/api/anthropic/v1/messages?beta=true" || r.Host != "open.bigmodel.cn" || r.Header.Get("Authorization") != "Bearer fixture-controller-key" {
					t.Error("TLS request drift")
				}
				switch mode {
				case "redirect":
					http.Redirect(w, r, "/again", http.StatusTemporaryRedirect)
				case "rate_limit":
					w.WriteHeader(429)
				case "closed_connection":
					c, _, e := w.(http.Hijacker).Hijack()
					if e != nil {
						t.Error(e)
						return
					}
					c.Close()
				default:
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, "fixture\n")
				}
			}))
			res, e := tr.RoundTrip(cnRequest(t))
			if mode == "closed_connection" {
				if e == nil || res != nil {
					t.Fatal("failed fresh connection accepted")
				}
			} else {
				if e != nil || res == nil {
					t.Fatal(e)
				}
				_, e = io.ReadAll(res.Body)
				if e != nil {
					t.Fatal(e)
				}
				res.Body.Close()
				if res.Request.Context().Err() == nil {
					t.Fatal("response lifecycle not cancelled")
				}
			}
			if sends.Load() != 1 {
				t.Fatal("hidden send/retry/redirect")
			}
		})
	}
}

func TestGLMCNTransportDoesNotTrustForeignTLS(t *testing.T) {
	var sends atomic.Int64
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { sends.Add(1) }))
	t.Cleanup(srv.Close)
	local, _ := url.Parse(srv.URL)
	tr := NewCNTransport()
	tr.transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, local.Host)
	}
	if _, e := tr.RoundTrip(cnRequest(t)); e == nil || strings.Contains(e.Error(), "fixture-controller-key") || sends.Load() != 0 {
		t.Fatal("untrusted TLS accepted")
	}
}

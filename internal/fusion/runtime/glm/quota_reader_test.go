package glm

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/quota"
	"github.com/yetone/magpie/internal/fusion/stageplan"
)

const quotaFixture = `{"success":true,"data":{"level":"fixture-plan","limits":[{"type":"TOKENS_LIMIT","unit":3,"number":5,"percentage":25,"nextResetTime":4102444800000},{"type":"TIME_LIMIT","unit":5,"number":1,"percentage":0}]}}`

func quotaReaderFixture(t *testing.T) (*QuotaReader, quota.Identity, *atomic.Int64, *atomic.Int64) {
	t.Helper()
	i := quota.Identity{Provider: "bigmodel", Account: "fixture-account", Workspace: "fixture-workspace", Region: "CN", Generation: 1}
	loads, sends := &atomic.Int64{}, &atomic.Int64{}
	r, e := NewQuotaReader(QuotaReaderConfig{Identity: i, Target: stageplan.ExecutionTarget{Account: i.Account, Workspace: i.Workspace, CredentialIdentity: "fixture-credential", BillingPath: "coding_plan"}, QueryAllowed: func(context.Context, quota.Identity) bool { return true }, LoadCredential: func(context.Context, stageplan.ExecutionTarget) (Credential, error) {
		loads.Add(1)
		return Credential{Identity: "fixture-credential", Key: "fixture-controller-key"}, nil
	}})
	if e != nil {
		t.Fatal(e)
	}
	r.transport = fixtureRoundTrip(func(req *http.Request) (*http.Response, error) {
		sends.Add(1)
		if req.Method != http.MethodGet || req.URL.String() != QuotaEndpoint || req.GetBody != nil || req.Header.Get("Authorization") != "fixture-controller-key" || req.Header.Get("Accept") != "application/json" {
			t.Error("incorrect quota request")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(quotaFixture))}, nil
	})
	return r, i, loads, sends
}

func TestGLMQuotaReaderFixedQueryKeepsAdmissionUnverified(t *testing.T) {
	r, i, loads, sends := quotaReaderFixture(t)
	s, e := r.Read(context.Background(), i)
	if e != nil || s.Identity != i || s.Source != QuotaEndpoint || len(s.Windows) != 2 || s.Windows[0].UsedPercent == nil || *s.Windows[0].UsedPercent != 25 || s.Windows[1].Kind != "mcp" || s.ObservedAt == nil || s.ReceivedAt.Before(*s.ObservedAt) || s.Pool.Verified || s.Status != quota.Unverified || sends.Load() != 1 || loads.Load() != 1 {
		t.Fatalf("quota query contract failed: %v", e)
	}
	if quota.State(s, time.Now(), time.Minute) == quota.Available {
		t.Fatal("read was promoted to execution admission")
	}
}

func TestGLMQuotaReaderRefusesBeforeSecretOrNetwork(t *testing.T) {
	for _, mode := range []string{"account", "generation", "region", "denied", "cancelled", "credential", "late_denied"} {
		t.Run(mode, func(t *testing.T) {
			r, i, loads, sends := quotaReaderFixture(t)
			ctx := context.Background()
			allowed := true
			wantLoads := int64(0)
			switch mode {
			case "account":
				i.Account = "other"
			case "generation":
				i.Generation++
			case "region":
				i.Region = "Global"
			case "denied":
				allowed = false
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "credential", "late_denied":
				wantLoads = 1
				r.config.LoadCredential = func(context.Context, stageplan.ExecutionTarget) (Credential, error) {
					loads.Add(1)
					if mode == "late_denied" {
						allowed = false
						return Credential{Identity: "fixture-credential", Key: "fixture-controller-key"}, nil
					}
					return Credential{Identity: "wrong", Key: "fixture-controller-key"}, nil
				}
			}
			r.config.QueryAllowed = func(context.Context, quota.Identity) bool { return allowed }
			if _, e := r.Read(ctx, i); e == nil || loads.Load() != wantLoads || sends.Load() != 0 {
				t.Fatal("refused query executed")
			}
		})
	}
}

func TestGLMQuotaReaderBoundsAndStaticErrors(t *testing.T) {
	for _, mode := range []string{"redirect", "auth", "forbidden", "not_found", "rate_limit", "server", "network", "large", "duplicate", "missing_success", "null", "trailing", "body_key", "read_failure", "date", "late_identity"} {
		t.Run(mode, func(t *testing.T) {
			r, i, _, sends := quotaReaderFixture(t)
			allowed := true
			r.config.QueryAllowed = func(context.Context, quota.Identity) bool { return allowed }
			r.transport = fixtureRoundTrip(func(req *http.Request) (*http.Response, error) {
				sends.Add(1)
				code, raw := 200, quotaFixture
				header := http.Header{"Content-Type": {"application/json"}}
				switch mode {
				case "redirect":
					code = 302
					header.Set("Location", "https://other.example/secret")
				case "auth":
					code = 401
				case "forbidden":
					code = 403
				case "not_found":
					code = 404
				case "rate_limit":
					code = 429
				case "server":
					code = 503
				case "network":
					return nil, errors.New("fixture-controller-key")
				case "large":
					raw = strings.Repeat("x", 65537)
				case "duplicate":
					raw = `{"success":true,"success":false,"data":{"limits":[]}}`
				case "missing_success":
					raw = `{"data":{"limits":[]}}`
				case "null":
					raw = `null`
				case "trailing":
					raw += `{}`
				case "body_key":
					raw = `{"success":true,"data":{"limits":[],"secret":"fixture-controller-key"}}`
				case "read_failure":
					return &http.Response{StatusCode: 200, Header: header, Body: failingQuotaBody{}}, nil
				case "date":
					header.Set("Date", time.Now().Add(-2*time.Minute).UTC().Format(http.TimeFormat))
				case "late_identity":
					allowed = false
				}
				return &http.Response{StatusCode: code, Header: header, Body: io.NopCloser(strings.NewReader(raw))}, nil
			})
			s, e := r.Read(context.Background(), i)
			if mode == "date" {
				if e != nil || s.ObservedAt == nil || time.Since(*s.ObservedAt) < time.Minute {
					t.Fatal("source date rejuvenated")
				}
				return
			}
			if e == nil || strings.Contains(e.Error(), "fixture-controller-key") || sends.Load() != 1 {
				t.Fatal("unsafe error or repeated send")
			}
			var q *quota.QueryError
			if !errors.As(e, &q) {
				t.Fatal("query error lost")
			}
			if mode == "auth" && q.Status != quota.AuthRequired || mode == "not_found" && q.Status != quota.Unsupported {
				t.Fatal("status classification incorrect")
			}
		})
	}
}

type failingQuotaBody struct{}

func (failingQuotaBody) Read([]byte) (int, error) { return 0, errors.New("fixture-controller-key") }
func (failingQuotaBody) Close() error             { return nil }

func TestGLMQuotaReaderProductionTransportSendsOnce(t *testing.T) {
	for _, mode := range []string{"redirect", "closed_connection"} {
		t.Run(mode, func(t *testing.T) {
			var sends atomic.Int64
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				sends.Add(1)
				if mode == "redirect" {
					http.Redirect(w, req, "/again", http.StatusTemporaryRedirect)
					return
				}
				conn, _, e := w.(http.Hijacker).Hijack()
				if e != nil {
					t.Error("fixture connection unavailable")
					return
				}
				conn.Close()
			}))
			t.Cleanup(srv.Close)
			r, i, _, _ := quotaReaderFixture(t)
			owned, e := NewQuotaReader(r.config)
			if e != nil {
				t.Fatal(e)
			}
			tr := owned.transport.(*http.Transport)
			if tr.Proxy != nil || !tr.DisableKeepAlives || !tr.DisableCompression || tr.ForceAttemptHTTP2 || len(tr.TLSNextProto) != 0 || tr.TLSClientConfig.InsecureSkipVerify || tr.MaxConnsPerHost != 2 {
				t.Fatal("production transport permits inherited proxy/reuse/unverified TLS")
			}
			// Only the test replaces dialing and the trust root; production
			// still has the exact CN URL and TLS verification enabled.
			local, _ := url.Parse(srv.URL)
			pool := x509.NewCertPool()
			pool.AddCert(srv.Certificate())
			tr.TLSClientConfig.RootCAs, tr.TLSClientConfig.ServerName = pool, local.Hostname()
			tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				if addr != "open.bigmodel.cn:443" {
					t.Error("destination changed")
				}
				return (&net.Dialer{}).DialContext(ctx, network, local.Host)
			}
			if _, e := owned.Read(context.Background(), i); e == nil || sends.Load() != 1 {
				t.Fatal("redirect/retry occurred")
			}
		})
	}
}
func TestGLMQuotaReaderConstructorRequiresExactCNIdentity(t *testing.T) {
	r, _, _, _ := quotaReaderFixture(t)
	for _, mode := range []string{"loader", "permission", "provider", "region", "generation", "account", "workspace", "credential", "billing"} {
		c := r.config
		switch mode {
		case "loader":
			c.LoadCredential = nil
		case "permission":
			c.QueryAllowed = nil
		case "provider":
			c.Identity.Provider = "other"
		case "region":
			c.Identity.Region = "Global"
		case "generation":
			c.Identity.Generation = 0
		case "account":
			c.Target.Account = "other"
		case "workspace":
			c.Target.Workspace = "other"
		case "credential":
			c.Target.CredentialIdentity = ""
		case "billing":
			c.Target.BillingPath = "payg"
		}
		if _, e := NewQuotaReader(c); e == nil {
			t.Fatal("unsafe quota configuration accepted", mode)
		}
	}
}

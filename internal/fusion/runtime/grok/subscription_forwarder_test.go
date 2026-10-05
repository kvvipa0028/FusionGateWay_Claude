//go:build darwin

package grok

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

const grokFwdBody = `{"model":"fixture-model","stream":true,"stream_options":{"include_usage":true},"reasoning_effort":"high","messages":[{"role":"user","content":"fixture"}],"tools":[{"type":"function","function":{"name":"read_file","parameters":{"type":"object"}}}]}`

func grokSubscriptionFixture(t *testing.T, h http.Handler) (*SubscriptionForwarder, stageplan.ExecutionTarget, string) {
	t.Helper()
	p, s, target := grokCredentialFixture(t)
	credential, e := NewFileCredential(p, s)
	if e != nil {
		t.Fatal(e)
	}
	f, e := NewSubscriptionForwarder(credential)
	if e != nil {
		t.Fatal(e)
	}
	tr := f.transport
	if tr.Proxy != nil || !tr.DisableKeepAlives || !tr.DisableCompression || tr.ForceAttemptHTTP2 || len(tr.TLSNextProto) != 0 || tr.TLSClientConfig.InsecureSkipVerify || tr.TLSClientConfig.RootCAs == nil {
		t.Fatal("production HTTPS boundary weakened")
	}
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	local, _ := url.Parse(srv.URL)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	tr.TLSClientConfig.RootCAs, tr.TLSClientConfig.ServerName = pool, local.Hostname()
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != "cli-chat-proxy.grok.com:443" {
			t.Error("subscription destination changed")
		}
		return (&net.Dialer{}).DialContext(ctx, network, local.Host)
	}
	target.RequestedModel, target.ResolvedModel = "fixture-model", "fixture-model"
	target.Effort = stageplan.FrozenEffort{RequestedMode: stageplan.EffortExplicit, Value: ptr("high")}
	target.LockEnforcement = stageplan.ControlledCalls
	return f, target, p
}

func TestGrokSubscriptionForwarderActualTLSAndModelEcho(t *testing.T) {
	var calls atomic.Int64
	f, target, _ := grokSubscriptionFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.RequestURI() != "/v1/chat/completions" || r.Host != "cli-chat-proxy.grok.com" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer opaque-fixture-grok-bearer" || r.Header.Get("X-XAI-Token-Auth") != "xai-grok-cli" || r.Header.Get("x-grok-model-override") != "fixture-model" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Accept") != "text/event-stream" || !strings.HasPrefix(r.UserAgent(), "grok-cli/"+CLIVersion) || !r.Close || r.Header.Get("Cookie") != "" || r.Header.Get("X-Api-Key") != "" || r.Header.Get("XAI_API_KEY") != "" {
			t.Error("fixed authenticated request drifted")
		}
		b, e := io.ReadAll(r.Body)
		if e != nil || string(b) != grokFwdBody {
			t.Error("frozen request body changed")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Set-Cookie", "fixture-upstream-cookie")
		io.WriteString(w, gateSSE)
	}))
	r, e := f.Send(context.Background(), target, []byte(grokFwdBody))
	if e != nil || r.StatusCode != 200 || r.ContentType != "text/event-stream" || r.Body == nil {
		t.Fatal("real TLS subscription not accepted", e)
	}
	if r.ReportedModel != "fixture-model" || len(r.PrivateMarkers) != 1 || string(r.PrivateMarkers[0]) != "opaque-fixture-grok-bearer" {
		t.Fatal("model echo or private marker lost")
	}
	b, e := io.ReadAll(r.Body)
	if e != nil || string(b) != gateSSE {
		t.Fatal("buffered upstream stream changed", e)
	}
	if calls.Load() != 1 {
		t.Fatal("single fresh send expected")
	}
}

func TestGrokSubscriptionForwarderFailClosed(t *testing.T) {
	for _, mode := range []string{"status-500", "wrong-media", "compressed", "no-model-echo", "model-drift", "secret-reflection", "request-secret", "nonstream", "model-substituted", "redirect", "429", "credential-rotated", "cancelled", "oversize-body"} {
		t.Run(mode, func(t *testing.T) {
			var handler http.HandlerFunc = func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "status-500":
					w.WriteHeader(500)
					io.WriteString(w, "upstream private error detail")
					return
				case "wrong-media":
					w.Header().Set("Content-Type", "application/json")
					io.WriteString(w, gateSSE)
					return
				case "compressed":
					w.Header().Set("Content-Type", "text/event-stream")
					w.Header().Set("Content-Encoding", "gzip")
					io.WriteString(w, gateSSE)
					return
				case "no-model-echo":
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, strings.ReplaceAll(gateSSE, `"model":"fixture-model"`, `"model":""`))
					return
				case "model-drift":
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, strings.Replace(gateSSE, `"model":"fixture-model"`, `"model":"other-model"`, 1))
					return
				case "secret-reflection":
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, strings.Replace(gateSSE, `"content":"fixture"`, `"content":"opaque-fixture-grok-bearer"`, 1))
					return
				case "redirect":
					w.Header().Set("Location", "https://evil.example/collect")
					w.WriteHeader(307)
					return
				case "429":
					w.WriteHeader(429)
					io.WriteString(w, `{"error":{"message":"rate limited"}}`)
					return
				case "oversize-body":
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, "data: "+strings.Repeat("x", (8<<20)+2)+"\n\ndata: [DONE]\n\n")
					return
				default:
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, gateSSE)
				}
			}
			f, target, p := grokSubscriptionFixture(t, handler)
			body := grokFwdBody
			switch mode {
			case "nonstream":
				body = strings.Replace(body, `"stream":true`, `"stream":false`, 1)
			case "model-substituted":
				body = strings.Replace(body, `"model":"fixture-model"`, `"model":"other"`, 1)
			case "request-secret":
				body = strings.Replace(body, `"content":"fixture"`, `"content":"opaque-fixture-grok-bearer"`, 1)
			}
			ctx := context.Background()
			if mode == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if mode == "credential-rotated" {
				if e := os.WriteFile(p, []byte(`{"https://accounts.x.ai/sign-in":{"key":"opaque-fixture-rotated"}}`), 0600); e != nil {
					t.Fatal(e)
				}
			}
			r, e := f.Send(ctx, target, []byte(body))
			if mode == "429" {
				if e != nil || r.StatusCode != 429 {
					t.Fatal("rate limit not surfaced as bounded empty body", e)
				}
				if n, _ := io.ReadAll(r.Body); len(n) != 0 {
					t.Fatal("429 leaked upstream body")
				}
				return
			}
			if e == nil && mode != "cancelled" {
				// Delivery-time rotation must also fail on read.
				if _, re := io.ReadAll(r.Body); mode == "credential-rotated" && re == nil {
					t.Fatal("rotated credential still delivering")
				}
				if mode != "credential-rotated" {
					t.Fatal("unsafe upstream accepted", mode)
				}
				return
			}
			if e != nil && !errors.Is(e, ErrSubscriptionTransport) && mode != "cancelled" {
				t.Fatal("unexpected error", mode, e)
			}
		})
	}
}

func ptr(s string) *string { return &s }

//go:build darwin

package codex

import (
	"context"
	"crypto/x509"
	"encoding/json"
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
	"time"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

func subscriptionFixture(t *testing.T, h http.Handler) (*SubscriptionForwarder, stageplan.ExecutionTarget, string) {
	t.Helper()
	p, s, target := codexCredentialFixture(t)
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
		if addr != "chatgpt.com:443" {
			t.Error("subscription destination changed")
		}
		return (&net.Dialer{}).DialContext(ctx, network, local.Host)
	}
	target.RequestedModel, target.ResolvedModel = "fixture-model", "fixture-model"
	target.Effort = stageplan.FrozenEffort{RequestedMode: stageplan.EffortExplicit, Value: ptr("high")}
	target.LockEnforcement = stageplan.ControlledCalls
	return f, target, p
}

func modelHeadersStream(raw, key, model string) string {
	return strings.Replace(raw, `"model":"fixture-model"`, `"model":"fixture-model","headers":{"`+key+`":"`+model+`"}`, 1)
}

func TestCodexSubscriptionForwarderActualTLSHeadersAndModelProof(t *testing.T) {
	for _, proof := range []string{"http", "sse", "sse-lowercase", "sse-x-alias", "both"} {
		t.Run(proof, func(t *testing.T) {
			var calls atomic.Int64
			f, target, _ := subscriptionFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.RequestURI() != "/backend-api/codex/responses" || r.Host != "chatgpt.com" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer opaque-fixture-access-token" || r.Header.Get("ChatGPT-Account-ID") != "fixture-upstream-account" || r.Header.Get("Originator") != "codex_cli_rs" || r.Header.Get("Version") != CLIVersion || !strings.HasPrefix(r.UserAgent(), "codex_cli_rs/"+CLIVersion) || !r.Close || r.Header.Get("Cookie") != "" || r.Header.Get("X-Api-Key") != "" {
					t.Error("fixed authenticated request drifted")
				}
				b, e := io.ReadAll(r.Body)
				if e != nil || string(b) != gateBody {
					t.Error("frozen request body changed")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Set-Cookie", "fixture-upstream-cookie")
				if proof == "http" || proof == "both" {
					w.Header().Set("OpenAI-Model", "fixture-model")
				}
				body := gateSSE()
				if proof != "http" {
					key := map[string]string{"sse": "OpenAI-Model", "sse-lowercase": "openai-model", "sse-x-alias": "X-OpenAI-Model", "both": "OpenAI-Model"}[proof]
					body = modelHeadersStream(body, key, "fixture-model")
				}
				io.WriteString(w, body)
			}))
			r, e := f.Send(context.Background(), target, []byte(gateBody))
			if e != nil || r.ReportedModel != "fixture-model" || r.StatusCode != 200 || r.ContentType != "text/event-stream" || r.Body == nil {
				t.Fatal("real TLS/model proof not accepted")
			}
			b, e := io.ReadAll(r.Body)
			if e != nil || !responsesStream(b, "fixture-model", r.PrivateMarkers...) {
				t.Fatal("validated upstream SSE lost")
			}
			r.Body.Close()
			if calls.Load() != 1 {
				t.Fatal("hidden retry")
			}
			encoded, _ := json.Marshal(r)
			if strings.Contains(string(encoded), "opaque-fixture") || strings.Contains(string(encoded), "fixture-upstream-cookie") {
				t.Fatal("private payload escaped metadata")
			}
		})
	}
}

func TestCodexSubscriptionForwarderRejectsUnprovenOrDriftedModel(t *testing.T) {
	for _, mode := range []string{"missing-proof", "http-drift", "http-duplicate", "sse-drift", "conflict", "late-drift", "header-type", "header-duplicate", "header-alias-conflict", "unknown-header", "null-header", "ordinary-model-drift", "incomplete", "gzip", "wrong-media", "oversize", "reflect-access", "reflect-id", "reflect-refresh", "escaped-reflection"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int64
			f, target, _ := subscriptionFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				if mode != "missing-proof" {
					w.Header().Set("OpenAI-Model", "fixture-model")
				}
				body := gateSSE()
				switch mode {
				case "http-drift":
					w.Header().Set("OpenAI-Model", "other")
				case "http-duplicate":
					w.Header().Add("OpenAI-Model", "fixture-model")
				case "sse-drift", "conflict":
					body = modelHeadersStream(body, "OpenAI-Model", "other")
				case "late-drift":
					body = strings.ReplaceAll(body, `"status":"completed","usage"`, `"status":"completed","headers":{"OpenAI-Model":"other"},"usage"`)
				case "header-type":
					body = strings.Replace(modelHeadersStream(body, "OpenAI-Model", "fixture-model"), `"OpenAI-Model":"fixture-model"`, `"OpenAI-Model":[]`, 1)
				case "header-duplicate", "header-alias-conflict":
					second := `"OpenAI-Model":"other"`
					if mode == "header-alias-conflict" {
						second = `"X-OpenAI-Model":"other"`
					}
					body = strings.Replace(modelHeadersStream(body, "OpenAI-Model", "fixture-model"), `"OpenAI-Model":"fixture-model"`, `"OpenAI-Model":"fixture-model",`+second, 1)
				case "unknown-header":
					body = modelHeadersStream(body, "Set-Cookie", "fixture-cookie")
				case "null-header":
					body = strings.Replace(modelHeadersStream(body, "OpenAI-Model", "fixture-model"), `{"OpenAI-Model":"fixture-model"}`, "null", 1)
				case "ordinary-model-drift":
					body = strings.Replace(body, `"model":"fixture-model"`, `"model":"other"`, 1)
				case "incomplete":
					body = strings.Replace(body, "response.completed", "response.incomplete", -1)
				case "gzip":
					w.Header().Set("Content-Encoding", "gzip")
				case "wrong-media":
					w.Header().Set("Content-Type", "application/json")
				case "oversize":
					body = strings.Repeat("x", (8<<20)+1)
				case "reflect-access", "reflect-id", "reflect-refresh", "escaped-reflection":
					secret := map[string]string{"reflect-access": "opaque-fixture-access-token", "reflect-id": "opaque-fixture-id-token", "reflect-refresh": "opaque-fixture-refresh-token", "escaped-reflection": `\u006fpaque-fixture-access-token`}[mode]
					body = strings.ReplaceAll(body, `"text":"fixture"`, `"text":"`+secret+`"`)
				}
				io.WriteString(w, body)
			}))
			response, e := f.Send(context.Background(), target, []byte(gateBody))
			if !errors.Is(e, ErrSubscriptionTransport) || response.Body != nil || calls.Load() != 1 || strings.Contains(e.Error(), "opaque-fixture") {
				t.Fatal("unproven/private response accepted")
			}
		})
	}
}

func TestCodexSubscriptionForwarderSingleAttemptAndStaticErrors(t *testing.T) {
	for _, mode := range []string{"redirect-307", "redirect-303", "rate-limit", "unauthorized", "closed-connection", "cancel-after-headers"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int64
			release := make(chan struct{})
			defer close(release)
			f, target, _ := subscriptionFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				switch mode {
				case "redirect-307", "redirect-303":
					status := 307
					if mode == "redirect-303" {
						status = 303
					}
					http.Redirect(w, r, "https://api.openai.com/v1/responses", status)
				case "rate-limit", "unauthorized":
					status := 429
					if mode == "unauthorized" {
						status = 401
					}
					w.WriteHeader(status)
					io.WriteString(w, "opaque-fixture-private-error")
				case "closed-connection":
					c, _, e := w.(http.Hijacker).Hijack()
					if e != nil {
						t.Error(e)
						return
					}
					c.Close()
				case "cancel-after-headers":
					w.Header().Set("Content-Type", "text/event-stream")
					w.Header().Set("OpenAI-Model", "fixture-model")
					w.WriteHeader(200)
					w.(http.Flusher).Flush()
					select {
					case <-r.Context().Done():
					case <-release:
					}
				}
			}))
			ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			defer cancel()
			start := time.Now()
			response, e := f.Send(ctx, target, []byte(gateBody))
			if mode == "cancel-after-headers" && time.Since(start) > time.Second {
				t.Fatal("body cancellation exceeded bound")
			}
			if mode == "rate-limit" {
				if e != nil || response.StatusCode != 429 || response.Body == nil {
					t.Fatal("429 lost")
				}
				b, e := io.ReadAll(response.Body)
				response.Body.Close()
				if e != nil || strings.Contains(string(b), "opaque-fixture") {
					t.Fatal("private 429 body returned")
				}
			} else if !errors.Is(e, ErrSubscriptionTransport) || response.Body != nil {
				t.Fatal("error or redirect accepted")
			}
			if calls.Load() != 1 {
				t.Fatal("hidden retry/fallback")
			}
		})
	}
}

func TestCodexSubscriptionForwarderRejectsBeforeDialAndAfterRotation(t *testing.T) {
	for _, mode := range []string{"account", "billing", "runtime", "model", "effort", "tools", "cancel", "nil-context", "rotate-before", "rotate-during", "rotate-delivery", "request-secret", "cache-key-control"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int64
			var path string
			f, target, p := subscriptionFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if mode == "rotate-during" {
					if e := os.WriteFile(path, []byte(strings.Replace(cacheFixture, "access-token", "another-token", 1)), 0600); e != nil {
						t.Error(e)
					}
				}
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("OpenAI-Model", "fixture-model")
				io.WriteString(w, gateSSE())
			}))
			path = p
			body := gateBody
			ctx := context.Background()
			switch mode {
			case "account":
				target.Account = "other"
			case "billing":
				target.BillingPath = "api"
			case "runtime":
				target.RuntimeVersion = "other"
			case "model":
				target.RequestedModel = "other"
			case "effort":
				target.Effort.Value = nil
			case "tools":
				body = strings.Replace(body, `"tools":[]`, `"tools":[{"type":"web_search"}]`, 1)
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "nil-context":
				ctx = nil
			case "rotate-before":
				if e := os.WriteFile(p, []byte(strings.Replace(cacheFixture, "access-token", "another-token", 1)), 0600); e != nil {
					t.Fatal(e)
				}
			case "request-secret":
				body = strings.Replace(body, `"instructions":"fixture"`, `"instructions":"opaque-fixture-refresh-token"`, 1)
			case "cache-key-control":
				body = strings.Replace(body, `"tools":[]`, `"prompt_cache_key":"bad\r\nheader","tools":[]`, 1)
			}
			r, e := f.Send(ctx, target, []byte(body))
			if mode == "rotate-delivery" {
				if e != nil || r.Body == nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(p, []byte(strings.Replace(cacheFixture, "access-token", "another-token", 1)), 0600); e != nil {
					t.Fatal(e)
				}
				b, e := io.ReadAll(r.Body)
				r.Body.Close()
				if !errors.Is(e, ErrSubscriptionTransport) || len(b) != 0 {
					t.Fatal("rotated credential still delivered payload")
				}
			} else if !errors.Is(e, ErrSubscriptionTransport) || r.Body != nil {
				t.Fatal("invalid send accepted")
			}
			want := int64(0)
			if mode == "rotate-during" || mode == "rotate-delivery" {
				want = 1
			}
			if calls.Load() != want {
				t.Fatal("preflight did not bound sends")
			}
		})
	}
}

func TestCodexSubscriptionForwarderForeignTLSIsRejected(t *testing.T) {
	var calls atomic.Int64
	f, target, _ := subscriptionFixture(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	// Restore production root pool after the test-only local CA injection.
	f.transport.TLSClientConfig.RootCAs = f.ca.pool
	r, e := f.Send(context.Background(), target, []byte(gateBody))
	if !errors.Is(e, ErrSubscriptionTransport) || r.Body != nil || calls.Load() != 0 {
		t.Fatal("foreign TLS reached HTTP")
	}
}

func TestCodexSubscriptionForwarderGateSpendsEachActualAttempt(t *testing.T) {
	var calls atomic.Int64
	f, target, _ := subscriptionFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(429)
			io.WriteString(w, "opaque-fixture-private-error")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("OpenAI-Model", "fixture-model")
		io.WriteString(w, modelHeadersStream(gateSSE(), "OpenAI-Model", "fixture-model"))
	}))
	gate := newGateFixture(t)
	gate.config.Binding.Target = target
	gate.config.Binding.Identity.Account = target.Account
	gate.config.Binding.Identity.Workspace = target.Workspace
	gate.config.Binding.Identity.CredentialIdentity = target.CredentialIdentity
	binding := cloneGate(gate.config.Binding)
	gate.config.Current = func(got Binding) bool {
		return gate.current.Load() && got.Identity == binding.Identity && got.Target.Account == target.Account && got.Target.CredentialIdentity == target.CredentialIdentity
	}
	gate.config.Forwarder = f
	g := newGate(t, gate)
	for _, status := range []int{429, 200} {
		w := send(g, gate, gateBody, "/responses")
		if w.Code != status || strings.Contains(w.Body.String(), "opaque-fixture") {
			t.Fatal("controlled TLS attempt failed or leaked")
		}
	}
	if calls.Load() != 2 || gate.permits.Load() != 2 || g.Audit().Forwarded != 2 || g.Audit().Retryable != 1 || !gateHealthy(g, gate) {
		t.Fatal("TLS attempt bypassed per-request permit")
	}
	gate.manager.RevokeRun(gate.config.Claims.RunID)
	w := send(g, gate, gateBody, "/responses")
	if w.Code < 400 || calls.Load() != 2 || gate.permits.Load() != 2 {
		t.Fatal("revoked model context sent another request")
	}
}

func TestCodexSubscriptionForwarderIgnoresEnvironmentCAAndProxy(t *testing.T) {
	t.Setenv("SSL_CERT_FILE", "/fixture-untrusted-ca.pem")
	t.Setenv("SSL_CERT_DIR", "/fixture-untrusted-ca-dir")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("ALL_PROXY", "http://127.0.0.1:1")
	ca, e := readSubscriptionCA()
	if e != nil || ca.pool == nil {
		t.Fatal("system trust replaced by environment")
	}
	if f, e := NewSubscriptionForwarder(nil); f != nil || !errors.Is(e, ErrSubscriptionTransport) {
		t.Fatal("unregistered credential accepted")
	}
	f, target, _ := subscriptionFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("OpenAI-Model", "fixture-model")
		io.WriteString(w, gateSSE())
	}))
	r, e := f.Send(context.Background(), target, []byte(gateBody))
	if e != nil || r.Body == nil {
		t.Fatal("environment proxy influenced fixed direct request")
	}
	r.Body.Close()
}

//go:build darwin || linux

package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/api"
	"github.com/yetone/magpie/internal/fusion/quota"
	"github.com/yetone/magpie/internal/fusion/runtime/glm"
	"github.com/yetone/magpie/internal/fusion/stageplan"
)

func quotaHostKey(t *testing.T, source string) string {
	t.Helper()
	dir := filepath.Join(filepath.Dir(source), "credentials")
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "glm-coding-plan.key")
	if e := os.WriteFile(path, []byte("fixture-only-quota-host-key\n"), 0600); e != nil {
		t.Fatal(e)
	}
	return path
}
func TestGLMQuotaHostReadIsIndependentFromExecution(t *testing.T) {
	path, _ := sourceFixture(t)
	key := quotaHostKey(t, path)
	var sends atomic.Int64
	factory := glmQuotaFactory("fixture-project", "fixture-glm", key, func(c glm.QuotaReaderConfig) (quota.Fetch, error) {
		return func(ctx context.Context, i quota.Identity) (quota.Snapshot, error) {
			if !c.QueryAllowed(ctx, i) {
				return quota.Snapshot{}, errors.New("fixture query revoked")
			}
			credential, e := c.LoadCredential(ctx, c.Target)
			if e != nil || credential.Key != "fixture-only-quota-host-key" {
				return quota.Snapshot{}, errors.New("fixture credential changed")
			}
			sends.Add(1)
			used := float64(12)
			now := time.Now().UTC()
			return quota.Snapshot{Identity: i, Source: glm.QuotaEndpoint, ObservedAt: &now, ReceivedAt: now, Complete: false, Status: quota.Unverified, Pool: quota.Pool{Provider: i.Provider, Region: i.Region}, Windows: []quota.Window{{Kind: "coding_plan", Unit: "percent", UsedPercent: &used}}}, nil
		}, nil
	})
	h, e := OpenQuotaControl(context.Background(), path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0", factory)
	if e != nil {
		t.Fatal(e)
	}
	serveExecutionHost(t, h)
	if h.controller != nil {
		t.Fatal("quota reader installed execution")
	}
	read := func(method, url string) api.QuotaReply {
		t.Helper()
		code, b, _ := hostHTTP(t, h, method, url, "{}", "", "")
		var reply api.QuotaReply
		if code != 200 || json.Unmarshal(b, &reply) != nil || len(reply.Routes) != 1 {
			t.Fatal("quota reply", code)
		}
		if strings.Contains(string(b), "fixture-only-quota-host-key") || strings.Contains(string(b), key) {
			t.Fatal("private query disclosure")
		}
		return reply
	}
	req, _ := http.NewRequest("POST", "http://"+h.Addr()+"/control/v1/projects/fixture-project/quota/fixture-glm/refresh", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 401 || sends.Load() != 0 {
		t.Fatal("unauthorized refresh reached reader")
	}
	q := read("GET", "/control/v1/projects/fixture-project/quota")
	if q.Routes[0].Status != quota.Unknown || sends.Load() != 0 {
		t.Fatal("GET performed query")
	}
	q = read("POST", "/control/v1/projects/fixture-project/quota/fixture-glm/refresh")
	if sends.Load() != 1 || q.Routes[0].Snapshot == nil || q.Routes[0].Status != quota.Unverified || q.Routes[0].Snapshot.Pool.Verified {
		t.Fatal("query promoted generation")
	}
	read("GET", "/control/v1/projects/fixture-project/quota")
	if sends.Load() != 1 {
		t.Fatal("GET refreshed cache")
	}
	code, b, _ := hostHTTP(t, h, "GET", "/control/v1/projects/fixture-project/configuration", "", "", "")
	if code != 200 || !strings.Contains(string(b), `"admitted":false`) {
		t.Fatal("query granted route")
	}
	code, _, _ = hostHTTP(t, h, "POST", "/control/v1/tasks/preview", `{"project_id":"fixture-project","goal":"fixture","required_roles":["design"]}`, "", "")
	if code != 422 {
		t.Fatal("unadmitted model preview succeeded", code)
	}
	if e := os.WriteFile(key, []byte("fixture-changed-quota-host-key\n"), 0600); e != nil {
		t.Fatal(e)
	}
	q = read("GET", "/control/v1/projects/fixture-project/quota")
	if q.Routes[0].Snapshot != nil || q.Routes[0].Status != quota.Unverified {
		t.Fatal("changed credential leaked cache")
	}
	code, _, _ = hostHTTP(t, h, "POST", "/control/v1/projects/fixture-project/quota/fixture-glm/refresh", "{}", "", "")
	if code != 409 || sends.Load() != 1 {
		t.Fatal("changed key queried")
	}
}
func TestQuotaHostRejectsInvalidRegistrationAndCleans(t *testing.T) {
	for _, mode := range []string{"nil", "empty", "unknown-project", "unknown-route", "revision", "account", "no-current", "error"} {
		t.Run(mode, func(t *testing.T) {
			path, _ := sourceFixture(t)
			var cleaned atomic.Int64
			var f QuotaFactory
			if mode != "nil" {
				f = func(ctx context.Context, e QuotaEnvironment) (QuotaRegistration, error) {
					p, _ := e.Source.Project("fixture-project")
					r := p.Configuration.Routes[0]
					q := api.QuotaSource{Route: stageplan.RouteRef{ID: r.ID, Revision: r.Revision}, Identity: quota.Identity{Provider: "bigmodel", Account: r.Account, Workspace: r.Workspace, Region: "CN", Generation: 1}, Current: func(context.Context, quota.Identity) bool { return true }}
					id := "fixture-project"
					switch mode {
					case "unknown-project":
						id = "unknown"
					case "unknown-route":
						q.Route.ID = "unknown"
					case "revision":
						q.Route.Revision++
					case "account":
						q.Identity.Account = "unknown"
					case "no-current":
						q.Current = nil
					}
					reg := QuotaRegistration{Sources: map[string][]api.QuotaSource{id: {q}}, Close: func(context.Context) error { cleaned.Add(1); return nil }}
					if mode == "empty" {
						reg.Sources = nil
					}
					if mode == "error" {
						return reg, errors.New("fixture-private-error")
					}
					return reg, nil
				}
			}
			h, e := OpenQuotaControl(context.Background(), path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0", f)
			if h != nil || e != ErrControlHost || strings.Contains(e.Error(), "fixture-private-error") {
				t.Fatal("invalid query host accepted")
			}
			if mode != "nil" && cleaned.Load() != 1 {
				t.Fatal("cleanup not owned")
			}
		})
	}
}

func TestGLMQuotaHostRejectsInvalidScopeBeforeQuery(t *testing.T) {
	for _, mode := range []string{"unknown-project", "unknown-route", "ambiguous-revision", "wrong-provider", "missing-key", "public-key", "key-in-project", "cancelled", "no-reader"} {
		t.Run(mode, func(t *testing.T) {
			path, d := sourceFixture(t)
			key := quotaHostKey(t, path)
			project, route := "fixture-project", "fixture-glm"
			ctx := context.Background()
			var constructed atomic.Int64
			switch mode {
			case "unknown-project":
				project = "unknown"
			case "unknown-route":
				route = "unknown"
			case "ambiguous-revision":
				r := d.Routes[0]
				r.Revision = 2
				d.Routes = append(d.Routes, r)
				d.Projects[0].Routes = append(d.Projects[0].Routes, stageplan.RouteRef{ID: r.ID, Revision: 2})
				writeSource(t, path, d)
			case "wrong-provider":
				d.Routes[0].NativeRoute = "codex-chatgpt"
				writeSource(t, path, d)
			case "missing-key":
				key += "missing"
			case "public-key":
				if e := os.Chmod(key, 0644); e != nil {
					t.Fatal(e)
				}
			case "key-in-project":
				dir := filepath.Join(d.Projects[0].Path, "credentials")
				if e := os.Mkdir(dir, 0700); e != nil {
					t.Fatal(e)
				}
				key = filepath.Join(dir, "glm-coding-plan.key")
				if e := os.WriteFile(key, []byte("fixture-only-quota-host-key"), 0600); e != nil {
					t.Fatal(e)
				}
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			constructor := func(glm.QuotaReaderConfig) (quota.Fetch, error) {
				constructed.Add(1)
				return nil, errors.New("must not construct")
			}
			if mode == "no-reader" {
				constructor = nil
			}
			h, e := OpenQuotaControl(ctx, path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0", glmQuotaFactory(project, route, key, constructor))
			if h != nil || e != ErrControlHost || constructed.Load() != 0 {
				t.Fatal("invalid scope reached reader", mode)
			}
		})
	}
}
func TestGLMQuotaHostProductReaderOpensWithoutOutboundOrAdmission(t *testing.T) {
	path, _ := sourceFixture(t)
	key := quotaHostKey(t, path)
	h, e := OpenGLMQuotaControl(context.Background(), path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0", "fixture-project", "fixture-glm", key)
	if e != nil {
		t.Fatal(e)
	}
	serveExecutionHost(t, h)
	code, b, _ := hostHTTP(t, h, "GET", "/control/v1/projects/fixture-project/quota", "", "", "")
	var q api.QuotaReply
	if code != 200 || json.Unmarshal(b, &q) != nil || q.Routes[0].Status != quota.Unknown || h.controller != nil {
		t.Fatal("product query setup failed")
	}
}

func TestQuotaHostWaitsForDetachedReaderAfterCloseOrSourceRevocation(t *testing.T) {
	for _, mode := range []string{"close", "source"} {
		t.Run(mode, func(t *testing.T) {
			path, d := sourceFixture(t)
			entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var once sync.Once
			finish := func() { once.Do(func() { close(release) }) }
			defer finish()
			var cleaned atomic.Int64
			h, e := OpenQuotaControl(context.Background(), path, filepath.Join(filepath.Dir(path), "control"), "127.0.0.1:0", func(ctx context.Context, env QuotaEnvironment) (QuotaRegistration, error) {
				p, _ := env.Source.Project("fixture-project")
				r := p.Configuration.Routes[0]
				return QuotaRegistration{Sources: map[string][]api.QuotaSource{"fixture-project": {{Route: stageplan.RouteRef{ID: r.ID, Revision: r.Revision}, Identity: quota.Identity{Provider: "bigmodel", Account: r.Account, Workspace: r.Workspace, Region: "CN", Generation: 1}, Current: func(context.Context, quota.Identity) bool { return true }, Fetch: func(ctx context.Context, q quota.Identity) (quota.Snapshot, error) {
					close(entered)
					<-ctx.Done()
					close(cancelled)
					<-release
					return quota.Snapshot{}, ctx.Err()
				}}}}, Close: func(context.Context) error { cleaned.Add(1); return nil }}, nil
			})
			if e != nil {
				t.Fatal(e)
			}
			serveExecutionHost(t, h)
			token, e := os.ReadFile(filepath.Join(h.root, "management.token"))
			if e != nil {
				t.Fatal(e)
			}
			requestCtx, disconnect := context.WithCancel(context.Background())
			defer disconnect()
			req, e := http.NewRequestWithContext(requestCtx, "POST", "http://"+h.Addr()+"/control/v1/projects/fixture-project/quota/fixture-glm/refresh", strings.NewReader("{}"))
			if e != nil {
				t.Fatal(e)
			}
			req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
			req.Header.Set("Content-Type", "application/json")
			done := make(chan struct{})
			go func() {
				defer close(done)
				response, e := (&http.Client{Timeout: 3 * time.Second}).Do(req)
				if e == nil {
					io.Copy(io.Discard, response.Body)
					response.Body.Close()
				}
			}()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("reader did not start")
			}
			disconnect()
			select {
			case <-cancelled:
				t.Fatal("one HTTP disconnect cancelled shared query")
			case <-time.After(20 * time.Millisecond):
			}
			if mode == "source" {
				d.Revision++
				writeSource(t, path, d)
				select {
				case <-cancelled:
				case <-time.After(2 * time.Second):
					t.Fatal("source revocation did not cancel query")
				}
			}
			short, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			if e := h.CloseContext(short); !errors.Is(e, context.DeadlineExceeded) {
				t.Fatal("close did not retain active reader", e)
			}
			select {
			case <-cancelled:
			case <-time.After(time.Second):
				t.Fatal("owned query not cancelled")
			}
			if cleaned.Load() != 0 {
				t.Fatal("cleanup before reader stop")
			}
			if _, e := h.store.Capacity(); e != nil {
				t.Fatal("Store closed before reader stopped")
			}
			finish()
			wait, cancelWait := context.WithTimeout(context.Background(), time.Second)
			defer cancelWait()
			if e := h.CloseContext(wait); e != nil {
				t.Fatal(e)
			}
			if cleaned.Load() != 1 {
				t.Fatal("owned cleanup count")
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("HTTP reader did not exit")
			}
		})
	}
}

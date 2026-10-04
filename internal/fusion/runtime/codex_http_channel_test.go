package runtime

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/stageplan"
)

func codexHTTPFixture(t *testing.T) (*CodexChannel, *policy.PendingModelGrant, Spec, string) {
	t.Helper()
	r := codexChannelRun()
	r.Target.Route = stageplan.RouteRef{ID: "fixture-codex-route", Revision: 1}
	r.Target.LockEnforcement = stageplan.ControlledCalls
	r.Target.Effort.RequestedMode = stageplan.EffortExplicit
	claims := policy.Claims{TaskID: r.TaskID, RunID: r.ID, Role: r.Role, Attempt: r.Attempt, PlanRevision: r.PlanRevision, Generation: r.Generation, ProjectID: "fixture-project", Audience: policy.ModelAudience}
	m := policy.NewManager("fixture-management", func(c policy.Claims) bool { return c == claims }, nil)
	grant, e := m.PrepareModel(claims, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	root, cwd := "/private/fusion-worker", "/private/fusion-project"
	ch, e := NewCodexHTTPChannel(r, root, cwd, "fixture-launch", func(context.Context, io.ReadWriteCloser) error { return nil }, func() bool { return true }, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) }), grant)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { ch.Close() })
	return ch, grant, Spec{Root: root, Workspace: cwd, ExecutableHash: CodexExecutableSHA256, Args: []string{"app-server", "--listen", "stdio://", "--strict-config"}, NativeSessionID: "fixture-launch", CodexChannel: ch, Timeout: time.Second}, claims.ProjectID
}
func TestCodexHTTPChannelOwnsPortsAndFrozenPreparedAuthority(t *testing.T) {
	ch, grant, s, project := codexHTTPFixture(t)
	c, e := s.modelChannel()
	if e != nil || c == nil || c != ch.models || !c.valid(ch.run, project, time.Second) || !ch.valid(ch.run, s) {
		t.Fatal("scope refused", e)
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: time.Second}
	defer client.CloseIdleConnections()
	for _, x := range []struct{ family, host string }{{"tcp4", "127.0.0.1"}, {"tcp6", "::1"}} {
		address := net.JoinHostPort(x.host, c.port)
		if l, e := net.Listen(x.family, address); e == nil {
			l.Close()
			t.Fatal("port not owned")
		}
		r, e := client.Get("http://" + address + "/responses")
		if e != nil {
			t.Fatal(e)
		}
		r.Body.Close()
		if r.StatusCode != 403 {
			t.Fatal(r.StatusCode)
		}
	}
	if _, e = json.Marshal(ch); e == nil {
		t.Fatal("channel is JSON authority")
	}
	secret, e := grant.Secret()
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(ch.String(), secret) {
		t.Fatal("secret exposed")
	}
	run := ch.run
	run.Generation++
	if c.valid(run, project, time.Second) {
		t.Fatal("wrong scope allowed")
	}
	if c.valid(ch.run, "other", time.Second) || c.valid(ch.run, project, 45*time.Second) {
		t.Fatal("project/lifetime escaped")
	}
	if !c.acquire(ch.run, project, time.Second) || !ch.acquire(ch.run, s) || ch.Close() == nil {
		t.Fatal("active ports released")
	}
	if l, e := net.Listen("tcp6", net.JoinHostPort("::1", c.port)); e == nil {
		l.Close()
		t.Fatal("active port lost")
	}
	c.release()
	ch.release()
	if ch.valid(ch.run, s) || c.valid(ch.run, project, time.Second) {
		t.Fatal("channel reused")
	}
}
func TestCodexHTTPChannelRejectsMissingTransportAndUnsafeTarget(t *testing.T) {
	for _, mode := range []string{"handler", "grant", "billing", "lock", "effort", "plugin", "revision", "version", "model", "current"} {
		t.Run(mode, func(t *testing.T) {
			ch, g, s, _ := codexHTTPFixture(t)
			r := ch.run
			h := http.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			current := func() bool { return true }
			switch mode {
			case "handler":
				h = nil
			case "grant":
				g = nil
			case "billing":
				r.Target.BillingPath = "api"
			case "lock":
				r.Target.LockEnforcement = "other"
			case "effort":
				r.Target.Effort.Value = nil
			case "plugin":
				v := "other"
				r.Target.PluginVersion = &v
			case "revision":
				r.Target.Route.Revision = 0
			case "version":
				r.Target.RuntimeVersion = "other"
			case "model":
				r.Target.RequestedModel = "bad model"
			case "current":
				current = func() bool { return false }
			}
			if bad, e := NewCodexHTTPChannel(r, s.Root, s.Workspace, s.NativeSessionID, func(context.Context, io.ReadWriteCloser) error { return nil }, current, h, g); e == nil {
				bad.Close()
				t.Fatal("unverified transport allowed")
			}
		})
	}
}

package runtime

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

func channelFixture(t *testing.T) (*ClaudeChannel, *policy.PendingModelGrant, store.StageRun, string) {
	t.Helper()
	c := policy.Claims{TaskID: "fixture-task", RunID: "fixture-run", Role: stageplan.Design, Attempt: 1, PlanRevision: 1, Generation: 1, ProjectID: "fixture-project", Audience: policy.ModelAudience}
	m := policy.NewManager("fixture-manager", func(got policy.Claims) bool { return got == c }, nil)
	p, e := m.PrepareModel(c, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	target := stageplan.ExecutionTarget{Route: stageplan.RouteRef{ID: "fixture-glm", Revision: 1}, RequestedModel: "glm-5.3", ResolvedModel: "glm-5.3", RuntimeVersion: ClaudeCLIVersion, Account: "fixture-account", Workspace: "fixture-workspace", CredentialIdentity: "fixture-credential", BillingPath: "coding_plan", Capabilities: []string{"text"}, LockEnforcement: stageplan.ControlledCalls}
	ch, e := NewClaudeChannel(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(403) }), p, target)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { ch.Close() })
	r := store.StageRun{ID: c.RunID, TaskID: c.TaskID, Role: c.Role, Attempt: c.Attempt, PlanRevision: c.PlanRevision, Generation: c.Generation, Target: target}
	return ch, p, r, c.ProjectID
}

func TestClaudeChannelOwnsBothLoopbackFamiliesAndScopedPreparation(t *testing.T) {
	ch, _, r, project := channelFixture(t)
	if !ch.valid(r, project, 3*time.Second) {
		t.Fatal("valid prepared channel denied")
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: time.Second}
	defer client.CloseIdleConnections()
	for _, family := range []struct{ network, host string }{{"tcp4", "127.0.0.1"}, {"tcp6", "::1"}} {
		address := net.JoinHostPort(family.host, ch.port)
		if listener, e := net.Listen(family.network, address); e == nil {
			listener.Close()
			t.Fatal("controller did not exclusively own loopback address")
		}
		response, e := client.Get("http://" + address + "/api/anthropic/v1/messages")
		if e != nil {
			t.Fatal(e)
		}
		response.Body.Close()
		if response.StatusCode != 403 {
			t.Fatal("families did not reach same handler")
		}
	}
	for _, mode := range []string{"scope", "project", "target", "timeout", "activated", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			ch, p, r, project := channelFixture(t)
			timeout := 3 * time.Second
			switch mode {
			case "scope":
				r.Generation++
			case "project":
				project = "fixture-other"
			case "target":
				r.Target.CredentialIdentity = "fixture-other"
			case "timeout":
				timeout = 45 * time.Second
			case "activated":
				if e := p.Activate(); e != nil {
					t.Fatal(e)
				}
			case "cancelled":
				p.Cancel()
			}
			if ch.valid(r, project, timeout) {
				t.Fatal("invalid or already active channel accepted", mode)
			}
		})
	}
}

func TestClaudeChannelRetainsPortsUntilManagedProcessIsReaped(t *testing.T) {
	ch, _, r, project := channelFixture(t)
	if !ch.acquire(r, project, time.Second) {
		t.Fatal("prepared channel not acquired")
	}
	if ch.Close() == nil || ch.acquire(r, project, time.Second) {
		t.Fatal("active channel released or reused")
	}
	if listener, e := net.Listen("tcp6", net.JoinHostPort("::1", ch.port)); e == nil {
		listener.Close()
		t.Fatal("active IPv6 port lost")
	}
	ch.release()
	if ch.valid(r, project, time.Second) {
		t.Fatal("released channel accepted")
	}
	if e := ch.Close(); e != nil {
		t.Fatal(e)
	}
}

func TestClaudeChannelClosedOrFailedListenerCannotLaunch(t *testing.T) {
	for _, mode := range []string{"closed", "listener_failed"} {
		t.Run(mode, func(t *testing.T) {
			ch, p, r, project := channelFixture(t)
			if mode == "closed" {
				if e := ch.Close(); e != nil {
					t.Fatal(e)
				}
			} else {
				ch.listeners[0].Close()
				select {
				case <-ch.failure:
				case <-time.After(time.Second):
					t.Fatal("listener failure not observed")
				}
			}
			if ch.valid(r, project, time.Second) || ch.acquire(r, project, time.Second) {
				t.Fatal("unowned channel launched")
			}
			if _, e := p.Secret(); e == nil {
				t.Fatal("failed channel retained grant")
			}
		})
	}
}

func TestClaudeChannelCopiesTargetAndNeverSerializesGrant(t *testing.T) {
	ch, p, r, project := channelFixture(t)
	raw, e := p.Secret()
	if e != nil {
		t.Fatal(e)
	}
	r.Target.Capabilities[0] = "fixture-other"
	if ch.valid(r, project, 3*time.Second) {
		t.Fatal("mutable caller changed channel target")
	}
	r.Target.Capabilities = []string{"text"}
	if !ch.valid(r, project, 3*time.Second) {
		t.Fatal("channel target changed")
	}
	b, e := json.Marshal(ch)
	if e != nil || strings.Contains(string(b), raw) {
		t.Fatal("channel JSON exposed secret")
	}
	for _, edit := range []func(*stageplan.ExecutionTarget){func(t *stageplan.ExecutionTarget) { t.RequestedModel = "glm-other" }, func(t *stageplan.ExecutionTarget) { t.RuntimeVersion = "fixture-other" }, func(t *stageplan.ExecutionTarget) { t.BillingPath = "paid_api" }, func(t *stageplan.ExecutionTarget) { t.LockEnforcement = "" }} {
		target := r.Target
		edit(&target)
		if extra, e := NewClaudeChannel(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), p, target); e == nil {
			extra.Close()
			t.Fatal("unapproved Native target accepted")
		}
	}
}

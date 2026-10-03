package runtime

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

func grokChannelFixture(t *testing.T) (*GrokChannel, *policy.PendingModelGrant, store.StageRun, string) {
	t.Helper()
	claims := policy.Claims{TaskID: "fixture-task", RunID: "fixture-run", Role: stageplan.Design, Attempt: 1, PlanRevision: 1, Generation: 1, ProjectID: "fixture-project", Audience: policy.ModelAudience}
	manager := policy.NewManager("fixture-controller", func(c policy.Claims) bool { return c == claims }, nil)
	pending, e := manager.PrepareModel(claims, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	target := stageplan.ExecutionTarget{Route: stageplan.RouteRef{ID: "fixture-grok", Revision: 1}, RequestedModel: "fixture-model", ResolvedModel: "fixture-model", RuntimeVersion: GrokCLIVersion, Account: "fixture-account", Workspace: "fixture-workspace", CredentialIdentity: "fixture-identity", BillingPath: "subscription", Capabilities: []string{"text"}, LockEnforcement: stageplan.ControlledCalls}
	channel, e := NewGrokChannel(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(403) }), pending, target)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { channel.Close() })
	return channel, pending, store.StageRun{ID: claims.RunID, TaskID: claims.TaskID, Role: claims.Role, Attempt: 1, PlanRevision: 1, Generation: 1, Target: target}, claims.ProjectID
}

func TestGrokChannelExclusivePortsAndPreparedScope(t *testing.T) {
	ch, _, run, project := grokChannelFixture(t)
	if !ch.valid(run, project, time.Second) || !strings.HasSuffix(ch.endpoint, "/v1") {
		t.Fatal("Grok prepared channel refused")
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: time.Second}
	defer client.CloseIdleConnections()
	for _, family := range []struct{ network, host string }{{"tcp4", "127.0.0.1"}, {"tcp6", "::1"}} {
		addr := net.JoinHostPort(family.host, ch.port)
		if l, e := net.Listen(family.network, addr); e == nil {
			l.Close()
			t.Fatal("port unowned")
		}
		r, e := client.Get("http://" + addr + "/v1/chat/completions")
		if e != nil {
			t.Fatal(e)
		}
		r.Body.Close()
		if r.StatusCode != 403 {
			t.Fatal("handler changed")
		}
	}
	run.Generation++
	if ch.valid(run, project, time.Second) {
		t.Fatal("foreign generation accepted")
	}
	run.Generation--
	if ch.valid(run, "foreign-project", time.Second) || ch.valid(run, project, 45*time.Second) {
		t.Fatal("foreign project/expiry accepted")
	}
	if !ch.acquire(run, project, time.Second) || ch.Close() == nil || ch.acquire(run, project, time.Second) {
		t.Fatal("active ports released or reused")
	}
	ch.release()
	if ch.valid(run, project, time.Second) {
		t.Fatal("closed channel accepted")
	}
}

func TestGrokChannelFreezesTargetAndHidesCapability(t *testing.T) {
	ch, p, run, project := grokChannelFixture(t)
	secret, e := p.Secret()
	if e != nil {
		t.Fatal(e)
	}
	run.Target.Capabilities[0] = "changed"
	if ch.valid(run, project, time.Second) {
		t.Fatal("caller changed frozen target")
	}
	run.Target.Capabilities = []string{"text"}
	if !ch.valid(run, project, time.Second) {
		t.Fatal("deep copy lost")
	}
	raw, e := json.Marshal(ch)
	if e != nil || strings.Contains(string(raw), secret) || strings.Contains(fmt.Sprintf("%#v %s", ch, ch), secret) {
		t.Fatal("capability exposed")
	}
}

func TestGrokChannelRejectsInvalidRuntimeAndTarget(t *testing.T) {
	_, p, run, _ := grokChannelFixture(t)
	for _, mode := range []string{"model", "alias", "version", "billing", "lock", "credential", "plugin"} {
		target := run.Target
		switch mode {
		case "model":
			target.RequestedModel = "bad\nmodel"
			target.ResolvedModel = target.RequestedModel
		case "alias":
			target.ResolvedModel = "other"
		case "version":
			target.RuntimeVersion = ClaudeCLIVersion
		case "billing":
			target.BillingPath = "paid_api"
		case "lock":
			target.LockEnforcement = ""
		case "credential":
			target.CredentialIdentity = ""
		case "plugin":
			v := "fixture"
			target.PluginVersion = &v
		}
		if ch, e := NewGrokChannel(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), p, target); e == nil {
			ch.Close()
			t.Fatal("invalid target accepted", mode)
		}
	}
}

func TestSpecRejectsDualOrEmptyTypedChannels(t *testing.T) {
	grok, _, _, _ := grokChannelFixture(t)
	claude, _, _, _ := channelFixture(t)
	if _, e := (Spec{GrokChannel: grok, ClaudeChannel: claude}).modelChannel(); e == nil {
		t.Fatal("ambiguous channel accepted")
	}
	if _, e := (Spec{GrokChannel: &GrokChannel{}}).modelChannel(); e == nil {
		t.Fatal("empty Grok wrapper accepted")
	}
	if _, e := (Spec{ClaudeChannel: &ClaudeChannel{}}).modelChannel(); e == nil {
		t.Fatal("empty Claude wrapper accepted")
	}
	ch, e := (Spec{GrokChannel: grok}).modelChannel()
	if e != nil || ch == nil {
		t.Fatal("valid Grok channel denied")
	}
	ch, e = (Spec{}).modelChannel()
	if e != nil || ch != nil {
		t.Fatal("plain isolated worker changed")
	}
}

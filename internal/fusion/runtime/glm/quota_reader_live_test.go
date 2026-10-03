package glm

import (
	"context"
	"flag"
	"testing"

	"github.com/yetone/magpie/internal/fusion/quota"
	"github.com/yetone/magpie/internal/fusion/stageplan"
)

var liveQuotaKey = flag.String("fusion-live-glm-quota-key", "", "explicit private CN key file for a single read-only live quota diagnostic; no model request")

// This explicit opt-in test is a diagnostic collector, not production credential
// or quota-purpose admission. FileCredential pins the local file, without
// claiming upstream account or pool verification.
func TestGLMQuotaReaderLiveCN(t *testing.T) {
	if *liveQuotaKey == "" {
		t.Skip("explicit private-key live quota opt-in required")
	}
	i := quota.Identity{Provider: "bigmodel", Account: "local-account-unverified", Workspace: "local-workspace-unverified", Region: "CN", Generation: 1}
	target := stageplan.ExecutionTarget{Account: i.Account, Workspace: i.Workspace, CredentialIdentity: "private-diagnostic-only", BillingPath: "coding_plan"}
	credential, e := NewFileCredential(*liveQuotaKey, FileCredentialScope{Account: i.Account, Workspace: i.Workspace, Identity: target.CredentialIdentity})
	if e != nil {
		t.Fatal("private credential registration refused")
	}
	r, e := NewQuotaReader(QuotaReaderConfig{Identity: i, Target: target, QueryAllowed: func(ctx context.Context, got quota.Identity) bool { return ctx.Err() == nil && got == i }, LoadCredential: credential.Load})
	if e != nil {
		t.Fatal("live quota diagnostic setup refused")
	}
	s, e := r.Read(context.Background(), i)
	if e != nil {
		t.Fatal(e)
	}
	if s.Pool.Verified || s.Complete || s.Status == quota.Available || s.Status == quota.Zero {
		t.Fatal("live query promoted identity/pool admission")
	}
	modelWindows := 0
	for n, w := range s.Windows {
		if w.Kind != "coding_plan" {
			continue
		}
		modelWindows++
		if w.UsedPercent == nil {
			t.Logf("model quota window %d used_percent=unknown", n)
			continue
		}
		t.Logf("model quota window %d used_percent=%g", n, *w.UsedPercent)
	}
	if modelWindows == 0 {
		t.Fatal("no Coding Plan model window observed")
	}
	t.Logf("single live CN quota read succeeded; model_windows=%d status=%s; real model calls=0, identity/pool/model/effort/billing/generation admission remain unverified", modelWindows, s.Status)
}

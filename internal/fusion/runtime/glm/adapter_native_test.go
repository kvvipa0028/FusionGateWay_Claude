//go:build darwin

package glm

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/quota"
	"github.com/yetone/magpie/internal/fusion/stageplan"
)

func TestGLMAdapterPinnedNativeLifecycle(t *testing.T) {
	if *nativeGateCLI == "" {
		t.Skip("explicit pinned Adapter Native fixture only")
	}
	for _, mode := range []string{"design", "implementation", "sdk_retry", "wrong_credential", "late_quota", "current_identity", "cancel_inflight"} {
		t.Run(mode, func(t *testing.T) {
			exe, e := filepath.EvalSymlinks(*nativeGateCLI)
			if e != nil {
				t.Fatal(e)
			}
			role := stageplan.Design
			if mode == "implementation" {
				role = stageplan.Implementation
			}
			c, r, in, env, loads := adapterFixture(t, exe, role)
			var calls atomic.Int64
			entered := make(chan struct{}, 1)
			c.Transport = fixtureRoundTrip(func(req *http.Request) (*http.Response, error) {
				n := calls.Add(1)
				if req.Header.Get("Authorization") != "Bearer fixture-controller-key" || req.URL.String() != Endpoint+"/v1/messages?beta=true" {
					t.Error("Adapter controller route/credential drift")
				}
				if mode == "sdk_retry" && n == 1 {
					return &http.Response{StatusCode: 429, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("fixture-error"))}, nil
				}
				if mode == "cancel_inflight" {
					entered <- struct{}{}
					<-req.Context().Done()
					return nil, req.Context().Err()
				}
				body := nativeFixtureSSE(t)
				if mode == "implementation" && n == 1 {
					body = nativeFixtureToolSSE(t, n, "Edit", map[string]any{"file_path": filepath.Join(in.Workspace, "created.txt"), "old_string": "", "new_string": "synthetic adapter created file\n"})
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			loader := c.LoadCredential
			if mode == "wrong_credential" || mode == "late_quota" {
				c.LoadCredential = func(ctx context.Context, target stageplan.ExecutionTarget) (Credential, error) {
					key, e := loader(ctx, target)
					if mode == "wrong_credential" {
						key.Identity = "fixture-other"
					} else {
						env.Quota.Status = quota.Unknown
					}
					return key, e
				}
			}
			if mode == "current_identity" {
				c.Current = func(Binding) bool { return false }
			}
			a, e := NewAdapter(c)
			if e != nil {
				t.Fatal(e)
			}
			if _, _, e = a.Observation(r.ID, r.Generation); e == nil {
				t.Fatal("prelaunch observation available")
			}
			h, e := a.Start(context.Background(), r, in)
			if mode == "wrong_credential" || mode == "late_quota" || mode == "current_identity" {
				if e == nil || h != nil || calls.Load() != 0 {
					t.Fatal("blocked credential/admission reached Native")
				}
				if _, e = os.Stat(filepath.Join(in.Root, "launch.json")); !os.IsNotExist(e) {
					t.Fatal("blocked Adapter spawned")
				}
				want := int64(1)
				if mode == "current_identity" {
					want = 0
				}
				if loads.Load() != want {
					t.Fatal("unexpected secret load")
				}
				return
			}
			if e != nil || h == nil {
				if h != nil {
					h.Cancel()
				}
				t.Fatal("Native Adapter start", e)
			}
			defer h.Cancel()
			if mode == "cancel_inflight" {
				select {
				case <-entered:
				case <-time.After(10 * time.Second):
					t.Fatal("Adapter request never reached controlled transport")
				}
				if _, _, e = a.Observation(r.ID, r.Generation); e == nil {
					t.Fatal("running Adapter observation released")
				}
				if e = h.Cancel(); e != nil {
					t.Fatal(e)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			result, e := h.Wait(ctx)
			wantState := "succeeded"
			if mode == "cancel_inflight" {
				wantState = "cancelled"
			}
			if e != nil || result.State != wantState || !result.StoppedVerified || !a.VerifyStop(result.Proof) {
				t.Fatal("Adapter lifecycle failed", e, result.State, result.ExitCode)
			}
			out, text, e := a.Observation(r.ID, r.Generation)
			wantText := "FUSION_FIXTURE_OK"
			if mode == "cancel_inflight" {
				wantText = ""
			}
			if e != nil || out.State != wantState || text != wantText || out.StrictLockVerified || out.BillingVerified || out.QuotaVerified || out.UpstreamVerified {
				t.Fatal("Adapter promoted unverified observation", e, out.State)
			}
			if _, _, e = a.Observation(r.ID, r.Generation+1); e == nil {
				t.Fatal("foreign generation observation accepted")
			}
			want := int64(1)
			if mode == "implementation" || mode == "sdk_retry" {
				want = 2
			}
			budget, e := c.Scheduler.Store.Budget(r.TaskID)
			if e != nil || int64(budget.UsedCalls) != want || calls.Load() != want || loads.Load() != 1 {
				t.Fatal("Adapter bypassed Scheduler budget")
			}
			if mode == "implementation" {
				raw, e := os.ReadFile(filepath.Join(in.Workspace, "created.txt"))
				if e != nil || string(raw) != "synthetic adapter created file\n" {
					t.Fatal("Adapter file result not produced")
				}
			}
			if e = a.Release(result.Proof); e != nil {
				t.Fatal(e)
			}
			t.Logf("production GLM Adapter -> fixed Native -> %d synthetic HTTP/persisted calls -> validated result and verified stop; real model calls=0, actual route/admission/billing/quota unverified", want)
		})
	}
}

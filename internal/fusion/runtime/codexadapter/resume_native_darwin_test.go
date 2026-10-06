//go:build darwin

package codexadapter

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/runtime/codex"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

// Actual pinned 0.160.0 end-to-end checkpoint/resume through the production
// adapter and sealed archives: run A succeeds and persists its thread rollout,
// Checkpoint seals it only after verified stop and release, and a distinct
// prepared run B resumes the exact thread so its upstream request carries the
// prior history.
func TestCodexAdapterPinnedResume(t *testing.T) {
	if *nativeCLI == "" {
		t.Skip("explicit pinned private Codex Adapter only")
	}
	for _, mode := range []string{"resume", "digest_drift", "cwd_drift", "reopen", "foreign_archive"} {
		t.Run(mode, func(t *testing.T) {
			c, r, in, calls, _, _ := adapterFixture(t)
			exe, e := filepath.EvalSymlinks(*nativeCLI)
			if e != nil {
				t.Fatal(e)
			}
			c.Executable = exe
			var mu sync.Mutex
			var bodies []string
			c.Forwarder = fakeForwarder(func(ctx context.Context, target stageplan.ExecutionTarget, raw []byte) (codex.ForwardResponse, error) {
				calls.Add(1)
				mu.Lock()
				bodies = append(bodies, string(raw))
				mu.Unlock()
				return codex.ForwardResponse{StatusCode: 200, ContentType: "text/event-stream", ReportedModel: target.ResolvedModel, Body: io.NopCloser(strings.NewReader(textSSE(target.ResolvedModel)))}, nil
			})
			a, e := NewAdapter(c)
			if e != nil {
				t.Fatal(e)
			}
			archives, e := NewArchives(privateDir(t))
			if e != nil {
				t.Fatal(e)
			}
			defer archives.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
			defer cancel()
			specA := in
			specA.Input = []byte("Remember the codeword KUMQUAT-7. Do not use tools.")
			hA, e := a.Start(ctx, r, specA)
			if e != nil || hA == nil {
				t.Fatal("original launch refused", e)
			}
			defer func() { hA.Cancel(); hA.Wait(context.Background()) }()
			resultA, e := hA.Wait(ctx)
			if e != nil || resultA.State != "succeeded" || !resultA.StoppedVerified || !a.VerifyStop(resultA.Proof) {
				t.Fatal("original Native failed", e, resultA.State)
			}
			if e = a.Release(resultA.Proof); e != nil {
				t.Fatal(e)
			}
			ref, e := a.Checkpoint(ctx, r.ID, r.Generation, archives)
			if e != nil || !validRefShape(ref) {
				t.Fatal("checkpoint refused or malformed", e)
			}
			if _, e := a.config.Scheduler.Store.Reservation(r.ID); !errors.Is(e, store.ErrNotFound) {
				t.Fatal("checkpoint run retained reservation")
			}
			// The sealed archive is verifiable after a full reopen.
			if mode == "reopen" {
				path := archives.path
				if e := archives.Close(); e != nil {
					t.Fatal(e)
				}
				archives, e = NewArchives(path)
				if e != nil {
					t.Fatal(e)
				}
				defer archives.Close()
			}
			// A second capture of the same stopped run is idempotent.
			again, e := a.Checkpoint(ctx, r.ID, r.Generation, archives)
			if e != nil || again != ref {
				t.Fatal("idempotent recapture failed", e)
			}
			consume := archives
			if mode == "foreign_archive" {
				// The seal is meaningless under a different archive key.
				foreign, e := NewArchives(privateDir(t))
				if e != nil {
					t.Fatal(e)
				}
				defer foreign.Close()
				if _, e := foreign.Info(ref); e == nil {
					t.Fatal("foreign archive verified another key's seal")
				}
				consume = foreign
			}
			next, e := a.config.Scheduler.Prepare(ctx, store.StartRequest{TaskID: r.TaskID, Role: r.Role, PlanRevision: r.PlanRevision, Owner: "fixture-resume-owner", TTL: time.Minute, Target: r.Target})
			if e != nil {
				t.Fatal(e)
			}
			specB := managed.Spec{Source: in.Source, Root: privateDir(t), Workspace: in.Workspace, Timeout: 20 * time.Second, Input: []byte("Reply with only the codeword I gave you earlier. Do not use tools.")}
			drifted := ref
			switch mode {
			case "digest_drift":
				drifted.Digest = strings.Repeat("f", 64)
			case "cwd_drift":
				specB.Workspace = privateDir(t)
			}
			hB, e := a.ResumeCheckpoint(ctx, next, specB, consume, drifted)
			if mode != "resume" && mode != "reopen" {
				if hB != nil || e == nil {
					hB.Cancel()
					t.Fatal("drifted resume launched")
				}
				budget, _ := a.config.Scheduler.Store.Budget(r.TaskID)
				if budget.UsedCalls != 1 || calls.Load() != 1 {
					t.Fatal("refused resume spent upstream", budget.UsedCalls, calls.Load())
				}
				if _, e := a.config.Scheduler.Store.Reservation(next.ID); errors.Is(e, store.ErrNotFound) {
					t.Fatal("refused resume dropped the held reservation")
				}
				if _, e := os.Stat(filepath.Join(specB.Root, "launch.json")); !os.IsNotExist(e) {
					t.Fatal("refused resume published launch intent")
				}
				t.Logf("mode=%s originalHTTP=1 resumedHTTP=0 nativeLaunches=1 sealedCapture=1 refusal=prelaunch", mode)
				return
			}
			if e != nil || hB == nil {
				t.Fatal("resumed launch refused", e)
			}
			defer func() { hB.Cancel(); hB.Wait(context.Background()) }()
			resultB, e := hB.Wait(ctx)
			if e != nil || resultB.State != "succeeded" || !resultB.StoppedVerified || !a.VerifyStop(resultB.Proof) {
				t.Fatal("resumed Native failed", e, resultB.State)
			}
			if e = a.Release(resultB.Proof); e != nil {
				t.Fatal(e)
			}
			outB, textB, e := a.Observation(next.ID, next.Generation)
			if e != nil || outB.State != "succeeded" || textB != "fixture" || outB.ThreadID == "" {
				t.Fatal("resumed observation wrong", e, outB.State)
			}
			mu.Lock()
			count := len(bodies)
			second := ""
			if count == 2 {
				second = bodies[1]
			}
			mu.Unlock()
			if count != 2 || !strings.Contains(second, "KUMQUAT-7") {
				t.Fatal("resumed upstream request does not carry prior-thread history", count)
			}
			budget, _ := a.config.Scheduler.Store.Budget(r.TaskID)
			if budget.UsedCalls != 2 || calls.Load() != 2 {
				t.Fatal("resumed budget accounting wrong", budget.UsedCalls, calls.Load())
			}
			t.Logf("mode=%s originalHTTP=1 resumedHTTP=1 historyCarried=true nativeLaunches=2 budget=2 sealedRef=opaque reopen=%v", mode, mode == "reopen")
		})
	}
}

func validRefShape(ref CheckpointRef) bool {
	for _, v := range []string{ref.ID, ref.Digest} {
		if len(v) != 64 || strings.Trim(v, "0123456789abcdef") != "" {
			return false
		}
	}
	return true
}

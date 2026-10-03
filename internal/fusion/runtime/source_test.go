//go:build darwin

package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/fusion/workspace"
)

func TestSupervisorSourceGuardRevokesIdleProcess(t *testing.T) {
	source, root := pdir(t), pdir(t)
	p := filepath.Join(source, "input")
	if e := os.WriteFile(p, []byte("original"), 0600); e != nil {
		t.Fatal(e)
	}
	snapshot, e := workspace.Copy(source, root, "copy")
	if e != nil {
		t.Fatal(e)
	}
	guard, e := snapshot.Guard()
	if e != nil {
		t.Fatal(e)
	}
	sup, h, st, run, _, _ := setupWithSpec(t, "delay", false, func(spec *Spec) { spec.Source = guard; spec.Workspace = snapshot.Path; spec.Timeout = 20 * time.Second })
	t.Cleanup(func() {
		h.Cancel()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		h.Wait(ctx)
	})
	if e = os.WriteFile(p, []byte("changed"), 0600); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	result, e := h.Wait(ctx)
	if e != nil || result.State != "cancelled" || !result.StoppedVerified || !sup.VerifyStop(result.Proof) {
		t.Fatal("idle source drift not actually stopped", result.State, e)
	}
	budget, e := st.Budget(run.TaskID)
	if e != nil || budget.UsedCalls != 0 {
		t.Fatal("source revoke used model budget", e)
	}
}

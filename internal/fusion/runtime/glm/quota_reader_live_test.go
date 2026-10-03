package glm

import (
	"context"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/yetone/magpie/internal/fusion/quota"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"golang.org/x/sys/unix"
)

var liveQuotaKey = flag.String("fusion-live-glm-quota-key", "", "explicit private CN key file for a single read-only live quota diagnostic; no model request")

// This explicit opt-in test is a diagnostic collector, not production credential
// registration or quota-purpose admission. It never claims a verified identity.
func TestGLMQuotaReaderLiveCN(t *testing.T) {
	if *liveQuotaKey == "" {
		t.Skip("explicit private-key live quota opt-in required")
	}
	path := *liveQuotaKey
	canonical, e := filepath.EvalSymlinks(path)
	if e != nil || !filepath.IsAbs(path) || filepath.Clean(path) != path || path != canonical {
		t.Fatal("private key location refused")
	}
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		if _, e := os.Lstat(filepath.Join(parent, ".git")); e == nil || !os.IsNotExist(e) {
			t.Fatal("credential inside Git checkout refused")
		}
		if parent == filepath.Dir(parent) {
			break
		}
	}
	for _, p := range []string{filepath.Dir(path), filepath.Dir(filepath.Dir(path)), path} {
		info, e := os.Lstat(p)
		if e != nil {
			t.Fatal("private credential metadata unavailable")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != uint32(os.Getuid()) || info.Mode().Perm()&0077 != 0 || p == path && !info.Mode().IsRegular() || p != path && !info.IsDir() {
			t.Fatal("private credential ownership refused")
		}
	}
	i := quota.Identity{Provider: "bigmodel", Account: "local-account-unverified", Workspace: "local-workspace-unverified", Region: "CN", Generation: 1}
	target := stageplan.ExecutionTarget{Account: i.Account, Workspace: i.Workspace, CredentialIdentity: "private-diagnostic-only", BillingPath: "coding_plan"}
	r, e := NewQuotaReader(QuotaReaderConfig{Identity: i, Target: target, QueryAllowed: func(ctx context.Context, got quota.Identity) bool { return ctx.Err() == nil && got == i }, LoadCredential: func(context.Context, stageplan.ExecutionTarget) (Credential, error) {
		before, e := os.Lstat(path)
		if e != nil {
			return Credential{}, ErrIdentity
		}
		fd, e := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if e != nil {
			return Credential{}, ErrIdentity
		}
		f := os.NewFile(uintptr(fd), "private-glm-key")
		defer f.Close()
		after, e := f.Stat()
		stat, ok := afterSys(after).(*syscall.Stat_t)
		if e != nil || !os.SameFile(before, after) || !ok || stat.Uid != uint32(os.Getuid()) || after.Mode().Perm()&0077 != 0 || !after.Mode().IsRegular() {
			return Credential{}, ErrIdentity
		}
		raw, e := io.ReadAll(io.LimitReader(f, 4098))
		if e != nil || len(raw) > 4097 {
			return Credential{}, ErrIdentity
		}
		key := strings.TrimSpace(string(raw))
		for _, c := range key {
			if c < 33 || c > 126 {
				return Credential{}, ErrIdentity
			}
		}
		return Credential{Identity: target.CredentialIdentity, Key: key}, nil
	}})
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

func afterSys(info os.FileInfo) any {
	if info == nil {
		return nil
	}
	return info.Sys()
}

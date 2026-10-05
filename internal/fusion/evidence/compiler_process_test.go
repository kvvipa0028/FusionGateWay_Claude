//go:build darwin

package evidence

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPinnedCompilerFixtureWorker(t *testing.T) {
	if len(os.Args) < 4 || os.Args[len(os.Args)-3] != "--" || os.Args[len(os.Args)-2] != "compiler" {
		return
	}
	if os.Args[len(os.Args)-1] == "child" {
		raw, _ := json.Marshal([]int64{int64(os.Getpid())})
		if os.WriteFile(filepath.Join(os.Getenv("TMPDIR"), "child.json"), raw, 0600) != nil {
			os.Exit(71)
		}
		for {
			time.Sleep(time.Second)
		}
	}
	child := exec.Command(os.Args[0], "-test.run=^TestPinnedCompilerFixtureWorker$", "--", "compiler", "child")
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	if child.Start() != nil {
		os.Exit(72)
	}
	fmt.Println("real pinned compiler child started")
	for {
		time.Sleep(time.Second)
	}
}

func TestCompilerTreeCancellationAndRevocationReapActualChildren(t *testing.T) {
	for _, mode := range []string{"cancel", "revocation"} {
		t.Run(mode, func(t *testing.T) {
			exe, _ := os.Executable()
			exe, _ = filepath.EvalSymlinks(exe)
			input, scratch := private(t), private(t)
			for _, dir := range []string{"home", "config", "cache", "data", "tmp"} {
				if os.Mkdir(filepath.Join(scratch, dir), 0700) != nil {
					t.Fatal("fixture")
				}
			}
			profile, env := sandbox(exe, input, scratch)
			profile += "(allow process-fork)\n(allow signal (target children))\n"
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var current atomic.Bool
			current.Store(true)
			done := make(chan execution, 1)
			go func() {
				done <- executeOwned(ctx, exe, []string{"-test.run=^TestPinnedCompilerFixtureWorker$", "--", "compiler", "parent"}, input, profile, env, 4096, current.Load, nil, true)
			}()
			var child []int64
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				raw, e := os.ReadFile(filepath.Join(scratch, "tmp/child.json"))
				if e == nil && json.Unmarshal(raw, &child) == nil && len(child) == 1 {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if len(child) != 1 {
				cancel()
				<-done
				t.Fatal("real child not started")
			}
			childIdentity := identity(int(child[0]))
			if childIdentity == 0 {
				cancel()
				<-done
				t.Fatal("child identity not alive")
			}
			if mode == "cancel" {
				cancel()
			} else {
				current.Store(false)
			}
			select {
			case out := <-done:
				if !out.executed || !out.stopped || !out.interrupted || out.exit == 0 {
					t.Fatal("compiler tree stop unproven", out)
				}
			case <-time.After(6 * time.Second):
				t.Fatal("compiler tree failed to stop")
			}
			if identity(int(child[0])) == childIdentity {
				t.Fatal("real compiler child survived")
			}
		})
	}
}

func TestCompilerSandboxOnlyAllowsPinnedSDKTools(t *testing.T) {
	exe, _ := os.Executable()
	exe, _ = filepath.EvalSymlinks(exe)
	in, scratch := private(t), private(t)
	profile, _ := goCompilerSandbox(Spec{Tool: Tool{Executable: exe}, Go: &GoToolchain{Root: testGoRoot}}, in, scratch)
	for _, name := range []string{"compile", "asm", "link", "vet"} {
		literal := "(allow process-exec (literal " + strconv.Quote(filepath.Join(testGoRoot, "pkg/tool/darwin_arm64", name)) + ")"
		if !strings.Contains(profile, literal) {
			t.Fatal("missing pinned child", name)
		}
	}
	for _, forbidden := range []string{"(allow process-exec (subpath", "(allow network", "/bin/sh", "/usr/bin/env"} {
		if strings.Contains(profile, forbidden) {
			t.Fatal("expanded compiler authority", forbidden)
		}
	}
}

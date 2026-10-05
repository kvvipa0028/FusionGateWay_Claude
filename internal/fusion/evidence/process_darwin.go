//go:build darwin

package evidence

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func available() bool {
	i, e := os.Stat("/usr/bin/sandbox-exec")
	return e == nil && i.Mode().IsRegular()
}
func sandbox(exe, input, scratch string) (string, []string) {
	p := "(version 1)\n(deny default)\n(allow signal (target self))\n(allow file-read-metadata)\n(allow file-read* (literal \"/\"))\n(allow sysctl-read (sysctl-name-prefix \"hw.\") (sysctl-name \"kern.osrelease\") (sysctl-name \"kern.ostype\"))\n"
	for _, path := range []string{"/System/Library", "/usr/lib", "/Library/Apple/System/Library"} {
		p += fmt.Sprintf("(allow file-read* file-map-executable (subpath %s))\n", strconv.Quote(path))
	}
	for _, path := range []string{input, scratch} {
		p += fmt.Sprintf("(allow file-read* (subpath %s))\n", strconv.Quote(path))
	}
	p += fmt.Sprintf("(allow process-exec (literal %s))\n(allow file-read* file-map-executable (literal %s))\n(allow file-write* (subpath %s))\n", strconv.Quote(exe), strconv.Quote(exe), strconv.Quote(scratch))
	p += "(allow file-read* file-write* (literal \"/dev/null\"))\n(allow file-read* (literal \"/dev/urandom\"))\n"
	e := []string{"PATH=/usr/bin:/bin", "HOME=" + filepath.Join(scratch, "home"), "XDG_CONFIG_HOME=" + filepath.Join(scratch, "config"), "XDG_CACHE_HOME=" + filepath.Join(scratch, "cache"), "XDG_DATA_HOME=" + filepath.Join(scratch, "data"), "TMPDIR=" + filepath.Join(scratch, "tmp"), "LANG=C", "TZ=UTC", "GORACE=atexit_sleep_ms=0"}
	return p, e
}
func identity(pid int) int64 {
	k, e := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if e != nil || k == nil || int(k.Proc.P_pid) != pid {
		return 0
	}
	return k.Proc.P_starttime.Sec*1000000 + int64(k.Proc.P_starttime.Usec)
}
func execute(ctx context.Context, exe string, args []string, dir, profile string, env []string, limit int, current func() bool) execution {
	return executeOwned(ctx, exe, args, dir, profile, env, limit, current, nil, false)
}

func executeOwned(ctx context.Context, exe string, args []string, dir, profile string, env []string, limit int, current func() bool, stdin []byte, tree bool) execution {
	r := execution{exit: -1}
	if ctx.Err() != nil || !current() {
		r.interrupted = true
		return r
	}
	cmd := exec.Command("/usr/bin/sandbox-exec", append([]string{"-p", profile, exe}, args...)...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	if tree {
		cmd.WaitDelay = time.Second
	}
	cmd.Dir = dir
	cmd.Env = append([]string(nil), env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, errout := &bounded{limit: limit}, &bounded{limit: limit}
	cmd.Stdout = out
	cmd.Stderr = errout
	r.started = time.Now().UTC()
	if cmd.Start() != nil {
		r.finished = time.Now().UTC()
		return r
	}
	r.executed = true
	id := identity(cmd.Process.Pid)
	if id == 0 {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		r.finished = time.Now().UTC()
		return r
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	interrupt := func() {
		r.interrupted = true
		if identity(cmd.Process.Pid) == id {
			if tree {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			} else {
				_ = syscall.Kill(cmd.Process.Pid, syscall.SIGKILL)
			}
		}
	}
	ctxDone := ctx.Done()
	for {
		select {
		case <-done:
			if tree && !compilerGroupEmpty(cmd.Process.Pid) {
				r.interrupted = true
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				deadline := time.Now().Add(3 * time.Second)
				for !compilerGroupEmpty(cmd.Process.Pid) && time.Now().Before(deadline) {
					time.Sleep(10 * time.Millisecond)
				}
			}
			r.finished = time.Now().UTC()
			r.stdout = append([]byte(nil), out.data...)
			r.stderr = append([]byte(nil), errout.data...)
			r.truncated = out.truncated || errout.truncated
			if cmd.ProcessState != nil {
				r.exit = cmd.ProcessState.ExitCode()
				w, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
				r.stopped = ok && (w.Exited() || w.Signaled()) && identity(cmd.Process.Pid) != id && (!tree || compilerGroupEmpty(cmd.Process.Pid))
			}
			return r
		case <-ctxDone:
			interrupt()
			ctxDone = nil
		case <-ticker.C:
			if !current() {
				interrupt()
			}
		}
	}
}

// Only pinned compiler tools can run in this group. Generated project code is
// executed separately with fork denied. Lookup failure never proves stopped.
func compilerGroupEmpty(group int) bool {
	rows, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", group)
	return err == nil && len(rows) == 0
}
func goCompilerSandbox(s Spec, input, scratch string) (string, []string) {
	p, e := sandbox(s.Tool.Executable, input, scratch)
	p += "(allow process-fork)\n(allow signal (target children))\n"
	p += fmt.Sprintf("(allow file-read* file-map-executable (subpath %s))\n", strconv.Quote(s.Go.Root))
	for _, name := range []string{"compile", "asm", "link", "vet"} {
		tool := filepath.Join(s.Go.Root, "pkg/tool/darwin_arm64", name)
		p += fmt.Sprintf("(allow process-exec (literal %s))\n", strconv.Quote(tool))
	}
	e = append(e, "GOROOT="+s.Go.Root, "GOENV=off", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "GOVCS=off", "GOWORK=off", "CGO_ENABLED=0", "GOTELEMETRY=off", "GOCACHE="+filepath.Join(scratch, "cache"), "GOMODCACHE="+filepath.Join(scratch, "modules"))
	return p, e
}

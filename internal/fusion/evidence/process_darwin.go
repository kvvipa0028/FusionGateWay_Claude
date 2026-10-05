//go:build darwin

package evidence

import (
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
	r := execution{exit: -1}
	if ctx.Err() != nil || !current() {
		r.interrupted = true
		return r
	}
	cmd := exec.Command("/usr/bin/sandbox-exec", append([]string{"-p", profile, exe}, args...)...)
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
			_ = syscall.Kill(cmd.Process.Pid, syscall.SIGKILL)
		}
	}
	ctxDone := ctx.Done()
	for {
		select {
		case <-done:
			r.finished = time.Now().UTC()
			r.stdout = append([]byte(nil), out.data...)
			r.stderr = append([]byte(nil), errout.data...)
			r.truncated = out.truncated || errout.truncated
			if cmd.ProcessState != nil {
				r.exit = cmd.ProcessState.ExitCode()
				w, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
				r.stopped = ok && (w.Exited() || w.Signaled()) && identity(cmd.Process.Pid) != id
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

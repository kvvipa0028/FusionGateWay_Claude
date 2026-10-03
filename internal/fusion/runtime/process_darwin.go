//go:build darwin

package runtime

import (
	"golang.org/x/sys/unix"
	"os"
	"syscall"
)

func processIdentity(pid int) (Identity, error) {
	k, e := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if e != nil || k == nil || int(k.Proc.P_pid) != pid || k.Proc.P_starttime.Sec == 0 {
		return Identity{}, ErrIdentity
	}
	return Identity{pid, k.Proc.P_starttime.Sec*1000000 + int64(k.Proc.P_starttime.Usec)}, nil
}
func sameProcess(id Identity) bool {
	actual, e := processIdentity(id.PID)
	return e == nil && actual == id
}
func signalProcess(id Identity, s syscall.Signal) error {
	if !sameProcess(id) {
		return ErrIdentity
	}
	return syscall.Kill(id.PID, s)
}

func reaped(p *os.ProcessState) bool {
	if p == nil {
		return false
	}
	w, ok := p.Sys().(syscall.WaitStatus)
	return ok && (w.Exited() || w.Signaled())
}

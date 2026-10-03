//go:build !windows

package store

import (
	"golang.org/x/sys/unix"
	"os"
	"syscall"
)

func privateOwner(info os.FileInfo) bool {
	s, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(s.Uid) == os.Getuid()
}
func lockController(path string) (*os.File, error) {
	fd, e := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), path)
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !privateOwner(info) {
		f.Close()
		return nil, ErrInvalid
	}
	if e = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); e != nil {
		f.Close()
		return nil, ErrControllerActive
	}
	return f, nil
}

//go:build darwin || linux

package bootstrap

import (
	"crypto/rand"
	"encoding/base64"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
)

func controlRootIdentity(root string) (fileIdentity, error) {
	fd, e := openDirectory(root, true)
	if e != nil {
		return fileIdentity{}, ErrControlHost
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil {
		return fileIdentity{}, ErrControlHost
	}
	return fileIdentity{device: uint64(st.Dev), inode: uint64(st.Ino)}, nil
}
func openControlFiles(root string) (fileIdentity, sourceRecord, error) {
	parent, e := openDirectory(filepath.Dir(root), true)
	if e != nil {
		return fileIdentity{}, sourceRecord{}, ErrControlHost
	}
	defer unix.Close(parent)
	if e = unix.Mkdirat(parent, filepath.Base(root), 0700); e != nil && e != unix.EEXIST {
		return fileIdentity{}, sourceRecord{}, ErrControlHost
	}
	fd, e := openDirectory(root, true)
	if e != nil {
		return fileIdentity{}, sourceRecord{}, ErrControlHost
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil {
		return fileIdentity{}, sourceRecord{}, ErrControlHost
	}
	identity := fileIdentity{device: uint64(st.Dev), inode: uint64(st.Ino)}
	if e = unix.Mkdirat(fd, "tasks", 0700); e != nil && e != unix.EEXIST {
		return fileIdentity{}, sourceRecord{}, ErrControlHost
	}
	tokenFD, e := unix.Openat(fd, "management.token", unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if e == nil {
		f := os.NewFile(uintptr(tokenFD), "private-fusion-management")
		var entropy [32]byte
		_, e = rand.Read(entropy[:])
		if e == nil {
			_, e = f.WriteString("fgm_" + base64.RawURLEncoding.EncodeToString(entropy[:]) + "\n")
		}
		if e == nil {
			e = f.Sync()
		}
		ce := f.Close()
		if e != nil || ce != nil {
			return fileIdentity{}, sourceRecord{}, ErrControlHost
		}
	} else if e != unix.EEXIST {
		return fileIdentity{}, sourceRecord{}, ErrControlHost
	}
	record, e := readSource(filepath.Join(root, "management.token"))
	if e != nil {
		return fileIdentity{}, sourceRecord{}, ErrControlHost
	}
	return identity, record, nil
}

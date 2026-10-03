//go:build darwin || linux

package bootstrap

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// Walk using directory FDs so replacement with a symlink cannot redirect an
// already checked source/folder path. Only registration sources forbid Git.
func openDirectory(path string, private bool) (int, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return -1, ErrRegistration
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC
	fd, e := unix.Open("/", flags, 0)
	if e != nil {
		return -1, ErrRegistration
	}
	fail := func() (int, error) { unix.Close(fd); return -1, ErrRegistration }
	noGit := func() bool {
		var st unix.Stat_t
		return unix.Fstatat(fd, ".git", &st, unix.AT_SYMLINK_NOFOLLOW) == unix.ENOENT
	}
	for n, part := range parts {
		if part == "" || private && !noGit() {
			return fail()
		}
		child, e := unix.Openat(fd, part, flags, 0)
		if e != nil {
			return fail()
		}
		unix.Close(fd)
		fd = child
		if private && n >= len(parts)-2 {
			var st unix.Stat_t
			if unix.Fstat(fd, &st) != nil || st.Uid != uint32(os.Getuid()) || uint32(st.Mode)&0077 != 0 {
				return fail()
			}
		}
	}
	if private && !noGit() {
		return fail()
	}
	return fd, nil
}
func readFolder(path string) (fileIdentity, error) {
	fd, e := openDirectory(path, false)
	if e != nil {
		return fileIdentity{}, ErrRegistration
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Uid != uint32(os.Getuid()) || uint32(st.Mode)&0022 != 0 {
		return fileIdentity{}, ErrRegistration
	}
	return fileIdentity{device: uint64(st.Dev), inode: uint64(st.Ino)}, nil
}
func readSource(path string) (sourceRecord, error) {
	fd, e := openDirectory(filepath.Dir(path), true)
	if e != nil {
		return sourceRecord{}, ErrRegistration
	}
	defer unix.Close(fd)
	fileFD, e := unix.Openat(fd, filepath.Base(path), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if e != nil {
		return sourceRecord{}, ErrRegistration
	}
	f := os.NewFile(uintptr(fileFD), "private-fusion-registration")
	defer f.Close()
	valid := func(st unix.Stat_t) bool {
		return uint32(st.Mode)&unix.S_IFMT == unix.S_IFREG && st.Uid == uint32(os.Getuid()) && uint32(st.Mode)&0077 == 0 && st.Nlink == 1 && st.Size >= 2 && st.Size <= MaxSourceBytes
	}
	var before, after unix.Stat_t
	if unix.Fstat(fileFD, &before) != nil || !valid(before) {
		return sourceRecord{}, ErrRegistration
	}
	info, e := f.Stat()
	if e != nil {
		return sourceRecord{}, ErrRegistration
	}
	raw, e := io.ReadAll(io.LimitReader(f, MaxSourceBytes+1))
	if e != nil || len(raw) > MaxSourceBytes || unix.Fstat(fileFD, &after) != nil || !valid(after) || before.Dev != after.Dev || before.Ino != after.Ino || before.Size != after.Size || after.Size != int64(len(raw)) {
		return sourceRecord{}, ErrRegistration
	}
	last, e := f.Stat()
	if e != nil || !info.ModTime().Equal(last.ModTime()) {
		return sourceRecord{}, ErrRegistration
	}
	return sourceRecord{fileIdentity: fileIdentity{device: uint64(after.Dev), inode: uint64(after.Ino)}, raw: raw}, nil
}

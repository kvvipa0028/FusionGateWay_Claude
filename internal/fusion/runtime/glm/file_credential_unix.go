//go:build darwin || linux

package glm

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// Walk from / with directory FDs, refusing every symlink and Git ancestor.
// Relative openat calls avoid exchanging a checked directory for a symlink
// between checking its permissions and opening the private file below it.
func readCredentialFile(path string) (credentialRecord, error) {
	parts := strings.Split(strings.TrimPrefix(filepath.Dir(path), "/"), "/")
	if len(parts) < 2 {
		return credentialRecord{}, ErrIdentity
	}
	flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC
	fd, e := unix.Open("/", flags, 0)
	if e != nil {
		return credentialRecord{}, ErrIdentity
	}
	defer func() { unix.Close(fd) }()
	noGit := func(fd int) bool {
		var st unix.Stat_t
		return unix.Fstatat(fd, ".git", &st, unix.AT_SYMLINK_NOFOLLOW) == unix.ENOENT
	}
	for n, part := range parts {
		if part == "" || !noGit(fd) {
			return credentialRecord{}, ErrIdentity
		}
		child, e := unix.Openat(fd, part, flags, 0)
		if e != nil {
			return credentialRecord{}, ErrIdentity
		}
		unix.Close(fd)
		fd = child
		if n >= len(parts)-2 {
			var st unix.Stat_t
			if unix.Fstat(fd, &st) != nil || st.Uid != uint32(os.Getuid()) || uint32(st.Mode)&0077 != 0 {
				return credentialRecord{}, ErrIdentity
			}
		}
	}
	if !noGit(fd) {
		return credentialRecord{}, ErrIdentity
	}
	keyFD, e := unix.Openat(fd, filepath.Base(path), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if e != nil {
		return credentialRecord{}, ErrIdentity
	}
	f := os.NewFile(uintptr(keyFD), "private-glm-credential")
	defer f.Close()
	valid := func(st unix.Stat_t) bool {
		return uint32(st.Mode)&unix.S_IFMT == unix.S_IFREG && st.Uid == uint32(os.Getuid()) && uint32(st.Mode)&0077 == 0 && st.Nlink == 1 && st.Size >= 8 && st.Size <= 4097
	}
	var before, after unix.Stat_t
	if unix.Fstat(keyFD, &before) != nil || !valid(before) {
		return credentialRecord{}, ErrIdentity
	}
	raw, e := io.ReadAll(io.LimitReader(f, 4098))
	if e != nil || len(raw) > 4097 || unix.Fstat(keyFD, &after) != nil || !valid(after) || before.Dev != after.Dev || before.Ino != after.Ino || after.Size != int64(len(raw)) {
		return credentialRecord{}, ErrIdentity
	}
	key := strings.TrimSuffix(string(raw), "\n")
	if len(key) < 8 || len(key) > 4096 || strings.HasPrefix(key, "REPLACE_") {
		return credentialRecord{}, ErrIdentity
	}
	for _, c := range key {
		if c < 33 || c > 126 {
			return credentialRecord{}, ErrIdentity
		}
	}
	return credentialRecord{key: key, device: uint64(after.Dev), inode: uint64(after.Ino)}, nil
}

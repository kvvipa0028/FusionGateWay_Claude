//go:build darwin || linux

package grok

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// Descriptor-relative traversal refuses symlinks and every Git ancestor. The
// three private directories are grok/home/.grok, prepared by official login.
func readPrivateAuthFile(path string) (privateAuthFile, error) {
	parts := strings.Split(strings.TrimPrefix(filepath.Dir(path), "/"), "/")
	if len(parts) < 3 {
		return privateAuthFile{}, ErrIdentity
	}
	flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC
	fd, e := unix.Open("/", flags, 0)
	if e != nil {
		return privateAuthFile{}, ErrIdentity
	}
	defer func() { unix.Close(fd) }()
	noGit := func(dir int) bool {
		var st unix.Stat_t
		return unix.Fstatat(dir, ".git", &st, unix.AT_SYMLINK_NOFOLLOW) == unix.ENOENT
	}
	var record privateAuthFile
	for n, part := range parts {
		if part == "" || !noGit(fd) {
			return privateAuthFile{}, ErrIdentity
		}
		next, e := unix.Openat(fd, part, flags, 0)
		if e != nil {
			return privateAuthFile{}, ErrIdentity
		}
		unix.Close(fd)
		fd = next
		if n >= len(parts)-3 {
			var st unix.Stat_t
			if unix.Fstat(fd, &st) != nil || st.Uid != uint32(os.Getuid()) || uint32(st.Mode)&0077 != 0 {
				return privateAuthFile{}, ErrIdentity
			}
			record.dirs[n-(len(parts)-3)] = privateFileID{uint64(st.Dev), uint64(st.Ino)}
		}
	}
	if !noGit(fd) {
		return privateAuthFile{}, ErrIdentity
	}
	fileFD, e := unix.Openat(fd, filepath.Base(path), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if e != nil {
		return privateAuthFile{}, ErrIdentity
	}
	f := os.NewFile(uintptr(fileFD), "private-grok-credential")
	defer f.Close()
	valid := func(st unix.Stat_t) bool {
		// Official storage writes 0600 with a lock sidecar; refuse executable
		// or special bits, group/other access and hard-link aliases.
		return uint32(st.Mode)&unix.S_IFMT == unix.S_IFREG && st.Uid == uint32(os.Getuid()) && uint32(st.Mode)&07777 == 0600 && st.Nlink == 1 && st.Size > 0 && st.Size <= 256<<10
	}
	var before, after, current unix.Stat_t
	if unix.Fstat(fileFD, &before) != nil || !valid(before) {
		return privateAuthFile{}, ErrIdentity
	}
	info, e := f.Stat()
	if e != nil {
		return privateAuthFile{}, ErrIdentity
	}
	raw, e := io.ReadAll(io.LimitReader(f, (256<<10)+1))
	if e != nil || len(raw) > 256<<10 || unix.Fstat(fileFD, &after) != nil || !valid(after) || before.Dev != after.Dev || before.Ino != after.Ino || before.Size != after.Size || after.Size != int64(len(raw)) || unix.Fstatat(fd, filepath.Base(path), &current, unix.AT_SYMLINK_NOFOLLOW) != nil || !valid(current) || current.Dev != after.Dev || current.Ino != after.Ino {
		return privateAuthFile{}, ErrIdentity
	}
	last, e := f.Stat()
	if e != nil || !info.ModTime().Equal(last.ModTime()) {
		return privateAuthFile{}, ErrIdentity
	}
	record.raw = raw
	record.file = privateFileID{uint64(after.Dev), uint64(after.Ino)}
	return record, nil
}

//go:build unix

package grok

import (
	"os"
	"syscall"
)

func archiveIdentityOf(info os.FileInfo) (archiveIdentity, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return archiveIdentity{}, false
	}
	return archiveIdentity{Device: uint64(st.Dev), Inode: uint64(st.Ino), UID: st.Uid}, true
}

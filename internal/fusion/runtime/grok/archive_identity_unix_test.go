//go:build unix

package grok

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

type foreignArchiveInfo struct {
	os.FileInfo
	system syscall.Stat_t
}

func (f foreignArchiveInfo) Sys() any { return &f.system }
func TestArchiveIdentityPreservesApprovedFileOwner(t *testing.T) {
	path := filepath.Join(privateAdapterDir(t), "source.txt")
	if os.WriteFile(path, []byte("readable"), 0600) != nil {
		t.Fatal("fixture failed")
	}
	info, e := os.Stat(path)
	if e != nil {
		t.Fatal(e)
	}
	system := *info.Sys().(*syscall.Stat_t)
	system.Uid++
	identity, ok := archiveIdentityOf(foreignArchiveInfo{info, system})
	if !ok || identity.UID != system.Uid || identity.Inode != uint64(system.Ino) {
		t.Fatal("approved Source owner lost; checkpoint must not redefine read permissions")
	}
}

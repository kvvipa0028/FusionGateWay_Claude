//go:build unix

package grok

import (
	"os"
	"syscall"
)

const readPlatformSupported = true

func singleReadLink(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Nlink == 1
}

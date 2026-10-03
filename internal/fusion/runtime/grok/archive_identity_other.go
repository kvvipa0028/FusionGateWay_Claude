//go:build !unix

package grok

import "os"

func archiveIdentityOf(os.FileInfo) (archiveIdentity, bool) { return archiveIdentity{}, false }

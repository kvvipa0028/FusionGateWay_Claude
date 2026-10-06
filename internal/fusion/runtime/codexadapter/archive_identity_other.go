//go:build !unix

package codexadapter

import "os"

func archiveIdentityOf(os.FileInfo) (archiveIdentity, bool) { return archiveIdentity{}, false }
func singleArchiveLink(os.FileInfo) bool                    { return false }

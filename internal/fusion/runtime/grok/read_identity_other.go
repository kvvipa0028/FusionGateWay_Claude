//go:build !unix

package grok

import "os"

const readPlatformSupported = false

func singleReadLink(os.FileInfo) bool { return false }

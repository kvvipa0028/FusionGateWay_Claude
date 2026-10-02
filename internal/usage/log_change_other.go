//go:build !darwin && !linux

package usage

import "os"

// FileInfo exposes no portable change time here. Validate the fingerprint
// instead, so a same-size rewrite with restored mtime cannot return stale rows.
func logChangeStamp(os.FileInfo) string { return "" }

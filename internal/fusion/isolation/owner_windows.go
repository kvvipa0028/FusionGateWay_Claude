//go:build windows

package isolation

import "os"

// Windows Fusion isolation is not admitted by the current macOS pilot.
func ownedByCurrentUser(info os.FileInfo) bool { return false }

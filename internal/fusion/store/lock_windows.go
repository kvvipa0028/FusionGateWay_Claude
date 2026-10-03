//go:build windows

package store

import "os"

func privateOwner(os.FileInfo) bool           { return false }
func lockController(string) (*os.File, error) { return nil, ErrUnsupported }

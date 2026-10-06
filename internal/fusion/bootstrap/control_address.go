package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
)

// The control address file is a same-directory discovery hint for the local
// task CLI. The management token remains the authority; this file only names
// the loopback listener and lives inside the already-private control root.
const controlAddressFile = "control.address"

func writeControlAddress(root, addr string) error {
	if root == "" || addr == "" || strings.ContainsAny(addr, "\x00\n") {
		return ErrControlHost
	}
	path := filepath.Join(root, controlAddressFile)
	if e := os.WriteFile(path, []byte(addr+"\n"), 0600); e != nil {
		return ErrControlHost
	}
	return nil
}

func removeControlAddress(root string) {
	if root == "" {
		return
	}
	_ = os.Remove(filepath.Join(root, controlAddressFile))
}

// ControlAddress reads the published loopback address of a serving host.
func ControlAddress(root string) (string, error) {
	raw, e := os.ReadFile(filepath.Join(root, controlAddressFile))
	if e != nil {
		return "", ErrControlHost
	}
	addr := strings.TrimSpace(string(raw))
	if addr == "" || strings.ContainsAny(addr, "\x00\n ") {
		return "", ErrControlHost
	}
	return addr, nil
}

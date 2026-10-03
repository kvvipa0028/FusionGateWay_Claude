//go:build !darwin

package runtime

import (
	"os"
	"os/exec"
	"syscall"
)

func platformAvailable() bool                      { return false }
func platformCommand(string, Spec) *exec.Cmd       { return nil }
func sandbox(Spec) (string, []string, error)       { return "", nil, ErrUnsupported }
func processIdentity(int) (Identity, error)        { return Identity{}, ErrUnsupported }
func sameProcess(Identity) bool                    { return false }
func signalProcess(Identity, syscall.Signal) error { return ErrUnsupported }

func reaped(*os.ProcessState) bool { return false }

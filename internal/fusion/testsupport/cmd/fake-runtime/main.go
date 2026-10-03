package main

import (
	"github.com/yetone/magpie/internal/fusion/testsupport"
	"os"
	"os/exec"
)

func main() {
	if os.Getenv("FUSION_FIXTURE_RUNTIME") != "1" {
		os.Exit(64)
	}
	if len(os.Args) == 3 && os.Args[1] == "--fixture-grandchild" {
		os.Exit(testsupport.GrandchildHeartbeat(os.Args[2]))
	}
	if len(os.Args) != 1 {
		os.Exit(64)
	}
	os.Exit(testsupport.ServeRuntime(os.Stdin, os.Stdout, func() (int, error) {
		exe, e := os.Executable()
		if e != nil {
			return 0, e
		}
		cmd := exec.Command(exe, "--fixture-grandchild", os.Getenv("FUSION_FIXTURE_HEARTBEAT"))
		cmd.Env = os.Environ()
		if e = cmd.Start(); e != nil {
			return 0, e
		}
		return cmd.Process.Pid, nil
	}))
}

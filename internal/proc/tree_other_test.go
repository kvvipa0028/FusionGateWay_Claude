//go:build !windows

package proc

import (
	"bufio"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// startSh runs script in a tree and gives the pid its first line names.
func startSh(t *testing.T, script string) (*Tree, int) {
	t.Helper()
	cmd := Command("/bin/sh", "-c", script)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	tree, err := StartTree(cmd)
	if err != nil {
		t.Fatal(err)
	}
	line, _ := bufio.NewReader(out).ReadString('\n')
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("no pid from the script: %q", line)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	return tree, pid
}

func gone(pid int) bool {
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if syscall.Kill(pid, 0) != nil {
			return true
		}
	}
	return false
}

// Killing a command ends what it started, not only the command.
func TestTreeKillEndsChildren(t *testing.T) {
	tree, child := startSh(t, "sleep 600 >/dev/null 2>&1 & echo $!; wait")
	done := make(chan struct{})
	go func() { _ = tree.Wait(); close(done) }()
	tree.Kill()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the command outlived Kill")
	}
	if !gone(child) {
		t.Fatalf("its child %d outlived Kill", child)
	}
	tree.Kill() // once waited for, a no-op
}

// A command that ends by itself takes what it left running with it.
func TestTreeWaitEndsWhatWasLeft(t *testing.T) {
	tree, child := startSh(t, "sleep 600 >/dev/null 2>&1 & echo $!")
	if err := tree.Wait(); err != nil {
		t.Fatal(err)
	}
	if !gone(child) {
		t.Fatalf("its child %d outlived it", child)
	}
}

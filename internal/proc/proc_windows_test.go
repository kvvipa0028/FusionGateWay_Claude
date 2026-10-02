package proc

import (
	"context"
	"testing"
)

func TestHideWithoutConsole(t *testing.T) {
	defer func(f func() bool) { hasConsole = f }(hasConsole)

	hasConsole = func() bool { return false }
	if cmd := Command("cmd", "/c", "ver"); cmd.SysProcAttr == nil || cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatalf("no console: CREATE_NO_WINDOW not set: %+v", cmd.SysProcAttr)
	}
	cmd := CommandContext(context.Background(), "cmd", "/c", "ver")
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatalf("no console: CREATE_NO_WINDOW not set: %+v", cmd.SysProcAttr)
	}
	if out, err := cmd.Output(); err != nil || len(out) == 0 {
		t.Fatalf("a hidden child's output should still be piped back: %q, %v", out, err)
	}

	hasConsole = func() bool { return true }
	if cmd := Command("cmd", "/c", "ver"); cmd.SysProcAttr != nil {
		t.Fatalf("with a console the child should share it: %+v", cmd.SysProcAttr)
	}
}

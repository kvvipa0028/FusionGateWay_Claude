package workspace

import (
	"context"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Only an explicit project-root repository supplies a Git base. rev-parse is
// read-only; no user environment, global config, fsmonitor or hooks are used.
// An empty excluded .git directory is not an initialized repository.
func gitBase(path string) (*string, error) {
	i, err := os.Lstat(filepath.Join(path, ".git"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil || !i.IsDir() && !i.Mode().IsRegular() {
		return nil, ErrUnsafe
	}
	if i.IsDir() {
		_, err := os.Lstat(filepath.Join(path, ".git", "HEAD"))
		if os.IsNotExist(err) {
			return nil, nil
		}
		if err != nil {
			return nil, ErrUnsafe
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/git", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-C", path, "rev-parse", "--verify", "HEAD^{commit}")
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=/var/empty", "XDG_CONFIG_HOME=/var/empty", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C"}
	w := &gitOutput{}
	cmd.Stdout, cmd.Stderr = w, w
	if cmd.Run() != nil || ctx.Err() != nil {
		return nil, ErrUnsafe
	}
	hash := strings.TrimSpace(string(w.b))
	if len(hash) != 40 && len(hash) != 64 {
		return nil, ErrUnsafe
	}
	if _, err := hex.DecodeString(hash); err != nil || strings.ToLower(hash) != hash {
		return nil, ErrUnsafe
	}
	return &hash, nil
}

type gitOutput struct{ b []byte }

func (w *gitOutput) Write(b []byte) (int, error) {
	if len(w.b)+len(b) > 2048 {
		return 0, ErrUnsafe
	}
	w.b = append(w.b, b...)
	return len(b), nil
}

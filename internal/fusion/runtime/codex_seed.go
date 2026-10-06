package runtime

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/store"
	"github.com/yetone/magpie/internal/fusion/workspace"
)

var codexThreadUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var codexRolloutPath = regexp.MustCompile(`^sessions/[0-9]{4}/[0-9]{2}/[0-9]{2}/rollout-[0-9A-Za-z._-]{1,128}\.jsonl$`)

// codexSessionSeed is immutable launch data supplied by the trusted checkpoint
// consumer. This boundary constrains bytes/layout; it does not authenticate a
// checkpoint or create upstream authority. There is no Spec seed field.
type codexSessionSeed struct {
	mu            sync.Mutex
	used          bool
	threadID, cwd string
	files         map[string][]byte
}

// NewCodexResumeChannel extends the scoped HTTP channel with a one-shot rollout
// seed: the pinned native finds the prior thread's persisted rollout inside its
// fresh private CODEX_HOME, so thread/resume can adopt the exact threadID. The
// caller's checkpoint binding decides whether this run may resume at all.
func NewCodexResumeChannel(r store.StageRun, root, cwd, session string, driver func(context.Context, io.ReadWriteCloser) error, current func() bool, handler http.Handler, grant *policy.PendingModelGrant, threadID string, files map[string][]byte) (*CodexChannel, error) {
	if !codexThreadUUID.MatchString(threadID) || workspace.PrivateState(cwd) != nil || len(files) == 0 || len(files) > 4 {
		return nil, ErrLaunch
	}
	seed := &codexSessionSeed{threadID: threadID, cwd: cwd, files: map[string][]byte{}}
	total := 0
	for name, data := range files {
		if !codexRolloutPath.MatchString(name) || !strings.Contains(filepath.Base(name), threadID) || len(data) == 0 || len(data) > 4<<20 || !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
			return nil, ErrLaunch
		}
		total += len(data)
		if total > 8<<20 {
			return nil, ErrLaunch
		}
		seed.files[name] = append([]byte(nil), data...)
	}
	ch, e := NewCodexHTTPChannel(r, root, cwd, session, driver, current, handler, grant)
	if e != nil {
		return nil, e
	}
	ch.seed = seed
	return ch, nil
}

func (s *codexSessionSeed) install(home string, spec Spec) error {
	if s == nil || spec.Workspace != s.cwd || !filepath.IsAbs(home) || filepath.Clean(home) != home || home == s.cwd || strings.HasPrefix(home, s.cwd+"/") || strings.HasPrefix(s.cwd, home+"/") {
		return ErrLaunch
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.used {
		return ErrLaunch
	}
	s.used = true
	defer func() {
		for _, data := range s.files {
			clear(data)
		}
		s.files = nil
	}()
	root, e := os.OpenRoot(home)
	if e != nil {
		return ErrLaunch
	}
	defer root.Close()
	// The rollout tree is new in this freshly created CODEX_HOME. Never merge
	// with an existing session tree or a partially initialized prior launch.
	dirs := map[string]bool{}
	for name := range s.files {
		dirs[filepath.Dir(name)] = true
	}
	for dir := range dirs {
		parts := strings.Split(dir, "/")
		for i := range parts {
			if e := root.Mkdir(strings.Join(parts[:i+1], "/"), 0700); e != nil {
				return ErrLaunch
			}
		}
	}
	published := false
	defer func() {
		if !published {
			root.RemoveAll("sessions")
		}
	}()
	for name, data := range s.files {
		f, e := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return ErrLaunch
		}
		_, write := f.Write(data)
		synced, closed := f.Sync(), f.Close()
		if write != nil || synced != nil || closed != nil {
			return ErrLaunch
		}
	}
	if seedSync(root) != nil {
		return ErrLaunch
	}
	published = true
	return nil
}

package runtime

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
)

const CodexCLIVersion = "0.160.0"
const CodexExecutableSHA256 = "112fae7a5a1223e673c8a1791d32338f37df8b527ff1159bb8adac6c4dbf1b4b"

// CodexChannel is trusted, single-use native bootstrap wiring. It grants only
// private stdio plus read access to the Codex managed-preferences domain. This
// initial channel has no network, credentials, model grant or generation
// admission. Driver must respect context cancellation and validate RPC replies;
// success still requires the supervisor's actual Wait and outcome validator.
type CodexChannel struct {
	mu                 sync.Mutex
	run                store.StageRun
	root, cwd, session string
	driver             func(context.Context, io.ReadWriteCloser) error
	current            func() bool
	active, closed     bool
}

func (*CodexChannel) String() string               { return "private Codex bootstrap channel (redacted)" }
func (*CodexChannel) GoString() string             { return "CodexChannel(<redacted>)" }
func (*CodexChannel) MarshalJSON() ([]byte, error) { return nil, ErrLaunch }

func NewCodexChannel(r store.StageRun, root, cwd, session string, driver func(context.Context, io.ReadWriteCloser) error, current func() bool) (*CodexChannel, error) {
	if driver == nil || current == nil || !current() || r.ID == "" || r.TaskID == "" || r.Owner == "" || r.Generation < 1 || r.Attempt < 1 || r.PlanRevision < 1 || !slices.Contains(stageplan.AllRoles(), r.Role) || session == "" || strings.ContainsRune(session, 0) || r.Target.RuntimeVersion != CodexCLIVersion || r.Target.RequestedModel == "" || r.Target.RequestedModel != r.Target.ResolvedModel || r.Target.Account == "" || r.Target.Workspace == "" || r.Target.CredentialIdentity == "" || !privateCodexPaths(root, cwd) {
		return nil, ErrLaunch
	}
	raw, e := json.Marshal(r)
	var frozen store.StageRun
	if e != nil || len(raw) > 64<<10 || json.Unmarshal(raw, &frozen) != nil {
		return nil, ErrLaunch
	}
	return &CodexChannel{run: frozen, root: root, cwd: cwd, session: session, driver: driver, current: current}, nil
}

func privateCodexPaths(root, cwd string) bool {
	return root != "/" && cwd != "/" && filepath.IsAbs(root) && filepath.Clean(root) == root && filepath.IsAbs(cwd) && filepath.Clean(cwd) == cwd && !strings.ContainsRune(root+cwd, 0) && root != cwd && !strings.HasPrefix(root, cwd+"/") && !strings.HasPrefix(cwd, root+"/")
}

func (c *CodexChannel) launchValid(s Spec) bool {
	return c != nil && c.driver != nil && c.current != nil && s.CodexChannel == c && s.Root == c.root && s.Workspace == c.cwd && s.NativeSessionID == c.session && s.ExecutableHash == CodexExecutableSHA256 && slices.Equal(s.Args, []string{"app-server", "--listen", "stdio://", "--strict-config"}) && !s.Writable && len(s.Input) == 0 && len(s.FixtureEnvironment) == 0 && s.ClaudeChannel == nil && s.GrokChannel == nil
}
func (c *CodexChannel) valid(r store.StageRun, s Spec) bool {
	if !c.launchValid(s) || !c.current() {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.validLocked(r)
}
func (c *CodexChannel) validLocked(r store.StageRun) bool {
	a, e := json.Marshal(c.run.Target)
	b, f := json.Marshal(r.Target)
	return !c.active && !c.closed && c.run.ID == r.ID && c.run.TaskID == r.TaskID && c.run.Owner == r.Owner && c.run.Role == r.Role && c.run.Attempt == r.Attempt && c.run.PlanRevision == r.PlanRevision && c.run.Generation == r.Generation && e == nil && f == nil && string(a) == string(b)
}
func (c *CodexChannel) acquire(r store.StageRun, s Spec) bool {
	if !c.launchValid(s) || !c.current() {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.validLocked(r) {
		return false
	}
	c.active = true
	return true
}
func (c *CodexChannel) release() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.active, c.closed = false, true
}

// OS pipes avoid a cmd.Wait copy-goroutine deadlock if the RPC driver stops
// reading. Child endpoints are closed in the parent immediately after Start.
// Only the driver sees this stream; no HTTP/controller DTO can borrow it.
type codexStream struct {
	read, write, childRead, childWrite *os.File
	current                            func() bool
	output                             *boundedOutput
	writeMu                            sync.Mutex
	written                            int
	failed                             atomic.Bool
	once                               sync.Once
}

func newCodexStream(current func() bool, output *boundedOutput) (*codexStream, error) {
	r, w, e := os.Pipe()
	if e != nil {
		return nil, ErrLaunch
	}
	in, input, e := os.Pipe()
	if e != nil {
		r.Close()
		w.Close()
		return nil, ErrLaunch
	}
	return &codexStream{read: r, write: input, childRead: in, childWrite: w, current: current, output: output}, nil
}
func (s *codexStream) Read(p []byte) (int, error) {
	if !s.current() {
		s.failed.Store(true)
		s.Close()
		return 0, ErrLaunch
	}
	n, e := s.read.Read(p)
	if !s.current() {
		s.failed.Store(true)
		s.Close()
		return 0, ErrLaunch
	}
	if n > 0 {
		if _, err := s.output.Write(p[:n]); err != nil {
			s.failed.Store(true)
			s.Close()
			return 0, err
		}
	}
	return n, e
}
func (s *codexStream) Write(p []byte) (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if !s.current() || s.written+len(p) > 64<<10 {
		s.failed.Store(true)
		s.Close()
		return 0, ErrLaunch
	}
	n, e := s.write.Write(p)
	s.written += n
	if e != nil || !s.current() {
		s.failed.Store(true)
		s.Close()
		return 0, ErrLaunch
	}
	return n, e
}
func (s *codexStream) closeChildEnds() { s.childRead.Close(); s.childWrite.Close() }
func (s *codexStream) Close() error {
	s.once.Do(func() { s.read.Close(); s.write.Close(); s.closeChildEnds() })
	return nil
}

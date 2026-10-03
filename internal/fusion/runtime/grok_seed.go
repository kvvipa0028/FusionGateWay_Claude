package runtime

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/workspace"
)

var grokSessionUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var grokSessionFiles = []string{"chat_history.jsonl", "chat_history.jsonl.lock", "events.jsonl", "prompt_context.json", "rewind_points.jsonl", "rewind_points.jsonl.lock", "signals.json", "summary.json", "summary.json.lock", "system_prompt.txt", "tool_definitions.json", "updates.jsonl", "updates.jsonl.lock", "usage.json"}

// grokSessionSeed is immutable launch data supplied by the trusted archive
// consumer. This boundary constrains bytes/layout; it does not authenticate a
// checkpoint or create historical tool authority. There is no Spec seed field.
type grokSessionSeed struct {
	mu      sync.Mutex
	used    bool
	id, cwd string
	files   map[string][]byte
}

func grokSeedCwd(cwd string) string { return strings.ReplaceAll(url.QueryEscape(cwd), "+", "%20") }

func NewGrokResumeChannel(handler http.Handler, grant *policy.PendingModelGrant, target stageplan.ExecutionTarget, id, cwd string, files map[string][]byte) (*GrokChannel, error) {
	if !grokSessionUUID.MatchString(id) || workspace.PrivateState(cwd) != nil || len(files) != len(grokSessionFiles) {
		return nil, ErrLaunch
	}
	seed := &grokSessionSeed{id: id, cwd: cwd, files: map[string][]byte{}}
	total := 0
	for _, name := range grokSessionFiles {
		data, ok := files[name]
		if !ok || len(data) > 2<<20 || !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 || strings.HasSuffix(name, ".lock") && len(data) != 0 {
			return nil, ErrLaunch
		}
		total += len(data)
		if total > 16<<20 {
			return nil, ErrLaunch
		}
		seed.files[name] = append([]byte(nil), data...)
	}
	var summary struct {
		Info  struct{ ID, Cwd string }
		Model string `json:"current_model_id"`
	}
	if json.Unmarshal(seed.files["summary.json"], &summary) != nil || summary.Info.ID != id || summary.Info.Cwd != cwd || summary.Model != target.RequestedModel {
		return nil, ErrLaunch
	}
	ch, e := NewGrokChannel(handler, grant, target)
	if e != nil {
		return nil, e
	}
	ch.seed = seed
	return ch, nil
}

func (s *grokSessionSeed) install(home string, spec Spec) error {
	if s == nil || spec.NativeSessionID != s.id || spec.Workspace != s.cwd || workspace.PrivateState(home) != nil || home == s.cwd || strings.HasPrefix(home, s.cwd+"/") || strings.HasPrefix(s.cwd, home+"/") {
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
	// sessions is new in this freshly created GROK_HOME. Never merge with an
	// existing session tree, daily config or partially initialized prior launch.
	if root.Mkdir("sessions", 0700) != nil {
		return ErrLaunch
	}
	published := false
	defer func() {
		if !published {
			root.RemoveAll("sessions")
		}
	}()
	parent := filepath.Join("sessions", grokSeedCwd(s.cwd))
	if root.Mkdir(parent, 0700) != nil {
		return ErrLaunch
	}
	temp := filepath.Join(parent, ".restoring-"+s.id)
	if root.Mkdir(temp, 0700) != nil {
		return ErrLaunch
	}
	dest, e := root.OpenRoot(temp)
	if e != nil {
		return ErrLaunch
	}
	defer dest.Close()
	for _, name := range grokSessionFiles {
		f, e := dest.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return ErrLaunch
		}
		_, write := f.Write(s.files[name])
		synced, closed := f.Sync(), f.Close()
		if write != nil || synced != nil || closed != nil {
			return ErrLaunch
		}
	}
	if seedSync(dest) != nil || root.Rename(temp, filepath.Join(parent, s.id)) != nil || seedSync(root) != nil {
		return ErrLaunch
	}
	published = true
	return nil
}

func seedSync(root *os.Root) error {
	f, e := root.Open(".")
	if e != nil {
		return ErrLaunch
	}
	defer f.Close()
	if f.Sync() != nil {
		return ErrLaunch
	}
	return nil
}

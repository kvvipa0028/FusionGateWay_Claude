package testsupport

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"
)

type RuntimeRequest struct {
	Model     string `json:"model"`
	Account   string `json:"account"`
	Mode      string `json:"mode"`
	Revision  int    `json:"revision"`
	Workspace string `json:"workspace"`
}
type RuntimeEvent struct {
	Seq      int    `json:"seq"`
	Kind     string `json:"kind"`
	Model    string `json:"model,omitempty"`
	Account  string `json:"account,omitempty"`
	Revision int    `json:"revision,omitempty"`
	Value    string `json:"value,omitempty"`
	PID      int    `json:"pid,omitempty"`
}

// ServeRuntime consumes one bounded JSON/RPC request, emitting ordered JSON lines.
// It never resolves real models, reads auth files, or invokes model services.
func ServeRuntime(input io.Reader, output io.Writer, spawn func() (int, error)) int {
	var in RuntimeRequest
	d := json.NewDecoder(io.LimitReader(input, 8193))
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil || !fixtureID(in.Model) || !fixtureID(in.Account) || in.Revision < 1 {
		return 64
	}
	var trailing any
	if d.Decode(&trailing) != io.EOF {
		return 64
	}
	seq := 0
	emit := func(kind, value string, pid int) bool {
		seq++
		return json.NewEncoder(output).Encode(RuntimeEvent{Seq: seq, Kind: kind, Model: in.Model, Account: in.Account, Revision: in.Revision, Value: value, PID: pid}) == nil
	}
	switch in.Mode {
	case "ok", "rpc", "tool", "write_loss", "crash", "config_changed", "quota_unknown", "rate_limit", "grandchild", "cancel_race", "delay":
	default:
		return 64
	}
	if !emit("started", "fixture-only", 0) {
		return 74
	}
	switch in.Mode {
	case "crash":
		return 70
	case "write_loss":
		if !SafeFixtureWorkspace(in.Workspace) {
			return 64
		}
		if os.WriteFile(filepath.Join(in.Workspace, "write-marker.txt"), []byte("fixture-written\n"), 0600) != nil {
			return 74
		}
		return 70
	case "tool":
		if !emit("tool_call", "fixture-tool", 0) {
			return 74
		}
	case "config_changed":
		if !emit("blocked", "fixture-revision-changed", 0) {
			return 74
		}
		return 0
	case "quota_unknown":
		if !emit("blocked", "quota-unknown", 0) {
			return 74
		}
		return 0
	case "rate_limit":
		if !emit("blocked", "429", 0) {
			return 74
		}
		return 0
	case "grandchild":
		if spawn == nil {
			return 64
		}
		pid, e := spawn()
		if e != nil {
			return 70
		}
		if !emit("child_started", "", pid) {
			return 74
		}
		for {
			time.Sleep(time.Hour)
		}
	case "cancel_race":
		if !SafeFixtureWorkspace(in.Workspace) {
			return 64
		}
		if !emit("waiting", "release-marker", 0) {
			return 74
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			if info, e := os.Lstat(filepath.Join(in.Workspace, "release-marker")); e == nil && info.Mode().IsRegular() {
				break
			}
			if time.Now().After(deadline) {
				return 75
			}
			time.Sleep(5 * time.Millisecond)
		}
	case "delay":
		time.Sleep(time.Second)
	}
	if !emit("chunk", "fixture-part-1", 0) || !emit("chunk", "fixture-part-2", 0) || !emit("completed", "fixture-result", 0) {
		return 74
	}
	return 0
}
func PrepareFixtureWorkspace(root string) error {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return os.ErrInvalid
	}
	if e := os.MkdirAll(root, 0700); e != nil {
		return e
	}
	info, e := os.Lstat(root)
	if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return os.ErrInvalid
	}
	if e = os.Chmod(root, 0700); e != nil {
		return e
	}
	marker, e := os.OpenFile(filepath.Join(root, ".fixture-workspace"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	defer marker.Close()
	_, e = marker.WriteString("fusion-synthetic-workspace\n")
	return e
}
func SafeFixtureWorkspace(root string) bool {
	if !filepath.IsAbs(root) || root == string(filepath.Separator) || filepath.Clean(root) != root {
		return false
	}
	info, e := os.Lstat(root)
	if e != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return false
	}
	// Reject parent symlinks, allowing only the OS's canonical temporary path.
	canonical, e := filepath.EvalSymlinks(root)
	if e != nil || canonical != root {
		return false
	}
	marker := filepath.Join(root, ".fixture-workspace")
	m, e := os.Lstat(marker)
	if e != nil || !m.Mode().IsRegular() || m.Mode().Perm()&0077 != 0 {
		return false
	}
	b, e := os.ReadFile(marker)
	return e == nil && string(b) == "fusion-synthetic-workspace\n"
}
func RuntimeEnvironment(root string) ([]string, error) {
	root, e := filepath.Abs(root)
	if e != nil {
		return nil, e
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, os.ErrInvalid
	}
	if e = os.Chmod(root, 0700); e != nil {
		return nil, e
	}
	root, e = filepath.EvalSymlinks(root)
	if e != nil {
		return nil, e
	}
	env := []string{"PATH=/usr/bin:/bin", "FUSION_FIXTURE_RUNTIME=1"}
	for _, item := range []struct{ key, dir string }{{"HOME", "home"}, {"XDG_CONFIG_HOME", "config"}, {"XDG_CACHE_HOME", "cache"}, {"XDG_DATA_HOME", "data"}, {"TMPDIR", "tmp"}} {
		path := filepath.Join(root, item.dir)
		if e = os.Mkdir(path, 0700); e != nil {
			return nil, e
		}
		env = append(env, item.key+"="+path)
	}
	return env, nil
}

// GrandchildHeartbeat is a local cancellation fixture, not an OS sandbox.
func GrandchildHeartbeat(path string) int {
	if !SafeFixtureWorkspace(filepath.Dir(path)) || filepath.Base(path) != "heartbeat.txt" {
		return 64
	}
	for {
		if e := os.WriteFile(path, []byte("fixture"+time.Now().UTC().Format(time.RFC3339Nano)), 0600); e != nil {
			return 74
		}
		time.Sleep(10 * time.Millisecond)
	}
}

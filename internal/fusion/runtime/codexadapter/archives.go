package codexadapter

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	managed "github.com/yetone/magpie/internal/fusion/runtime"
	"github.com/yetone/magpie/internal/fusion/runtime/codex"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
	"github.com/yetone/magpie/internal/fusion/workspace"
)

const maxArchiveFile = 4 << 20
const maxArchiveBytes = 8 << 20

var archiveThreadUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var archiveRolloutPath = regexp.MustCompile(`^sessions/[0-9]{4}/[0-9]{2}/[0-9]{2}/rollout-[0-9A-Za-z._-]{1,128}\.jsonl$`)

// CheckpointRef is the public opaque reference: nothing about the rollout,
// prompt text or private paths is disclosed through it.
type CheckpointRef struct {
	ID     string `json:"id"`
	Digest string `json:"digest"`
}

// CheckpointInfo is trusted service metadata. It contains no archived text or
// credential. Task/project authorization is still required before disclosure.
type CheckpointInfo struct {
	RunID, TaskID, ProjectID, ThreadID, NativeSessionID, Workspace string
	RuntimeVersion, ExecutableHash                                 string
	Generation, PlanRevision                                       int64
	Role                                                           stageplan.Role
	Target                                                         stageplan.ExecutionTarget
	FileCount                                                      int
}

func (CheckpointInfo) String() string { return "Codex private checkpoint metadata" }
func (*Archives) String() string      { return "Codex private archives" }

type archiveIdentity struct {
	Device, Inode uint64
	UID           uint32
}
type archiveFile struct {
	Name, Hash string
	Bytes      int
}

// checkpointRecord is built by the owning Adapter only after the durable run
// is succeeded and released; the native process is stopped, so the bytes below
// have no remaining writer.
type checkpointRecord struct {
	run                             store.StageRun
	projectID, threadID, cwd, owner string
	marker                          []byte
	files                           map[string][]byte
}
type checkpointManifest struct {
	Version                                                               int
	Run                                                                   store.StageRun
	ProjectID, ThreadID, Workspace, RuntimeVersion, ExecutableHash, Owner string
	Files                                                                 []archiveFile
}
type sealedCheckpoint struct {
	Payload json.RawMessage `json:"payload"`
	MAC     string          `json:"mac"`
}
type Archives struct {
	mu       sync.Mutex
	path     string
	root     *os.Root
	identity archiveIdentity
	keyInfo  os.FileInfo
	key      []byte
	closed   bool
}

// NewArchives opens a private controller directory outside Git and every
// Native launch root. The seal key is controller state, not Native data.
func NewArchives(dir string) (*Archives, error) {
	if workspace.PrivateState(dir) != nil {
		return nil, codex.ErrUnverified
	}
	root, e := os.OpenRoot(dir)
	if e != nil {
		return nil, codex.ErrUnverified
	}
	failed := true
	defer func() {
		if failed {
			root.Close()
		}
	}()
	info, e := root.Stat(".")
	if e != nil {
		return nil, codex.ErrUnverified
	}
	id, ok := archiveIdentityOf(info)
	if !ok {
		return nil, codex.ErrUnverified
	}
	if _, e = root.Lstat("archive.key"); os.IsNotExist(e) {
		key := make([]byte, 32)
		if _, e = rand.Read(key); e != nil {
			return nil, codex.ErrUnverified
		}
		if e = archiveWrite(root, "archive.key", key); e != nil {
			return nil, e
		}
		if e = archiveSync(root); e != nil {
			return nil, e
		}
	}
	key, keyInfo, e := archiveReadFile(root, "archive.key", 32, true)
	if e != nil || len(key) != 32 {
		return nil, codex.ErrUnverified
	}
	a := &Archives{path: dir, root: root, identity: id, keyInfo: keyInfo, key: append([]byte(nil), key...)}
	if a.check() != nil {
		return nil, codex.ErrUnverified
	}
	failed = false
	return a, nil
}
func (a *Archives) Close() error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return nil
	}
	a.closed = true
	clear(a.key)
	return a.root.Close()
}
func (a *Archives) check() error {
	if a == nil || a.closed || workspace.PrivateState(a.path) != nil {
		return codex.ErrIdentity
	}
	info, e := os.Stat(a.path)
	if e != nil {
		return codex.ErrIdentity
	}
	id, ok := archiveIdentityOf(info)
	if !ok || id != a.identity {
		return codex.ErrIdentity
	}
	key, info, e := archiveReadFile(a.root, "archive.key", 32, true)
	if e != nil || !sameReadFile(info, a.keyInfo) || !bytes.Equal(key, a.key) {
		return codex.ErrIdentity
	}
	return nil
}
func archiveHash(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func archiveDigest(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && hex.EncodeToString(b) == s
}
func archiveSync(root *os.Root) error {
	f, e := root.Open(".")
	if e != nil {
		return codex.ErrUnverified
	}
	defer f.Close()
	if f.Sync() != nil {
		return codex.ErrUnverified
	}
	return nil
}
func archiveWrite(root *os.Root, name string, data []byte) error {
	f, e := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return codex.ErrUnverified
	}
	_, write := f.Write(data)
	synced := f.Sync()
	closed := f.Close()
	if write != nil || synced != nil || closed != nil {
		return codex.ErrUnverified
	}
	return nil
}
func archiveReadFile(root *os.Root, name string, limit int, private bool) ([]byte, os.FileInfo, error) {
	before, e := root.Lstat(name)
	if e != nil {
		return nil, nil, codex.ErrUnverified
	}
	identity, owned := archiveIdentityOf(before)
	if !owned || private && identity.UID != uint32(os.Getuid()) || !before.Mode().IsRegular() || !singleArchiveLink(before) || before.Size() > int64(limit) || before.Mode().Perm()&0022 != 0 || private && before.Mode().Perm() != 0600 {
		return nil, nil, codex.ErrUnverified
	}
	f, e := root.Open(name)
	if e != nil {
		return nil, nil, codex.ErrUnverified
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !sameReadFile(before, info) {
		return nil, nil, codex.ErrIdentity
	}
	b, e := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if e != nil || len(b) > limit {
		return nil, nil, codex.ErrUnverified
	}
	after, e := f.Stat()
	path, pe := root.Lstat(name)
	if e != nil || pe != nil || !sameReadFile(info, after) || !sameReadFile(info, path) {
		return nil, nil, codex.ErrIdentity
	}
	return b, info, nil
}
func sameReadFile(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.Mode() == b.Mode() && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

// archiveTree verifies the exact file set of a checkpoint directory that may
// carry sessions/YYYY/MM/DD subtrees: no extra files, no symlinks, private
// directory permissions throughout.
func archiveTree(root *os.Root, expected []string) bool {
	var files []string
	var walk func(dir string) bool
	walk = func(dir string) bool {
		f, e := root.Open(dir)
		if e != nil {
			return false
		}
		entries, e := f.ReadDir(-1)
		f.Close()
		if e != nil {
			return false
		}
		for _, entry := range entries {
			name := filepath.Join(dir, entry.Name())
			if entry.Type()&os.ModeSymlink != 0 {
				return false
			}
			if entry.IsDir() {
				info, e := entry.Info()
				if e != nil || info.Mode().Perm() != 0700 {
					return false
				}
				if !walk(name) {
					return false
				}
				continue
			}
			files = append(files, name)
		}
		return true
	}
	if !walk(".") {
		return false
	}
	want := append([]string(nil), expected...)
	got := append([]string(nil), files...)
	sort.Strings(want)
	sort.Strings(got)
	if len(want) != len(got) {
		return false
	}
	for i := range want {
		if want[i] != got[i] {
			return false
		}
	}
	return true
}

// archiveText bounds the rollout shape and rejects stage-secret reflection.
// Rollouts are NDJSON: each line is bounded and marker-checked separately,
// because a multi-line document never parses as one JSON value.
// Thread-id containment in the file name is checked by the callers that know
// the thread id (the capture record and the open manifest).
func archiveText(name string, data []byte, marker []byte) bool {
	if !archiveRolloutPath.MatchString(name) || !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 || len(data) == 0 {
		return false
	}
	if len(marker) == 0 || len(marker) > 4096 || bytes.Contains(data, marker) {
		return false
	}
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		if len(line) > 1<<20 || privateJSON(line, string(marker)) {
			return false
		}
	}
	return true
}

func (a *Archives) capture(ctx context.Context, r checkpointRecord) (CheckpointRef, error) {
	if a == nil || ctx.Err() != nil {
		return CheckpointRef{}, codex.ErrUnverified
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.check() != nil || r.run.State != "succeeded" || r.run.Owner != "" || r.owner == "" || !r.run.LaunchConfirmed || r.run.NativeSessionID == "" || r.run.ID == "" || r.run.TaskID == "" || r.projectID == "" || r.run.Generation < 1 || !archiveThreadUUID.MatchString(r.threadID) || r.cwd == "" || workspace.PrivateState(r.cwd) != nil {
		return CheckpointRef{}, codex.ErrIdentity
	}
	names := make([]string, 0, len(r.files))
	for name := range r.files {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 || len(names) > 4 {
		return CheckpointRef{}, codex.ErrUnverified
	}
	m := checkpointManifest{Version: 1, Run: r.run, ProjectID: r.projectID, ThreadID: r.threadID, Workspace: r.cwd, RuntimeVersion: codex.CLIVersion, ExecutableHash: managed.CodexExecutableSHA256, Owner: r.owner}
	total := 0
	for _, name := range names {
		data := r.files[name]
		if !strings.Contains(filepath.Base(name), r.threadID) || !archiveText(name, data, r.marker) {
			return CheckpointRef{}, codex.ErrUnverified
		}
		total += len(data)
		if total > maxArchiveBytes {
			return CheckpointRef{}, codex.ErrUnverified
		}
		m.Files = append(m.Files, archiveFile{name, archiveHash(data), len(data)})
	}
	raw, e := json.Marshal(m)
	if e != nil || len(raw) > maxArchiveFile {
		return CheckpointRef{}, codex.ErrUnverified
	}
	mac := hmac.New(sha256.New, a.key)
	mac.Write(raw)
	sealed, _ := json.Marshal(sealedCheckpoint{raw, hex.EncodeToString(mac.Sum(nil))})
	if len(sealed) > maxArchiveFile {
		return CheckpointRef{}, codex.ErrUnverified
	}
	id := archiveHash([]byte(r.run.ID + ":" + string(r.run.Role) + ":" + r.threadID + ":" + m.Files[0].Hash))
	ref := CheckpointRef{id, archiveHash(sealed)}
	if _, e = a.root.Lstat(id); e == nil {
		if _, _, e = a.open(ref); e != nil {
			return CheckpointRef{}, e
		}
		return ref, nil
	} else if !os.IsNotExist(e) {
		return CheckpointRef{}, codex.ErrUnverified
	}
	var random [16]byte
	if _, e = rand.Read(random[:]); e != nil {
		return CheckpointRef{}, codex.ErrUnverified
	}
	temp := ".checkpoint-" + hex.EncodeToString(random[:])
	if a.root.Mkdir(temp, 0700) != nil {
		return CheckpointRef{}, codex.ErrUnverified
	}
	defer a.root.RemoveAll(temp)
	dest, e := a.root.OpenRoot(temp)
	if e != nil {
		return CheckpointRef{}, codex.ErrUnverified
	}
	defer dest.Close()
	// Rollout names carry sessions/YYYY/MM/DD subtrees; build each level
	// explicitly (os.Root never creates missing parents implicitly).
	dirs := map[string]bool{}
	for _, name := range names {
		dirs[filepath.Dir(name)] = true
	}
	for dir := range dirs {
		parts := strings.Split(dir, "/")
		for i := range parts {
			if e := dest.Mkdir(strings.Join(parts[:i+1], "/"), 0700); e != nil {
				return CheckpointRef{}, codex.ErrUnverified
			}
		}
	}
	for _, name := range names {
		if e = archiveWrite(dest, name, r.files[name]); e != nil {
			return CheckpointRef{}, e
		}
	}
	if e = archiveWrite(dest, "manifest.json", sealed); e != nil || archiveSync(dest) != nil {
		return CheckpointRef{}, codex.ErrUnverified
	}
	if ctx.Err() != nil || a.check() != nil {
		return CheckpointRef{}, codex.ErrIdentity
	}
	if a.root.Rename(temp, id) != nil || archiveSync(a.root) != nil {
		return CheckpointRef{}, codex.ErrUnverified
	}
	return ref, nil
}

// open validates the entire seal before exposing any archived bytes to a
// trusted restore consumer. It does not authenticate an HTTP caller or run.
func (a *Archives) open(ref CheckpointRef) (checkpointManifest, map[string][]byte, error) {
	var m checkpointManifest
	if a.check() != nil || !archiveDigest(ref.ID) || !archiveDigest(ref.Digest) {
		return m, nil, codex.ErrIdentity
	}
	st, e := a.root.Lstat(ref.ID)
	if e != nil || !st.IsDir() || st.Mode().Perm() != 0700 {
		return m, nil, codex.ErrIdentity
	}
	root, e := a.root.OpenRoot(ref.ID)
	if e != nil {
		return m, nil, codex.ErrUnverified
	}
	defer root.Close()
	sealed, _, e := archiveReadFile(root, "manifest.json", maxArchiveFile, true)
	if e != nil || archiveHash(sealed) != ref.Digest {
		return m, nil, codex.ErrIdentity
	}
	var envelope sealedCheckpoint
	if archiveJSON(sealed, &envelope) != nil {
		return m, nil, codex.ErrProtocol
	}
	mac := hmac.New(sha256.New, a.key)
	mac.Write(envelope.Payload)
	actual, e := hex.DecodeString(envelope.MAC)
	if e != nil || !hmac.Equal(actual, mac.Sum(nil)) || archiveJSON(envelope.Payload, &m) != nil {
		return m, nil, codex.ErrIdentity
	}
	names := []string{"manifest.json"}
	for _, file := range m.Files {
		names = append(names, file.Name)
	}
	if m.Version != 1 || m.RuntimeVersion != codex.CLIVersion || m.ExecutableHash != managed.CodexExecutableSHA256 || len(m.Files) == 0 || len(m.Files) > 4 || m.Run.State != "succeeded" || m.Run.Owner != "" || m.Owner == "" || !archiveThreadUUID.MatchString(m.ThreadID) || m.Workspace == "" || !archiveTree(root, names) {
		return m, nil, codex.ErrIdentity
	}
	total := 0
	blobs := map[string][]byte{}
	for _, file := range m.Files {
		if !archiveRolloutPath.MatchString(file.Name) || !strings.Contains(filepath.Base(file.Name), m.ThreadID) || !archiveDigest(file.Hash) || file.Bytes <= 0 || file.Bytes > maxArchiveFile {
			return m, nil, codex.ErrIdentity
		}
		data, _, e := archiveReadFile(root, file.Name, maxArchiveFile, true)
		if e != nil || len(data) != file.Bytes || archiveHash(data) != file.Hash {
			return m, nil, codex.ErrIdentity
		}
		total += len(data)
		if total > maxArchiveBytes {
			return m, nil, codex.ErrUnverified
		}
		blobs[file.Name] = data
	}
	if a.check() != nil {
		return m, nil, codex.ErrIdentity
	}
	return m, blobs, nil
}
func (a *Archives) Info(ref CheckpointRef) (CheckpointInfo, error) {
	if a == nil {
		return CheckpointInfo{}, codex.ErrUnverified
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	m, _, e := a.open(ref)
	if e != nil {
		return CheckpointInfo{}, e
	}
	return CheckpointInfo{RunID: m.Run.ID, TaskID: m.Run.TaskID, ProjectID: m.ProjectID, ThreadID: m.ThreadID, NativeSessionID: m.Run.NativeSessionID, Workspace: m.Workspace, RuntimeVersion: m.RuntimeVersion, ExecutableHash: m.ExecutableHash, Generation: m.Run.Generation, PlanRevision: m.Run.PlanRevision, Role: m.Run.Role, Target: m.Run.Target, FileCount: len(m.Files)}, nil
}

// The manifest has its own 4 MiB bound. Duplicate keys, invalid UTF-8/NUL and
// trailing JSON still fail.
func archiveJSON(raw []byte, out any) error {
	if len(raw) == 0 || len(raw) > maxArchiveFile || !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return codex.ErrProtocol
	}
	if !json.Valid(raw) {
		return codex.ErrProtocol
	}
	if json.Unmarshal(raw, out) != nil {
		return codex.ErrProtocol
	}
	return nil
}

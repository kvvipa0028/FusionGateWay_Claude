package grok

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/fusion/policy"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/store"
	"github.com/yetone/magpie/internal/fusion/workspace"
)

const maxArchiveFile = 2 << 20
const maxArchiveBytes = 16 << 20

// This is the observed 1.0.48 session layout, not an arbitrary directory copy.
var nativeArchiveFiles = []string{"chat_history.jsonl", "chat_history.jsonl.lock", "events.jsonl", "prompt_context.json", "rewind_points.jsonl", "rewind_points.jsonl.lock", "signals.json", "summary.json", "summary.json.lock", "system_prompt.txt", "tool_definitions.json", "updates.jsonl", "updates.jsonl.lock", "usage.json"}

type CheckpointRef struct {
	ID     string `json:"id"`
	Digest string `json:"digest"`
}

// CheckpointInfo is trusted service metadata. It contains no archived text or
// credential. Task/project authorization is still required before disclosure.
type CheckpointInfo struct {
	RunID, TaskID, ProjectID, NativeSessionID, Workspace string
	RuntimeVersion, ExecutableHash, ReportHash           string
	Generation, PlanRevision                             int64
	Role                                                 stageplan.Role
	Target                                               stageplan.ExecutionTarget
	FileCount, ReadCount                                 int
}

func (CheckpointInfo) String() string { return "Grok private checkpoint metadata" }
func (*Archives) String() string      { return "Grok private archives" }

type archiveIdentity struct {
	Device, Inode uint64
	UID           uint32
}
type archiveRead struct {
	Call                          readCall
	Argument, Path, Relative, Raw string
	Identity                      archiveIdentity
	Mode                          uint32
	Bytes, Modified               int64
}
type archiveFile struct {
	Name, Hash string
	Bytes      int
}
type checkpointRecord struct {
	run                         store.StageRun
	projectID, root, cwd, owner string
	cwdIdentity                 archiveIdentity
	rootIdentity                archiveIdentity
	proof                       policy.StopProof
	reads                       []archiveRead
	markers                     [][]byte
}
type checkpointManifest struct {
	Version                                                     int
	Run                                                         store.StageRun
	ProjectID, Workspace, RuntimeVersion, ExecutableHash, Owner string
	WorkspaceIdentity                                           archiveIdentity
	Proof                                                       policy.StopProof
	Files                                                       []archiveFile
	Reads                                                       []archiveRead
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
	if !readPlatformSupported || workspace.PrivateState(dir) != nil {
		return nil, ErrUnverified
	}
	root, e := os.OpenRoot(dir)
	if e != nil {
		return nil, ErrUnverified
	}
	failed := true
	defer func() {
		if failed {
			root.Close()
		}
	}()
	info, e := root.Stat(".")
	if e != nil {
		return nil, ErrUnverified
	}
	id, ok := archiveIdentityOf(info)
	if !ok {
		return nil, ErrUnverified
	}
	if _, e = root.Lstat("archive.key"); os.IsNotExist(e) {
		key := make([]byte, 32)
		if _, e = rand.Read(key); e != nil {
			return nil, ErrUnverified
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
		return nil, ErrUnverified
	}
	a := &Archives{path: dir, root: root, identity: id, keyInfo: keyInfo, key: append([]byte(nil), key...)}
	if a.check() != nil {
		return nil, ErrUnverified
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
		return ErrIdentity
	}
	info, e := os.Stat(a.path)
	if e != nil {
		return ErrIdentity
	}
	id, ok := archiveIdentityOf(info)
	if !ok || id != a.identity {
		return ErrIdentity
	}
	key, info, e := archiveReadFile(a.root, "archive.key", 32, true)
	if e != nil || !sameReadFile(info, a.keyInfo) || !bytes.Equal(key, a.key) {
		return ErrIdentity
	}
	return nil
}
func archiveCwd(cwd string) string { return strings.ReplaceAll(url.QueryEscape(cwd), "+", "%20") }
func archiveHash(b []byte) string  { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func archiveDigest(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && hex.EncodeToString(b) == s
}
func archiveSeparate(a, b string) bool {
	return a != b && !strings.HasPrefix(a, b+string(filepath.Separator)) && !strings.HasPrefix(b, a+string(filepath.Separator))
}
func archiveSync(root *os.Root) error {
	f, e := root.Open(".")
	if e != nil {
		return ErrUnverified
	}
	defer f.Close()
	if f.Sync() != nil {
		return ErrUnverified
	}
	return nil
}
func archiveWrite(root *os.Root, name string, data []byte) error {
	f, e := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return ErrUnverified
	}
	_, write := f.Write(data)
	synced := f.Sync()
	closed := f.Close()
	if write != nil || synced != nil || closed != nil {
		return ErrUnverified
	}
	return nil
}
func archiveReadFile(root *os.Root, name string, limit int, private bool) ([]byte, os.FileInfo, error) {
	before, e := root.Lstat(name)
	if e != nil {
		return nil, nil, ErrUnverified
	}
	identity, owned := archiveIdentityOf(before)
	if e != nil || !owned || private && identity.UID != uint32(os.Getuid()) || !before.Mode().IsRegular() || !singleReadLink(before) || before.Size() > int64(limit) || before.Mode().Perm()&0022 != 0 || private && before.Mode().Perm() != 0600 {
		return nil, nil, ErrUnverified
	}
	f, e := root.Open(name)
	if e != nil {
		return nil, nil, ErrUnverified
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !sameReadFile(before, info) {
		return nil, nil, ErrIdentity
	}
	b, e := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if e != nil || len(b) > limit {
		return nil, nil, ErrUnverified
	}
	after, e := f.Stat()
	path, pe := root.Lstat(name)
	if e != nil || pe != nil || !sameReadFile(info, after) || !sameReadFile(info, path) {
		return nil, nil, ErrIdentity
	}
	return b, info, nil
}
func archiveNames(root *os.Root, expected []string) bool {
	f, e := root.Open(".")
	if e != nil {
		return false
	}
	defer f.Close()
	names, e := f.ReadDir(len(expected) + 1)
	if e != nil && e != io.EOF || len(names) != len(expected) {
		return false
	}
	want := append([]string(nil), expected...)
	sort.Strings(want)
	got := make([]string, 0, len(names))
	for _, n := range names {
		got = append(got, n.Name())
	}
	sort.Strings(got)
	for i := range want {
		if want[i] != got[i] {
			return false
		}
	}
	return true
}
func archiveText(name string, data []byte, markers [][]byte) bool {
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return false
	}
	if strings.HasSuffix(name, ".lock") {
		return len(data) == 0
	}
	for _, marker := range markers {
		if len(marker) == 0 || len(marker) > 4096 || bytes.Contains(data, marker) {
			return false
		}
	}
	if strings.HasSuffix(name, ".jsonl") {
		for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
			if len(line) == 0 {
				continue
			}
			var value any
			if len(line) > 1<<20 || decode(line, &value) != nil || privateJSON(line, markers...) {
				return false
			}
		}
	} else if strings.HasSuffix(name, ".json") {
		var value any
		if archiveJSON(data, &value) != nil || privateValue(value, markers, 0) {
			return false
		}
	}
	return true
}
func archiveSummary(data []byte, run store.StageRun, cwd string) bool {
	var summary struct {
		Info  struct{ ID, Cwd string }
		Model string `json:"current_model_id"`
	}
	return archiveJSON(data, &summary) == nil && summary.Info.ID == run.NativeSessionID && summary.Info.Cwd == cwd && summary.Model == run.Target.RequestedModel
}
func archiveWorkspace(cwd string, id archiveIdentity, reads []archiveRead) bool {
	if workspace.PrivateState(cwd) != nil {
		return false
	}
	root, e := os.OpenRoot(cwd)
	if e != nil {
		return false
	}
	defer root.Close()
	info, e := root.Stat(".")
	if e != nil {
		return false
	}
	actual, ok := archiveIdentityOf(info)
	if !ok || actual != id {
		return false
	}
	seen := map[string]bool{}
	total := 0
	for _, read := range reads {
		argument, ok := readArgument([]byte(read.Call.Arguments))
		if !ok || read.Call.Name != "read_file" || !toolID.MatchString(read.Call.ID) || seen[read.Call.ID] || argument != read.Argument || read.Relative == "." || read.Relative == ".." || filepath.IsAbs(read.Relative) || filepath.Clean(read.Relative) != read.Relative || strings.HasPrefix(read.Relative, "../") || filepath.Join(cwd, read.Relative) != read.Path {
			return false
		}
		seen[read.Call.ID] = true
		for i, parts := 0, strings.Split(read.Relative, string(filepath.Separator)); i < len(parts); i++ {
			st, e := root.Lstat(filepath.Join(parts[:i+1]...))
			if e != nil || st.Mode()&os.ModeSymlink != 0 {
				return false
			}
		}
		data, st, e := archiveReadFile(root, read.Relative, 65536, false)
		if e != nil {
			return false
		}
		got, ok := archiveIdentityOf(st)
		if !ok || got != read.Identity || uint32(st.Mode()) != read.Mode || st.Size() != read.Bytes || st.ModTime().UnixNano() != read.Modified || string(data) != read.Raw || !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 || bytes.Count(data, []byte("\n"))+1 > 1000 {
			return false
		}
		total += len(data)
		if total > 512<<10 {
			return false
		}
	}
	return len(reads) <= 64
}
func (a *Archives) capture(ctx context.Context, r checkpointRecord) (CheckpointRef, error) {
	if a == nil || ctx.Err() != nil {
		return CheckpointRef{}, ErrUnverified
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	rootInfo, rootErr := os.Stat(r.root)
	var rootID archiveIdentity
	var rootOK bool
	if rootErr == nil {
		rootID, rootOK = archiveIdentityOf(rootInfo)
	}
	if !rootOK || rootID != r.rootIdentity {
		return CheckpointRef{}, ErrIdentity
	}
	if a.check() != nil || r.run.State != "succeeded" || r.run.Owner != "" || r.owner == "" || !r.run.LaunchConfirmed || !uuid.MatchString(r.run.NativeSessionID) || r.run.ID == "" || r.run.TaskID == "" || r.projectID == "" || r.run.Generation < 1 || r.proof.RunID != r.run.ID || r.proof.Generation != r.run.Generation || r.proof.NativeSessionID != r.run.NativeSessionID || !r.proof.DescendantsStopped || !archiveDigest(r.proof.ReportHash) || !archiveDigest(r.proof.ProcessIdentityHash) || workspace.PrivateState(r.root) != nil || !archiveSeparate(a.path, r.root) || !archiveSeparate(a.path, r.cwd) || !archiveWorkspace(r.cwd, r.cwdIdentity, r.reads) {
		return CheckpointRef{}, ErrIdentity
	}
	session := filepath.Join(r.root, "config", "grok", "sessions", archiveCwd(r.cwd), r.run.NativeSessionID)
	if workspace.CanonicalDirectory(session, false) != nil {
		return CheckpointRef{}, ErrUnverified
	}
	source, e := os.OpenRoot(session)
	if e != nil {
		return CheckpointRef{}, ErrUnverified
	}
	defer source.Close()
	if !archiveNames(source, nativeArchiveFiles) {
		return CheckpointRef{}, ErrUnverified
	}
	m := checkpointManifest{Version: 1, Owner: r.owner, Run: r.run, ProjectID: r.projectID, Workspace: r.cwd, WorkspaceIdentity: r.cwdIdentity, RuntimeVersion: CLIVersion, ExecutableHash: NativeExecutableSHA256, Proof: r.proof, Reads: append([]archiveRead(nil), r.reads...)}
	blobs := map[string][]byte{}
	total := 0
	for _, name := range nativeArchiveFiles {
		b, _, e := archiveReadFile(source, name, maxArchiveFile, false)
		if e != nil || !archiveText(name, b, r.markers) {
			return CheckpointRef{}, ErrUnverified
		}
		total += len(b)
		if total > maxArchiveBytes {
			return CheckpointRef{}, ErrUnverified
		}
		blobs[name] = b
		m.Files = append(m.Files, archiveFile{name, archiveHash(b), len(b)})
	}
	if !archiveSummary(blobs["summary.json"], r.run, r.cwd) {
		return CheckpointRef{}, ErrIdentity
	}
	raw, e := json.Marshal(m)
	if e != nil || len(raw) > maxArchiveFile {
		return CheckpointRef{}, ErrUnverified
	}
	mac := hmac.New(sha256.New, a.key)
	mac.Write(raw)
	sealed, _ := json.Marshal(sealedCheckpoint{raw, hex.EncodeToString(mac.Sum(nil))})
	if len(sealed) > maxArchiveFile {
		return CheckpointRef{}, ErrUnverified
	}
	id := archiveHash([]byte(r.run.ID + ":" + string(r.run.Role) + ":" + r.proof.ReportHash))
	ref := CheckpointRef{id, archiveHash(sealed)}
	if _, e = a.root.Lstat(id); e == nil {
		if _, _, e = a.open(ref); e != nil {
			return CheckpointRef{}, e
		}
		return ref, nil
	} else if !os.IsNotExist(e) {
		return CheckpointRef{}, ErrUnverified
	}
	var random [16]byte
	if _, e = rand.Read(random[:]); e != nil {
		return CheckpointRef{}, ErrUnverified
	}
	temp := ".checkpoint-" + hex.EncodeToString(random[:])
	if a.root.Mkdir(temp, 0700) != nil {
		return CheckpointRef{}, ErrUnverified
	}
	defer a.root.RemoveAll(temp)
	dest, e := a.root.OpenRoot(temp)
	if e != nil {
		return CheckpointRef{}, ErrUnverified
	}
	defer dest.Close()
	for _, name := range nativeArchiveFiles {
		if e = archiveWrite(dest, name, blobs[name]); e != nil {
			return CheckpointRef{}, e
		}
	}
	if e = archiveWrite(dest, "manifest.json", sealed); e != nil || archiveSync(dest) != nil {
		return CheckpointRef{}, ErrUnverified
	}
	// Recheck the source after copying; an incomplete or changed Native directory
	// never becomes a published checkpoint. Only the controller snapshot is sealed.
	rootInfo, rootErr = os.Stat(r.root)
	if rootErr == nil {
		rootID, rootOK = archiveIdentityOf(rootInfo)
	}
	if rootErr != nil || !rootOK || rootID != r.rootIdentity || ctx.Err() != nil || a.check() != nil || !archiveNames(source, nativeArchiveFiles) || !archiveWorkspace(r.cwd, r.cwdIdentity, r.reads) {
		return CheckpointRef{}, ErrIdentity
	}
	for _, file := range m.Files {
		b, _, e := archiveReadFile(source, file.Name, maxArchiveFile, false)
		if e != nil || archiveHash(b) != file.Hash {
			return CheckpointRef{}, ErrIdentity
		}
	}
	if a.root.Rename(temp, id) != nil || archiveSync(a.root) != nil {
		return CheckpointRef{}, ErrUnverified
	}
	return ref, nil
}

// open validates the entire seal before exposing any archived bytes to a
// trusted restore consumer. It does not authenticate an HTTP caller or run.
func (a *Archives) open(ref CheckpointRef) (checkpointManifest, map[string][]byte, error) {
	var m checkpointManifest
	if a.check() != nil || !archiveDigest(ref.ID) || !archiveDigest(ref.Digest) {
		return m, nil, ErrIdentity
	}
	st, e := a.root.Lstat(ref.ID)
	if e != nil || !st.IsDir() || st.Mode().Perm() != 0700 {
		return m, nil, ErrIdentity
	}
	root, e := a.root.OpenRoot(ref.ID)
	if e != nil {
		return m, nil, ErrUnverified
	}
	defer root.Close()
	names := append(append([]string(nil), nativeArchiveFiles...), "manifest.json")
	if !archiveNames(root, names) {
		return m, nil, ErrUnverified
	}
	sealed, _, e := archiveReadFile(root, "manifest.json", maxArchiveFile, true)
	if e != nil || archiveHash(sealed) != ref.Digest {
		return m, nil, ErrIdentity
	}
	var envelope sealedCheckpoint
	if archiveJSON(sealed, &envelope) != nil {
		return m, nil, ErrProtocol
	}
	mac := hmac.New(sha256.New, a.key)
	mac.Write(envelope.Payload)
	actual, e := hex.DecodeString(envelope.MAC)
	if e != nil || !hmac.Equal(actual, mac.Sum(nil)) || archiveJSON(envelope.Payload, &m) != nil {
		return m, nil, ErrIdentity
	}
	if m.Version != 1 || m.RuntimeVersion != CLIVersion || m.ExecutableHash != NativeExecutableSHA256 || len(m.Files) != len(nativeArchiveFiles) || m.Run.State != "succeeded" || m.Run.Owner != "" || m.Owner == "" || !uuid.MatchString(m.Run.NativeSessionID) || m.Proof.RunID != m.Run.ID || m.Proof.Generation != m.Run.Generation || m.Proof.NativeSessionID != m.Run.NativeSessionID || !archiveWorkspace(m.Workspace, m.WorkspaceIdentity, m.Reads) {
		return m, nil, ErrIdentity
	}
	total := 0
	blobs := map[string][]byte{}
	for i, file := range m.Files {
		if file.Name != nativeArchiveFiles[i] || !archiveDigest(file.Hash) || file.Bytes < 0 || file.Bytes > maxArchiveFile {
			return m, nil, ErrIdentity
		}
		data, _, e := archiveReadFile(root, file.Name, maxArchiveFile, true)
		if e != nil || len(data) != file.Bytes || archiveHash(data) != file.Hash {
			return m, nil, ErrIdentity
		}
		total += len(data)
		if total > maxArchiveBytes {
			return m, nil, ErrUnverified
		}
		blobs[file.Name] = data
	}
	if !archiveSummary(blobs["summary.json"], m.Run, m.Workspace) || a.check() != nil {
		return m, nil, ErrIdentity
	}
	return m, blobs, nil
}
func (a *Archives) Info(ref CheckpointRef) (CheckpointInfo, error) {
	if a == nil {
		return CheckpointInfo{}, ErrUnverified
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	m, _, e := a.open(ref)
	if e != nil {
		return CheckpointInfo{}, e
	}
	// m is decoded for this call, so all returned slices/pointers are independent.
	return CheckpointInfo{RunID: m.Run.ID, TaskID: m.Run.TaskID, ProjectID: m.ProjectID, NativeSessionID: m.Run.NativeSessionID, Workspace: m.Workspace, RuntimeVersion: m.RuntimeVersion, ExecutableHash: m.ExecutableHash, ReportHash: m.Proof.ReportHash, Generation: m.Run.Generation, PlanRevision: m.Run.PlanRevision, Role: m.Run.Role, Target: m.Run.Target, FileCount: len(m.Files), ReadCount: len(m.Reads)}, nil
}

// The manifest has its own 2 MiB bound. Native NDJSON remains capped at 1 MiB
// per frame. Duplicate/folded keys, depth, NUL and trailing JSON still fail.
func archiveJSON(raw []byte, out any) error {
	if len(raw) == 0 || len(raw) > maxArchiveFile || !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ErrProtocol
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if unique(d, 0) != nil {
		return ErrProtocol
	}
	if _, e := d.Token(); e != io.EOF {
		return ErrProtocol
	}
	if json.Unmarshal(raw, out) != nil {
		return ErrProtocol
	}
	return nil
}

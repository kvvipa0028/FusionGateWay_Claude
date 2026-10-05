package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"unicode/utf8"
)

// ArtifactEntry hashes code semantics, including empty directories and the
// owner's execute bit. Private-copy permissions are not source-tree content.
type ArtifactEntry struct {
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	Hash       string `json:"sha256,omitempty"`
	Bytes      int64  `json:"bytes,omitempty"`
	Executable bool   `json:"executable,omitempty"`
}
type Change struct {
	Path   string         `json:"path"`
	Before *ArtifactEntry `json:"before"`
	After  *ArtifactEntry `json:"after"`
}
type ArtifactManifest struct {
	Version      int             `json:"version"`
	BaseCommit   *string         `json:"base_commit"`
	BaseTreeHash string          `json:"base_tree_hash"`
	TreeHash     string          `json:"tree_hash"`
	ChangeHash   string          `json:"change_hash"`
	Entries      []ArtifactEntry `json:"entries"`
	Changes      []Change        `json:"changes"`
}

// FrozenArtifact is owned in-memory provenance. Reading or editing a manifest
// cannot create it. Durable Store registration/restoration is a separate gate.
type FrozenArtifact struct {
	base     *sourceSeal
	seal     *artifactSeal
	manifest ArtifactManifest
}
type artifactSeal struct {
	path     string
	info     os.FileInfo
	code     *sourceSeal
	manifest sourceEntry
	header   *sourceEntry
}

func (FrozenArtifact) String() string   { return "frozen code artifact (redacted)" }
func (FrozenArtifact) GoString() string { return "FrozenArtifact(<redacted>)" }
func (a FrozenArtifact) Path() string {
	if a.seal == nil {
		return ""
	}
	return a.seal.path
}
func (a FrozenArtifact) Manifest() ArtifactManifest {
	b, _ := json.Marshal(a.manifest)
	var m ArtifactManifest
	_ = json.Unmarshal(b, &m)
	return m
}
func (a FrozenArtifact) Current() bool {
	return a.base != nil && a.seal != nil && (Snapshot{source: a.base}).SourceCurrent() && a.seal.current()
}

// Freeze publishes a separate code copy plus a hash-bound change manifest.
// The producer must be stopped by the caller; this observation is not a lock.
func Freeze(s Snapshot, root, name string) (FrozenArtifact, error) {
	g, err := s.Guard()
	if err != nil || !g.ValidFor(s.source.copyPath) || len(s.source.anchors) >= 16 || PrivateState(root) != nil || !safeCopyName(name) || withinDirectory(root, s.source.path) || withinDirectory(root, s.source.copyPath) {
		return FrozenArtifact{}, ErrUnsafe
	}
	dest := filepath.Join(root, name)
	if _, err := os.Lstat(dest); !os.IsNotExist(err) {
		return FrozenArtifact{}, ErrUnsafe
	}
	tmp, err := os.MkdirTemp(root, ".freeze-")
	if err != nil {
		return FrozenArtifact{}, err
	}
	defer os.RemoveAll(tmp)
	copy, err := Copy(s.source.copyPath, tmp, "code")
	if err != nil {
		return FrozenArtifact{}, err
	}
	base, entries := treeEntries(s.source), treeEntries(copy.source)
	for _, tree := range [][]ArtifactEntry{base, entries} {
		for _, entry := range tree {
			if !utf8.ValidString(entry.Path) {
				return FrozenArtifact{}, ErrUnsafe
			}
		}
	}
	changes := treeChanges(base, entries)
	m := ArtifactManifest{Version: 1, BaseCommit: s.source.baseCommit, BaseTreeHash: jsonHash(base), TreeHash: jsonHash(entries), ChangeHash: jsonHash(changes), Entries: entries, Changes: changes}
	// Capture the published code's own identity, never the mutable producer's.
	for _, entry := range entries {
		if entry.Kind == "file" {
			mode := os.FileMode(0400)
			if entry.Executable {
				mode |= 0100
			}
			if err := os.Chmod(filepath.Join(copy.Path, entry.Path), mode); err != nil {
				return FrozenArtifact{}, err
			}
		}
	}
	code, err := sealDirectory(copy.Path)
	if err != nil {
		return FrozenArtifact{}, err
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return FrozenArtifact{}, err
	}
	if err := os.WriteFile(filepath.Join(tmp, "manifest.json"), raw, 0400); err != nil {
		return FrozenArtifact{}, err
	}
	mi, err := os.Lstat(filepath.Join(tmp, "manifest.json"))
	if err != nil {
		return FrozenArtifact{}, err
	}
	info, err := os.Lstat(tmp)
	if err != nil || !g.ValidFor(s.source.copyPath) || !copy.SourceCurrent() {
		return FrozenArtifact{}, ErrUnsafe
	}
	if _, err := os.Lstat(dest); !os.IsNotExist(err) {
		return FrozenArtifact{}, ErrUnsafe
	}
	if err := os.Rename(tmp, dest); err != nil {
		return FrozenArtifact{}, err
	}
	code.path = filepath.Join(dest, "code")
	a := FrozenArtifact{base: s.source, seal: &artifactSeal{path: dest, info: info, code: code, manifest: sourceEntry{info: mi, hash: contentHash(raw)}}, manifest: m}
	if !a.Current() {
		return FrozenArtifact{}, ErrUnsafe
	}
	return a, nil
}

// Copy verifies the entire parent artifact before/after transfer and retains
// the registered original source plus every frozen ancestor in the guard.
// Each consumer receives a fresh writable private tree, no Native session.
func (a FrozenArtifact) Copy(root, name string) (Snapshot, error) {
	if !a.Current() || len(a.base.anchors) >= 16 || withinDirectory(root, a.base.path) {
		return Snapshot{}, ErrUnsafe
	}
	s, err := Copy(a.seal.code.path, root, name)
	if err != nil {
		return Snapshot{}, err
	}
	if !a.Current() || jsonHash(treeEntries(s.source)) != a.manifest.TreeHash {
		_ = os.RemoveAll(s.Path)
		return Snapshot{}, ErrUnsafe
	}
	anchors := append(append([]*artifactSeal(nil), a.base.anchors...), a.seal)
	s.source = &sourceSeal{path: a.base.path, entries: a.base.entries, baseCommit: a.base.baseCommit, copyPath: s.Path, copyInfo: s.source.copyInfo, anchors: anchors}
	return s, nil
}

func withinDirectory(path, parent string) bool {
	return path == parent || strings.HasPrefix(path, parent+string(os.PathSeparator))
}

func (a *artifactSeal) current() bool {
	if a == nil || PrivateState(a.path) != nil {
		return false
	}
	i, err := os.Lstat(a.path)
	if err != nil || !sameArtifactDirectory(a.info, i) || !(Snapshot{source: a.code}).SourceCurrent() {
		return false
	}
	r, err := os.OpenRoot(a.path)
	if err != nil {
		return false
	}
	defer r.Close()
	i, err = r.Lstat("manifest.json")
	if err != nil || !sameSourceInfo(a.manifest.info, i) {
		return false
	}
	b, err := readSourceFile(r, "manifest.json", i)
	if err != nil || contentHash(b) != a.manifest.hash {
		return false
	}
	if a.header != nil {
		i, err := r.Lstat("handoff.json")
		if err != nil || !sameSourceInfo(a.header.info, i) {
			return false
		}
		b, err := readSourceFile(r, "handoff.json", i)
		if err != nil || contentHash(b) != a.header.hash {
			return false
		}
	}
	return true
}

// Bundle metadata may be added alongside code after freeze. APFS directory
// nlink changes for that addition; only the code subtree is a fixed entry set.
func sameArtifactDirectory(a, b os.FileInfo) bool {
	if a == nil || b == nil || !a.IsDir() || !b.IsDir() || !os.SameFile(a, b) || a.Mode() != b.Mode() {
		return false
	}
	x, ok := a.Sys().(*syscall.Stat_t)
	y, other := b.Sys().(*syscall.Stat_t)
	return ok && other && x.Uid == y.Uid && x.Gid == y.Gid
}

func sealDirectory(path string) (*sourceSeal, error) {
	r, err := os.OpenRoot(path)
	if err != nil {
		return nil, ErrUnsafe
	}
	defer r.Close()
	s := &sourceSeal{path: path, entries: map[string]sourceEntry{}}
	err = walkSource(r, nil, func(rel string, i os.FileInfo, b []byte) error {
		hash := ""
		if !i.IsDir() {
			hash = contentHash(b)
		}
		s.entries[rel] = sourceEntry{info: i, hash: hash}
		return nil
	})
	if err != nil || !(Snapshot{source: s}).SourceCurrent() {
		return nil, ErrUnsafe
	}
	return s, nil
}
func treeEntries(s *sourceSeal) []ArtifactEntry {
	entries := make([]ArtifactEntry, 0, len(s.entries)-1)
	for p, e := range s.entries {
		if p == "." {
			continue
		}
		x := ArtifactEntry{Path: p, Kind: "directory"}
		if !e.info.IsDir() {
			x.Kind, x.Hash, x.Bytes, x.Executable = "file", e.hash, e.info.Size(), e.info.Mode().Perm()&0100 != 0
		}
		entries = append(entries, x)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries
}
func treeChanges(before, after []ArtifactEntry) []Change {
	old, now := map[string]ArtifactEntry{}, map[string]ArtifactEntry{}
	for _, e := range before {
		old[e.Path] = e
	}
	for _, e := range after {
		now[e.Path] = e
	}
	paths := map[string]bool{}
	for p := range old {
		paths[p] = true
	}
	for p := range now {
		paths[p] = true
	}
	changes := []Change{}
	for p := range paths {
		b, bok := old[p]
		a, aok := now[p]
		if bok == aok && reflect.DeepEqual(b, a) {
			continue
		}
		c := Change{Path: p}
		if bok {
			c.Before = &b
		}
		if aok {
			c.After = &a
		}
		changes = append(changes, c)
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes
}
func jsonHash(v any) string { b, _ := json.Marshal(v); return contentHash(b) }

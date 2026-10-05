package workspace

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"syscall"
)

// Fingerprint records the identity checks already used by SourceGuard. It is
// evidence data, not a filesystem or execution capability.
type Fingerprint struct {
	Device   uint64 `json:"device"`
	Inode    uint64 `json:"inode"`
	Mode     uint32 `json:"mode"`
	UID      uint32 `json:"uid"`
	GID      uint32 `json:"gid"`
	Links    uint64 `json:"links"`
	Bytes    int64  `json:"bytes"`
	Modified int64  `json:"modified"`
}
type witnessEntry struct {
	Path string      `json:"path"`
	Info Fingerprint `json:"info"`
	Hash string      `json:"sha256"`
}
type witnessSource struct {
	Path    string         `json:"path"`
	Commit  *string        `json:"commit"`
	Entries []witnessEntry `json:"entries"`
}
type witnessArtifact struct {
	Path     string        `json:"path"`
	Info     Fingerprint   `json:"info"`
	Code     witnessSource `json:"code"`
	Manifest witnessEntry  `json:"manifest"`
	Header   *witnessEntry `json:"header"`
}
type artifactWitness struct {
	Version   int               `json:"version"`
	StateRoot string            `json:"state_root"`
	StateInfo Fingerprint       `json:"state_info"`
	Source    witnessSource     `json:"source"`
	Artifacts []witnessArtifact `json:"artifacts"`
	Manifest  ArtifactManifest  `json:"manifest"`
}

func fingerprint(i os.FileInfo) (Fingerprint, error) {
	s, ok := i.Sys().(*syscall.Stat_t)
	if !ok {
		return Fingerprint{}, ErrUnsafe
	}
	f := Fingerprint{Device: uint64(s.Dev), Inode: uint64(s.Ino), Mode: uint32(i.Mode()), UID: s.Uid, GID: s.Gid, Links: uint64(s.Nlink)}
	if !i.IsDir() {
		f.Bytes, f.Modified = i.Size(), i.ModTime().UnixNano()
	}
	return f, nil
}

func matchesFingerprint(expected Fingerprint, i os.FileInfo, outer bool) bool {
	actual, err := fingerprint(i)
	if err != nil {
		return false
	}
	if outer {
		actual.Links = 0
		expected.Links = 0
	}
	return actual == expected
}

// WithHandoffHeader returns a new seal; it never mutates a shared source guard.
// Every subsequent consumer guard observes this header as well as code/manifest.
func (a FrozenArtifact) WithHandoffHeader(hash string) (FrozenArtifact, error) {
	if !a.Current() || len(hash) != 64 || a.seal.header != nil {
		return FrozenArtifact{}, ErrUnsafe
	}
	r, err := os.OpenRoot(a.Path())
	if err != nil {
		return FrozenArtifact{}, ErrUnsafe
	}
	defer r.Close()
	i, err := r.Lstat("handoff.json")
	if err != nil || i.Mode().Perm() != 0400 || !i.Mode().IsRegular() || i.Size() > 8<<20 {
		return FrozenArtifact{}, ErrUnsafe
	}
	b, err := readSourceFile(r, "handoff.json", i)
	st, ok := i.Sys().(*syscall.Stat_t)
	if err != nil || contentHash(b) != hash || !ok || st.Uid != uint32(os.Getuid()) || st.Nlink != 1 {
		return FrozenArtifact{}, ErrUnsafe
	}
	s := *a.seal
	s.header = &sourceEntry{info: i, hash: hash}
	a.seal = &s
	if !a.Current() {
		return FrozenArtifact{}, ErrUnsafe
	}
	return a, nil
}
func (a FrozenArtifact) HeaderFingerprint() (Fingerprint, string, error) {
	if !a.Current() || a.seal.header == nil {
		return Fingerprint{}, "", ErrUnsafe
	}
	f, err := fingerprint(a.seal.header.info)
	return f, a.seal.header.hash, err
}

// ExportWitness serializes owned provenance. Only an independently stored hash
// and independently registered source/state scope may authorize RestoreWitness.
func (a FrozenArtifact) ExportWitness(stateRoot string) ([]byte, error) {
	if !a.Current() || PrivateState(stateRoot) != nil {
		return nil, ErrUnsafe
	}
	i, err := os.Lstat(stateRoot)
	if err != nil {
		return nil, ErrUnsafe
	}
	state, err := fingerprint(i)
	if err != nil {
		return nil, err
	}
	w := artifactWitness{Version: 1, StateRoot: stateRoot, StateInfo: state, Manifest: a.Manifest()}
	w.Source, err = exportSource(a.base)
	if err != nil {
		return nil, err
	}
	all := append(append([]*artifactSeal(nil), a.base.anchors...), a.seal)
	var estimated int64
	for _, entry := range w.Source.Entries {
		estimated += 512 + int64(len(entry.Path))*6
	}
	for _, s := range all {
		if s.path == stateRoot || !withinDirectory(s.path, stateRoot) {
			return nil, ErrUnsafe
		}
		x := witnessArtifact{Path: s.path}
		x.Info, err = fingerprint(s.info)
		if err != nil {
			return nil, err
		}
		x.Info.Links = 0 // Outer metadata additions are allowed; code entry sets are fixed.
		x.Code, err = exportSource(s.code)
		if err != nil {
			return nil, err
		}
		x.Manifest.Info, err = fingerprint(s.manifest.info)
		if err != nil {
			return nil, err
		}
		x.Manifest.Path, x.Manifest.Hash = "manifest.json", s.manifest.hash
		if s.header != nil {
			fp, err := fingerprint(s.header.info)
			if err != nil {
				return nil, err
			}
			x.Header = &witnessEntry{Path: "handoff.json", Info: fp, Hash: s.header.hash}
		}
		for _, entry := range x.Code.Entries {
			estimated += 512 + int64(len(entry.Path))*6
		}
		if estimated > 32<<20 {
			return nil, ErrUnsafe
		}
		w.Artifacts = append(w.Artifacts, x)
	}
	raw, err := json.Marshal(w)
	if err != nil || len(raw) > 32<<20 || !a.Current() {
		return nil, ErrUnsafe
	}
	return raw, nil
}
func exportSource(s *sourceSeal) (witnessSource, error) {
	w := witnessSource{Path: s.path, Commit: s.baseCommit}
	for p, e := range s.entries {
		fp, err := fingerprint(e.info)
		if err != nil {
			return w, err
		}
		w.Entries = append(w.Entries, witnessEntry{Path: p, Info: fp, Hash: e.hash})
	}
	sort.Slice(w.Entries, func(i, j int) bool { return w.Entries[i].Path < w.Entries[j].Path })
	return w, nil
}

// RestoreWitness is trusted storage integration, not a task JSON importer.
// expectedHash must come from the protected Store receipt; source/stateRoot
// must come from the current independent project/host registration.
func RestoreWitness(raw []byte, expectedHash, source, stateRoot string) (FrozenArtifact, error) {
	if len(raw) == 0 || len(raw) > 32<<20 || contentHash(raw) != expectedHash || CanonicalDirectory(source, false) != nil || PrivateState(stateRoot) != nil {
		return FrozenArtifact{}, ErrUnsafe
	}
	var w artifactWitness
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&w) != nil || w.Version != 1 || w.Source.Path != source || w.StateRoot != stateRoot || len(w.Artifacts) == 0 || len(w.Artifacts) > 16 {
		return FrozenArtifact{}, ErrUnsafe
	}
	canonical, err := json.Marshal(w)
	if err != nil || !bytes.Equal(canonical, raw) {
		return FrozenArtifact{}, ErrUnsafe
	}
	i, err := os.Lstat(stateRoot)
	if err != nil || !matchesFingerprint(w.StateInfo, i, true) {
		return FrozenArtifact{}, ErrUnsafe
	}
	base, err := importSource(w.Source)
	if err != nil {
		return FrozenArtifact{}, err
	}
	var seals []*artifactSeal
	seen := map[string]bool{}
	for _, x := range w.Artifacts {
		if x.Path == stateRoot || !withinDirectory(x.Path, stateRoot) || seen[x.Path] || x.Code.Path != filepath.Join(x.Path, "code") || x.Code.Commit != nil || PrivateState(x.Path) != nil || x.Manifest.Path != "manifest.json" {
			return FrozenArtifact{}, ErrUnsafe
		}
		seen[x.Path] = true
		code, err := importSource(x.Code)
		if err != nil {
			return FrozenArtifact{}, err
		}
		info, err := os.Lstat(x.Path)
		if err != nil || !matchesFingerprint(x.Info, info, true) {
			return FrozenArtifact{}, ErrUnsafe
		}
		r, err := os.OpenRoot(x.Path)
		if err != nil {
			return FrozenArtifact{}, ErrUnsafe
		}
		mi, err := r.Lstat("manifest.json")
		if err != nil || !matchesFingerprint(x.Manifest.Info, mi, false) {
			r.Close()
			return FrozenArtifact{}, ErrUnsafe
		}
		s := &artifactSeal{path: x.Path, info: info, code: code, manifest: sourceEntry{info: mi, hash: x.Manifest.Hash}}
		if x.Header != nil {
			if x.Header.Path != "handoff.json" {
				r.Close()
				return FrozenArtifact{}, ErrUnsafe
			}
			hi, err := r.Lstat("handoff.json")
			if err != nil || !matchesFingerprint(x.Header.Info, hi, false) {
				r.Close()
				return FrozenArtifact{}, ErrUnsafe
			}
			s.header = &sourceEntry{info: hi, hash: x.Header.Hash}
		}
		r.Close()
		if !s.current() {
			return FrozenArtifact{}, ErrUnsafe
		}
		seals = append(seals, s)
	}
	base.anchors = append([]*artifactSeal(nil), seals[:len(seals)-1]...)
	a := FrozenArtifact{base: base, seal: seals[len(seals)-1], manifest: w.Manifest}
	if w.Manifest.Version != 1 || !reflect.DeepEqual(w.Manifest.BaseCommit, base.baseCommit) || w.Manifest.BaseTreeHash != jsonHash(treeEntries(base)) || w.Manifest.TreeHash != jsonHash(treeEntries(a.seal.code)) || !reflect.DeepEqual(w.Manifest.Changes, treeChanges(treeEntries(base), treeEntries(a.seal.code))) || w.Manifest.ChangeHash != jsonHash(w.Manifest.Changes) || !a.Current() {
		return FrozenArtifact{}, ErrUnsafe
	}
	r, err := os.OpenRoot(a.Path())
	if err != nil {
		return FrozenArtifact{}, ErrUnsafe
	}
	defer r.Close()
	b, err := readSourceFile(r, "manifest.json", a.seal.manifest.info)
	expected, marshalErr := json.Marshal(w.Manifest)
	if err != nil || marshalErr != nil || !bytes.Equal(b, expected) {
		return FrozenArtifact{}, ErrUnsafe
	}
	return a, nil
}
func importSource(w witnessSource) (*sourceSeal, error) {
	if len(w.Entries) == 0 || len(w.Entries) > 20000 {
		return nil, ErrUnsafe
	}
	s := &sourceSeal{path: w.Path, baseCommit: w.Commit, entries: map[string]sourceEntry{}}
	expected := map[string]witnessEntry{}
	for _, e := range w.Entries {
		if e.Path != "." && (filepath.IsAbs(e.Path) || filepath.Clean(e.Path) != e.Path || e.Path == ".." || withinDirectory(e.Path, "..")) {
			return nil, ErrUnsafe
		}
		if _, ok := expected[e.Path]; ok {
			return nil, ErrUnsafe
		}
		expected[e.Path] = e
	}
	r, err := os.OpenRoot(w.Path)
	if err != nil {
		return nil, ErrUnsafe
	}
	defer r.Close()
	// Check persisted metadata before reading any code bytes. os.SameFile only
	// accepts real FileInfo objects returned by os.Stat, not fabricated wrappers.
	err = fs.WalkDir(r.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return ErrUnsafe
		}
		switch d.Name() {
		case ".git", ".claude", ".codex", ".grok", ".fusion-dev", ".env":
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		e, ok := expected[path]
		if !ok {
			return ErrUnsafe
		}
		i, err := r.Lstat(path)
		if err != nil || !matchesFingerprint(e.Info, i, false) {
			return ErrUnsafe
		}
		s.entries[path] = sourceEntry{info: i, hash: e.Hash}
		return nil
	})
	if err != nil || len(s.entries) != len(expected) {
		return nil, ErrUnsafe
	}
	if _, ok := s.entries["."]; !ok || !(Snapshot{source: s}).SourceCurrent() {
		return nil, ErrUnsafe
	}
	return s, nil
}

func WitnessDescends(child, parent []byte) bool {
	if len(child) > 32<<20 || len(parent) > 32<<20 {
		return false
	}
	var c, p artifactWitness
	if json.Unmarshal(child, &c) != nil || json.Unmarshal(parent, &p) != nil || c.Version != 1 || p.Version != 1 || len(c.Artifacts) != len(p.Artifacts)+1 || len(c.Artifacts) > 16 || !reflect.DeepEqual(c.Source, p.Source) || c.StateRoot != p.StateRoot {
		return false
	}
	c.StateInfo.Links, p.StateInfo.Links = 0, 0
	return c.StateInfo == p.StateInfo && reflect.DeepEqual(c.Artifacts[:len(p.Artifacts)], p.Artifacts)
}

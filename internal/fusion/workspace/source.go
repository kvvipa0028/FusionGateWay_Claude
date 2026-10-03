package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"syscall"
)

type sourceEntry struct {
	info os.FileInfo
	hash string
}
type sourceSeal struct {
	path     string
	entries  map[string]sourceEntry
	copyPath string
	copyInfo os.FileInfo
}

// SourceGuard is trusted in-memory provenance, never a serialized authority.
type SourceGuard struct{ seal *sourceSeal }

func (SourceGuard) String() string   { return "workspace source guard (redacted)" }
func (SourceGuard) GoString() string { return "SourceGuard(<redacted>)" }
func (g SourceGuard) Present() bool  { return g.seal != nil }
func (s Snapshot) Guard() (SourceGuard, error) {
	g := SourceGuard{seal: s.source}
	if s.source == nil || !g.ValidFor(s.source.copyPath) {
		return SourceGuard{}, ErrUnsafe
	}
	return g, nil
}
func (g SourceGuard) ValidFor(cwd string) bool {
	if g.seal == nil || g.seal.copyInfo == nil || cwd != g.seal.copyPath {
		return false
	}
	copyCurrent := func() bool {
		if PrivateState(cwd) != nil {
			return false
		}
		i, e := os.Lstat(cwd)
		if e != nil || !i.IsDir() || !os.SameFile(g.seal.copyInfo, i) || i.Mode() != g.seal.copyInfo.Mode() {
			return false
		}
		x, ok := i.Sys().(*syscall.Stat_t)
		y, other := g.seal.copyInfo.Sys().(*syscall.Stat_t)
		return ok && other && x.Uid == y.Uid && x.Gid == y.Gid
	}
	return copyCurrent() && (Snapshot{source: g.seal}).SourceCurrent() && copyCurrent()
}

func contentHash(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func sameSourceInfo(a, b os.FileInfo) bool {
	if a == nil || b == nil || !os.SameFile(a, b) || a.Mode() != b.Mode() {
		return false
	}
	x, ok := a.Sys().(*syscall.Stat_t)
	y, other := b.Sys().(*syscall.Stat_t)
	if !ok || !other || x.Uid != y.Uid || x.Gid != y.Gid || x.Nlink != y.Nlink {
		return false
	}
	// Directory timestamps change for excluded authentication files. The included
	// entry set below, directory identity and permissions remain authoritative.
	return a.IsDir() || a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

func readSourceFile(root *os.Root, rel string, info os.FileInfo) ([]byte, error) {
	in, e := root.OpenFile(rel, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if e != nil {
		return nil, ErrUnsafe
	}
	defer in.Close()
	opened, e := in.Stat()
	if e != nil || !sameSourceInfo(info, opened) {
		return nil, ErrUnsafe
	}
	b, e := io.ReadAll(io.LimitReader(in, info.Size()+1))
	if e != nil || int64(len(b)) != info.Size() {
		return nil, ErrUnsafe
	}
	after, e := in.Stat()
	if e != nil || !sameSourceInfo(info, after) {
		return nil, ErrUnsafe
	}
	return b, nil
}

// Traverse the opened directory, never the possibly replaced absolute path.
func walkSource(root *os.Root, expected *sourceSeal, visit func(string, os.FileInfo, []byte) error) error {
	var entries, files int
	var total int64
	return fs.WalkDir(root.FS(), ".", func(rel string, d fs.DirEntry, e error) error {
		if e != nil {
			return ErrUnsafe
		}
		switch d.Name() {
		case ".git", ".claude", ".codex", ".grok", ".fusion-dev", ".env":
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		entries++
		if entries > 20000 {
			return ErrUnsafe
		}
		i, e := root.Lstat(rel)
		if e != nil || i.IsDir() != d.IsDir() {
			return ErrUnsafe
		}
		// A recheck must not read the bytes of newly discovered or replaced files.
		if expected != nil {
			entry, ok := expected.entries[rel]
			if !ok || !sameSourceInfo(entry.info, i) {
				return ErrUnsafe
			}
		}
		if i.IsDir() {
			return visit(rel, i, nil)
		}
		st, ok := i.Sys().(*syscall.Stat_t)
		if !i.Mode().IsRegular() || !ok || st.Nlink != 1 || files >= 10000 || i.Size() > 20<<20 || i.Size() < 0 || total+i.Size() > 100<<20 {
			return ErrUnsafe
		}
		b, e := readSourceFile(root, rel, i)
		if e != nil {
			return ErrUnsafe
		}
		files++
		total += int64(len(b))
		return visit(rel, i, b)
	})
}

// SourceCurrent is a bounded observation of the originally copied source.
// Public Path/Files are not authority. This is not a lock against other writers.
func (s Snapshot) SourceCurrent() bool {
	seal := s.source
	if seal == nil || len(seal.entries) == 0 || CanonicalDirectory(seal.path, false) != nil {
		return false
	}
	root, e := os.OpenRoot(seal.path)
	if e != nil {
		return false
	}
	defer root.Close()
	count := 0
	e = walkSource(root, seal, func(rel string, i os.FileInfo, b []byte) error {
		expected, ok := seal.entries[rel]
		if !ok || !sameSourceInfo(expected.info, i) || !i.IsDir() && expected.hash != contentHash(b) {
			return ErrUnsafe
		}
		count++
		return nil
	})
	if e != nil || count != len(seal.entries) || CanonicalDirectory(seal.path, false) != nil {
		return false
	}
	current, e := os.Lstat(seal.path)
	return e == nil && sameSourceInfo(seal.entries["."].info, current)
}

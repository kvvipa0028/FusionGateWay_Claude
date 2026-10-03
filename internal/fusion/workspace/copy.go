package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

var ErrUnsafe = errors.New("unsafe workspace boundary")

type File struct {
	Path  string `json:"path"`
	Hash  string `json:"sha256"`
	Bytes int64  `json:"bytes"`
}
type Snapshot struct {
	Path  string
	Files []File
}

// CanonicalDirectory rejects linked parents and state inside an existing Git
// checkout. The caller must select an approved project before copying data.
func CanonicalDirectory(p string, private bool) error {
	if !filepath.IsAbs(p) || filepath.Clean(p) != p || p == "/" {
		return ErrUnsafe
	}
	real, e := filepath.EvalSymlinks(p)
	if e != nil || real != p {
		return ErrUnsafe
	}
	i, e := os.Lstat(p)
	if e != nil || !i.IsDir() {
		return ErrUnsafe
	}
	if private {
		if i.Mode().Perm()&0077 != 0 {
			return ErrUnsafe
		}
		st, ok := i.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != uint32(os.Getuid()) {
			return ErrUnsafe
		}
	}
	return nil
}
func PrivateState(p string) error {
	if e := CanonicalDirectory(p, true); e != nil {
		return e
	}
	for dir := p; ; dir = filepath.Dir(dir) {
		if _, e := os.Lstat(filepath.Join(dir, ".git")); e == nil {
			return ErrUnsafe
		}
		if dir == "/" {
			break
		}
	}
	return nil
}
func Copy(source, root, name string) (Snapshot, error) {
	var result Snapshot
	if CanonicalDirectory(source, false) != nil || PrivateState(root) != nil || name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00\n\r") {
		return result, ErrUnsafe
	}
	if root == source || strings.HasPrefix(root, source+"/") {
		return result, ErrUnsafe
	}
	dest := filepath.Join(root, name)
	if _, e := os.Lstat(dest); !os.IsNotExist(e) {
		return result, ErrUnsafe
	}
	tmp, e := os.MkdirTemp(root, ".copy-")
	if e != nil {
		return result, e
	}
	defer os.RemoveAll(tmp)
	sourceRoot, e := os.OpenRoot(source)
	if e != nil {
		return result, ErrUnsafe
	}
	defer sourceRoot.Close()
	var total int64
	e = filepath.WalkDir(source, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return ErrUnsafe
		}
		rel, e := filepath.Rel(source, path)
		if e != nil {
			return ErrUnsafe
		}
		if rel == "." {
			return nil
		}
		switch d.Name() {
		case ".git", ".claude", ".codex", ".grok", ".fusion-dev", ".env":
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		i, e := os.Lstat(path)
		if e != nil {
			return ErrUnsafe
		}
		out := filepath.Join(tmp, rel)
		if i.IsDir() {
			return os.Mkdir(out, 0700)
		}
		st, ok := i.Sys().(*syscall.Stat_t)
		if !i.Mode().IsRegular() || !ok || st.Nlink != 1 || len(result.Files) >= 10000 || i.Size() > 20<<20 || total+i.Size() > 100<<20 {
			return ErrUnsafe
		}
		in, e := sourceRoot.OpenFile(rel, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
		if e != nil {
			return ErrUnsafe
		}
		defer in.Close()
		opened, e := in.Stat()
		if e != nil || !os.SameFile(i, opened) {
			return ErrUnsafe
		}
		openedStat, ok := opened.Sys().(*syscall.Stat_t)
		if !ok || openedStat.Nlink != 1 {
			return ErrUnsafe
		}
		b, e := io.ReadAll(io.LimitReader(in, i.Size()+1))
		if e != nil || int64(len(b)) != i.Size() {
			return ErrUnsafe
		}
		after, e := in.Stat()
		if e != nil || after.Size() != i.Size() || !after.ModTime().Equal(i.ModTime()) {
			return ErrUnsafe
		}
		if e = os.WriteFile(out, b, 0600); e != nil {
			return e
		}
		h := sha256.Sum256(b)
		result.Files = append(result.Files, File{rel, hex.EncodeToString(h[:]), int64(len(b))})
		total += int64(len(b))
		return nil
	})
	if e != nil {
		return Snapshot{}, e
	}
	if e = os.Rename(tmp, dest); e != nil {
		return Snapshot{}, e
	}
	result.Path = dest
	return result, nil
}

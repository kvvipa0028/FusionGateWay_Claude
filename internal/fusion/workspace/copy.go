package workspace

import (
	"errors"
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
	Path   string
	Files  []File
	source *sourceSeal
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
	return copyWorkspace(source, root, name, nil)
}

func copyWorkspace(source, root, name string, beforePublish func()) (Snapshot, error) {
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
	seal := &sourceSeal{path: source, entries: map[string]sourceEntry{}}
	e = walkSource(sourceRoot, nil, func(rel string, i os.FileInfo, b []byte) error {
		hash := ""
		if !i.IsDir() {
			hash = contentHash(b)
		}
		seal.entries[rel] = sourceEntry{info: i, hash: hash}
		if rel == "." {
			return nil
		}
		out := filepath.Join(tmp, rel)
		if i.IsDir() {
			return os.Mkdir(out, 0700)
		}
		if e := os.WriteFile(out, b, 0600); e != nil {
			return e
		}
		result.Files = append(result.Files, File{rel, hash, int64(len(b))})
		return nil
	})
	if e != nil {
		return Snapshot{}, e
	}
	result.source = seal
	if beforePublish != nil {
		beforePublish()
	}
	if !result.SourceCurrent() {
		return Snapshot{}, ErrUnsafe
	}
	if e = os.Rename(tmp, dest); e != nil {
		return Snapshot{}, e
	}
	result.Path = dest
	return result, nil
}

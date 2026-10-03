package testsupport

import (
	"os"
	"path/filepath"
)

// CreateSyntheticRepo copies only the four declared regular fixture files.
// A relative escape symlink points to a synthetic sibling, never user data.
func CreateSyntheticRepo(template, root string) error {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return os.ErrInvalid
	}
	if _, e := os.Lstat(root); !os.IsNotExist(e) {
		return os.ErrExist
	}
	if e := os.Mkdir(root, 0700); e != nil {
		return e
	}
	for _, name := range []string{"go.mod", "calc.go", "calc_test.go", "readonly.txt"} {
		source := filepath.Join(template, name)
		info, e := os.Lstat(source)
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return os.ErrInvalid
		}
		b, e := os.ReadFile(source)
		if e != nil {
			return e
		}
		mode := os.FileMode(0600)
		if name == "readonly.txt" {
			mode = 0444
		}
		if e = os.WriteFile(filepath.Join(root, name), b, mode); e != nil {
			return e
		}
	}
	if e := PrepareFixtureWorkspace(root); e != nil {
		return e
	}
	outside := filepath.Join(filepath.Dir(root), "outside-fixture.txt")
	file, e := os.OpenFile(outside, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = file.WriteString("fixture-only outside scope\n")
	file.Close()
	if e != nil {
		return e
	}
	return os.Symlink("../outside-fixture.txt", filepath.Join(root, "escape-link"))
}

package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func private(t *testing.T) string {
	t.Helper()
	p := t.TempDir()
	os.Chmod(p, 0700)
	p, _ = filepath.EvalSymlinks(p)
	return p
}
func TestCopyPreservesSourceAndExcludesAuthentication(t *testing.T) {
	source, root := private(t), private(t)
	os.WriteFile(filepath.Join(source, "calc.go"), []byte("fixture source"), 0600)
	os.Mkdir(filepath.Join(source, ".git"), 0700)
	os.Mkdir(filepath.Join(source, ".claude"), 0700)
	os.WriteFile(filepath.Join(source, ".claude", "auth.json"), []byte("fixture private"), 0600)
	os.WriteFile(filepath.Join(source, ".env"), []byte("fixture private"), 0600)
	c, e := Copy(source, root, "fixture-copy")
	if e != nil {
		t.Fatal(e)
	}
	if len(c.Files) != 1 || c.Files[0].Path != "calc.go" {
		t.Fatal("authentication copied", c.Files)
	}
	os.WriteFile(filepath.Join(c.Path, "calc.go"), []byte("changed"), 0600)
	b, _ := os.ReadFile(filepath.Join(source, "calc.go"))
	if string(b) != "fixture source" {
		t.Fatal("source altered")
	}
	if _, e = Copy(source, root, "fixture-copy"); e == nil {
		t.Fatal("existing copy overwritten")
	}
}
func TestCopyRejectsLinksAndNeverLeavesPartialWorkspace(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			source, root, out := private(t), private(t), private(t)
			f := filepath.Join(out, "private")
			os.WriteFile(f, []byte("fixture private"), 0600)
			if kind == "symlink" {
				os.Symlink(f, filepath.Join(source, "escape"))
			} else {
				os.Link(f, filepath.Join(source, "escape"))
			}
			if _, e := Copy(source, root, "fixture-copy"); e == nil {
				t.Fatal("link accepted")
			}
			entries, _ := os.ReadDir(root)
			if len(entries) != 0 {
				t.Fatal("partial workspace retained")
			}
		})
	}
}

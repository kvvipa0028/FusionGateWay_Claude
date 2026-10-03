package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotSourceCurrentUsesPrivateSeal(t *testing.T) {
	source, root := private(t), private(t)
	p := filepath.Join(source, "input.txt")
	if e := os.WriteFile(p, []byte("source"), 0600); e != nil {
		t.Fatal(e)
	}
	s, e := Copy(source, root, "copy")
	if e != nil || !s.SourceCurrent() {
		t.Fatal("copied source not current", e)
	}
	s.Files[0] = File{Path: "forged", Hash: "forged"}
	s.Files = nil
	s.Path = "forged"
	if !s.SourceCurrent() {
		t.Fatal("public manifest changed authority")
	}
	if e = os.WriteFile(p, []byte("changed"), 0600); e != nil {
		t.Fatal(e)
	}
	if s.SourceCurrent() || (Snapshot{}).SourceCurrent() {
		t.Fatal("changed or unowned source accepted")
	}
}

func TestSnapshotSourceCurrentRejectsWholeTreeDrift(t *testing.T) {
	for _, kind := range []string{"content", "same_bytes_replacement", "add_file", "remove_file", "add_directory", "replace_directory", "symlink", "hardlink", "mode", "root_replacement"} {
		t.Run(kind, func(t *testing.T) {
			source, root := private(t), private(t)
			dir := filepath.Join(source, "dir")
			if e := os.Mkdir(dir, 0700); e != nil {
				t.Fatal(e)
			}
			p := filepath.Join(dir, "input.txt")
			if e := os.WriteFile(p, []byte("source"), 0600); e != nil {
				t.Fatal(e)
			}
			s, e := Copy(source, root, "copy")
			if e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "content":
				e = os.WriteFile(p, []byte("change"), 0600)
			case "same_bytes_replacement":
				e = os.Rename(p, p+".old")
				if e == nil {
					e = os.WriteFile(p, []byte("source"), 0600)
				}
				if e == nil {
					e = os.Remove(p + ".old")
				}
			case "add_file":
				e = os.WriteFile(filepath.Join(source, "new"), []byte("new"), 0600)
			case "remove_file":
				e = os.Remove(p)
			case "add_directory":
				e = os.Mkdir(filepath.Join(source, "empty"), 0700)
			case "replace_directory":
				e = os.Rename(dir, dir+".old")
				if e == nil {
					e = os.Mkdir(dir, 0700)
				}
				if e == nil {
					e = os.WriteFile(p, []byte("source"), 0600)
				}
				if e == nil {
					e = os.RemoveAll(dir + ".old")
				}
			case "symlink":
				e = os.Remove(p)
				if e == nil {
					e = os.Symlink(filepath.Join(s.Path, "dir/input.txt"), p)
				}
			case "hardlink":
				e = os.Link(p, filepath.Join(root, "alias"))
			case "mode":
				e = os.Chmod(p, 0644)
			case "root_replacement":
				e = os.Rename(source, source+".old")
				t.Cleanup(func() { os.RemoveAll(source + ".old") })
				if e == nil {
					e = os.Mkdir(source, 0700)
				}
				if e == nil {
					e = os.Mkdir(dir, 0700)
				}
				if e == nil {
					e = os.WriteFile(p, []byte("source"), 0600)
				}
			}
			if e != nil {
				t.Fatal(e)
			}
			if s.SourceCurrent() {
				t.Fatal("source drift accepted")
			}
		})
	}
}

func TestCopyRechecksWholeSourceBeforePublication(t *testing.T) {
	for _, kind := range []string{"content", "new_file", "root_replacement"} {
		t.Run(kind, func(t *testing.T) {
			source, root := private(t), private(t)
			p := filepath.Join(source, "a.txt")
			if e := os.WriteFile(p, []byte("source"), 0600); e != nil {
				t.Fatal(e)
			}
			s, e := copyWorkspace(source, root, "copy", func() {
				var e error
				switch kind {
				case "content":
					e = os.WriteFile(p, []byte("change"), 0600)
				case "new_file":
					e = os.WriteFile(filepath.Join(source, "new"), []byte("new"), 0600)
				case "root_replacement":
					e = os.Rename(source, source+".old")
					t.Cleanup(func() { os.RemoveAll(source + ".old") })
					if e == nil {
						e = os.Mkdir(source, 0700)
					}
					if e == nil {
						e = os.WriteFile(p, []byte("source"), 0600)
					}
				}
				if e != nil {
					t.Fatal(e)
				}
			})
			if e == nil || s.Path != "" || len(s.Files) != 0 || s.SourceCurrent() {
				t.Fatal("drift published", e)
			}
			entries, e := os.ReadDir(root)
			if e != nil || len(entries) != 0 {
				t.Fatal("partial copy remained", e)
			}
		})
	}
}

func TestSnapshotSourceExclusionsAndPrivateCopy(t *testing.T) {
	source, root := private(t), private(t)
	if e := os.Mkdir(filepath.Join(source, ".claude"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(source, ".claude/auth"), []byte("synthetic auth"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(source, "input"), []byte("source"), 0600); e != nil {
		t.Fatal(e)
	}
	s, e := Copy(source, root, "copy")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(source, ".claude/auth"), []byte("changed synthetic auth"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(s.Path, "input"), []byte("generated"), 0600); e != nil {
		t.Fatal(e)
	}
	if !s.SourceCurrent() || len(s.Files) != 1 {
		t.Fatal("excluded auth or private copy treated as source")
	}
}

func TestCopyBoundsEmptyDirectoryTree(t *testing.T) {
	source, root := private(t), private(t)
	for i := 0; i < 20000; i++ {
		if e := os.Mkdir(filepath.Join(source, fmt.Sprintf("dir-%05d", i)), 0700); e != nil {
			t.Fatal(e)
		}
	}
	if s, e := Copy(source, root, "copy"); e == nil || s.Path != "" {
		t.Fatal("unbounded directory tree accepted", e)
	}
	entries, e := os.ReadDir(root)
	if e != nil || len(entries) != 0 {
		t.Fatal("partial copy remained", e)
	}
}

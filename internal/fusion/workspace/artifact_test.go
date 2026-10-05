package workspace

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
)

func artifactFixture(t *testing.T) (Snapshot, string, string) {
	t.Helper()
	source, root := private(t), private(t)
	if err := os.WriteFile(filepath.Join(source, "script.sh"), []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "old.txt"), []byte("old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Copy(source, root, "producer")
	if err != nil {
		t.Fatal(err)
	}
	return s, source, root
}

func TestCopyRetainsOwnerExecuteWithoutGroupPermissions(t *testing.T) {
	s, _, _ := artifactFixture(t)
	i, err := os.Stat(filepath.Join(s.Path, "script.sh"))
	if err != nil || i.Mode().Perm() != 0700 {
		t.Fatal("script semantics lost", i, err)
	}
}

func TestFrozenArtifactTransfersActualChangesWithPrivateProvenance(t *testing.T) {
	s, source, root := artifactFixture(t)
	if err := os.Remove(filepath.Join(s.Path, "old.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Path, "created.bin"), []byte{0, 1, 255}, 0600); err != nil {
		t.Fatal(err)
	}
	producer := s.Path
	s.Path, s.Files = source, nil // Public metadata never selects the producer.
	a, err := Freeze(s, root, "artifact")
	if err != nil || !a.Current() {
		t.Fatal("freeze", err)
	}
	m := a.Manifest()
	if m.Version != 1 || m.BaseCommit != nil || m.BaseTreeHash == m.TreeHash || len(m.Changes) != 2 || m.Changes[0].Path != "created.bin" || m.Changes[1].Path != "old.txt" || m.Changes[0].Before != nil || m.Changes[1].After != nil {
		t.Fatal("actual changes absent", m)
	}
	want := a.Manifest()
	m.Entries[0].Hash = "forged"
	m.Changes[0].Path = "forged"
	if !reflect.DeepEqual(want, a.Manifest()) || !a.Current() {
		t.Fatal("mutable manifest authority")
	}
	if err := os.WriteFile(filepath.Join(producer, "created.bin"), []byte("later producer edits"), 0600); err != nil {
		t.Fatal(err)
	}
	if !a.Current() {
		t.Fatal("producer changes altered frozen artifact")
	}
	next, err := a.Copy(root, "consumer")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(next.Path, "created.bin"))
	if err != nil || !reflect.DeepEqual(b, []byte{0, 1, 255}) {
		t.Fatal("implementation code lost", err)
	}
	if _, err := os.Stat(filepath.Join(next.Path, "old.txt")); !os.IsNotExist(err) {
		t.Fatal("deleted file resurrected", err)
	}
	g, err := next.Guard()
	if err != nil || !g.BoundToSource(source) || !g.ValidFor(next.Path) {
		t.Fatal("original/parent provenance lost", err)
	}
	if err := os.WriteFile(filepath.Join(next.Path, "test.txt"), []byte("new test"), 0600); err != nil {
		t.Fatal(err)
	}
	a2, err := Freeze(next, root, "testing-artifact")
	if err != nil || a2.Manifest().TreeHash == want.TreeHash || len(a2.Manifest().Changes) != 3 {
		t.Fatal("testing changes lost", err)
	}
	third, err := a2.Copy(root, "review")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(third.Path, "test.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "old.txt"), []byte("original drift"), 0600); err != nil {
		t.Fatal(err)
	}
	if a.Current() || a2.Current() || next.SourceCurrent() || third.SourceCurrent() {
		t.Fatal("original drift accepted by descendants")
	}
}

func TestFrozenArtifactRejectsCorruptionBeforeCopy(t *testing.T) {
	for _, mode := range []string{"code", "manifest", "add", "symlink", "hardlink", "permission", "root", "ancestor"} {
		t.Run(mode, func(t *testing.T) {
			s, _, root := artifactFixture(t)
			a, err := Freeze(s, root, "artifact")
			if err != nil {
				t.Fatal(err)
			}
			next, err := a.Copy(root, "consumer")
			if err != nil {
				t.Fatal(err)
			}
			p := filepath.Join(a.Path(), "code", "old.txt")
			switch mode {
			case "code", "ancestor":
				if err := os.Chmod(p, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte("corrupt"), 0600); err != nil {
					t.Fatal(err)
				}
			case "manifest":
				p = filepath.Join(a.Path(), "manifest.json")
				if err := os.Chmod(p, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			case "add":
				if err := os.WriteFile(filepath.Join(a.Path(), "code", "new"), []byte("new"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(s.Path, "old.txt"), p); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(p, filepath.Join(root, "alias")); err != nil {
					t.Fatal(err)
				}
			case "permission":
				if err := os.Chmod(p, 0644); err != nil {
					t.Fatal(err)
				}
			case "root":
				if err := os.Rename(a.Path(), a.Path()+"-old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(a.Path(), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if a.Current() || next.SourceCurrent() {
				t.Fatal("corrupt artifact retained authority")
			}
			if _, err := a.Copy(root, "denied"); err == nil {
				t.Fatal("corrupt code copied")
			}
			if _, err := os.Stat(filepath.Join(root, "denied")); !os.IsNotExist(err) {
				t.Fatal("partial consumer published")
			}
			if _, err := Freeze(next, root, "denied-freeze"); err == nil {
				t.Fatal("corrupt parent frozen again")
			}
		})
	}
	if (FrozenArtifact{}).Current() {
		t.Fatal("unowned artifact current")
	}
}

func TestFreezeRejectsUnsafeProducerAndOverwrites(t *testing.T) {
	for _, mode := range []string{"symlink", "hardlink", "forged", "existing", "invalid_utf8"} {
		t.Run(mode, func(t *testing.T) {
			s, _, root := artifactFixture(t)
			var err error
			switch mode {
			case "symlink":
				err = os.Symlink(filepath.Join(root, "unknown"), filepath.Join(s.Path, "link"))
			case "hardlink":
				err = os.Link(filepath.Join(s.Path, "old.txt"), filepath.Join(root, "alias"))
			case "forged":
				s = Snapshot{Path: s.Path, Files: s.Files}
			case "existing":
				err = os.Mkdir(filepath.Join(root, "artifact"), 0700)
			case "invalid_utf8":
				err = os.WriteFile(filepath.Join(s.Path, string([]byte{0xff})), []byte("ambiguous JSON path"), 0600)
				if errors.Is(err, syscall.EILSEQ) {
					t.Skip("filesystem rejects invalid UTF-8 names before artifact freeze; byte-name fixture requires a supporting filesystem")
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Freeze(s, root, "artifact"); err == nil {
				t.Fatal("unsafe freeze accepted")
			}
		})
	}
}

func TestArtifactNeverWritesIntoRegisteredOriginal(t *testing.T) {
	s, source, root := artifactFixture(t)
	if _, err := Freeze(s, source, "denied"); err == nil {
		t.Fatal("freeze wrote original")
	}
	a, err := Freeze(s, root, "artifact")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Copy(source, "denied"); err == nil {
		t.Fatal("consumer wrote original")
	}
	if _, err := os.Stat(filepath.Join(source, "denied")); !os.IsNotExist(err) {
		t.Fatal("original mutation published")
	}
	if !a.Current() {
		t.Fatal("rejected transfer altered source")
	}
}

func TestFrozenArtifactPinsGitBaseWithoutCommittingProject(t *testing.T) {
	source, root := private(t), private(t)
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("/usr/bin/git", append([]string{"-C", source}, args...)...)
		cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + root, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid"}
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatal("fixture git", err)
		}
		return string(b)
	}
	git("init", "-q")
	if err := os.WriteFile(filepath.Join(source, "file"), []byte("base"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "file")
	git("commit", "-qm", "synthetic base")
	s, err := Copy(source, root, "producer")
	if err != nil {
		t.Fatal(err)
	}
	a, err := Freeze(s, root, "artifact")
	if err != nil || a.Manifest().BaseCommit == nil || !a.Current() {
		t.Fatal("Git base missing", err)
	}
	before := git("rev-parse", "HEAD")
	if _, err := a.Copy(root, "consumer"); err != nil {
		t.Fatal(err)
	}
	if git("rev-parse", "HEAD") != before {
		t.Fatal("handoff committed user project")
	}
	git("commit", "--allow-empty", "-qm", "moved base without tree changes")
	if a.Current() || s.SourceCurrent() {
		t.Fatal("changed base commit accepted")
	}
}

package library

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/settings"
)

// keepBackups is how many changes' backups are kept.
const keepBackups = 30

// BackupDir is where a file is copied before magpie first changes it in a
// change: one folder a change, named by when, a folder an agent in it.
func BackupDir() string { return filepath.Join(settings.Dir(), "backups") }

// backups copies each file once, the first time a change is about to
// write it.
type backups struct {
	dir  string
	done map[string]bool
}

func newBackups() *backups {
	return &backups{done: map[string]bool{}}
}

// keep copies path aside, as the agent had it, before it is written; a
// file that isn't there has nothing to keep.
func (b *backups) keep(agent, path string) error {
	if b.done[path] {
		return nil
	}
	b.done[path] = true
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if b.dir == "" {
		b.dir = filepath.Join(BackupDir(), time.Now().Format("2006-01-02_15-04-05.000"))
	}
	dst := filepath.Join(b.dir, fileName(agent), filepath.Base(path))
	if _, err := os.Stat(dst); err == nil {
		// another file by that name (each dsh profile's cordis.patch.yml):
		// kept under its folder's name
		dst = filepath.Join(b.dir, fileName(agent), filepath.Base(filepath.Dir(path)), filepath.Base(path))
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}

// prune leaves the newest keepBackups, and every one that holds a skill
// folder set aside (a skill taken out of the library, or an agent's own
// the library's took the place of): that is the only copy of it left.
func pruneBackups() {
	es, err := os.ReadDir(BackupDir())
	if err != nil {
		return
	}
	var names []string
	for _, e := range es {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && !holdsSkills(filepath.Join(BackupDir(), e.Name())) {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	for len(names) > keepBackups {
		os.RemoveAll(filepath.Join(BackupDir(), names[0]))
		names = names[1:]
	}
}

// holdsSkills is whether a backup has skill folders in it: <when>/skills,
// or <when>/<agent>/skills.
func holdsSkills(dir string) bool {
	for _, pat := range []string{"skills", "*/skills"} {
		if m, _ := filepath.Glob(filepath.Join(dir, pat)); len(m) > 0 {
			return true
		}
	}
	return false
}

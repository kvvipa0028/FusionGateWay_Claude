package library

import (
	"bytes"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Bundle is the library as a backup carries it to another computer: the
// instruction sets and the one on, what each agent gets besides, the MCP
// servers and the skills with their files. What magpie wrote into each
// agent (Applied) is this computer's own and stays out. It is made the
// same way every time — no times, everything in order — so two computers
// with the same library carry the same bundle.
type Bundle struct {
	Instructions Instructions      `json:"instructions"`
	Texts        map[string]string `json:"texts,omitempty"` // set → its text
	Extra        map[string]string `json:"extra,omitempty"` // agent → what it gets besides
	MCP          []*Server         `json:"mcp"`
	Skills       []*CarriedSkill   `json:"skills"`
	Icons        map[string]string `json:"icons,omitempty"`
}

// CarriedSkill is a skill with its folder's files.
type CarriedSkill struct {
	Name   string            `json:"name"`
	Source *Source           `json:"source,omitempty"`
	Agents []string          `json:"agents"`
	Files  map[string][]byte `json:"files"`          // by path in the folder, with /
	Exec   []string          `json:"exec,omitempty"` // the files that run
	Left   []string          `json:"left,omitempty"` // files too big to carry
}

// A skill's file bigger than maxCarried is left out, and so is every file
// once the skills come to maxCarriedAll: a backup is for text, not for
// what a skill downloads beside it.
const (
	maxCarried    = 2 << 20
	maxCarriedAll = 32 << 20
)

// Collect is the library as it is now; nil when there is none yet.
func Collect() (*Bundle, error) {
	mu.Lock()
	defer mu.Unlock()
	if _, err := os.Stat(path()); err != nil {
		return nil, nil
	}
	l, err := load()
	if err != nil {
		return nil, err
	}
	b := &Bundle{Instructions: l.Instructions, Texts: map[string]string{}, Extra: extras(), MCP: []*Server{}, Skills: []*CarriedSkill{}, Icons: l.Icons}
	b.Instructions.Agents = orNone(b.Instructions.Agents)
	for _, s := range l.sets() {
		if text := readText(setPath(s.ID)); text != "" {
			b.Texts[s.ID] = text
		}
	}
	if len(b.Texts) == 0 {
		b.Texts = nil
	}
	if len(b.Extra) == 0 {
		b.Extra = nil
	}
	for _, s := range sorted(l.MCP, func(s *Server) string { return s.Name }) {
		c := *s
		c.Agents = orNone(slices.Sorted(slices.Values(s.Agents)))
		b.MCP = append(b.MCP, &c)
	}
	budget := maxCarriedAll
	for _, s := range sorted(l.Skills, func(s *Skill) string { return s.Name }) {
		c := readSkill(realDir(skillDir(s.Name)), &budget)
		c.Name, c.Source, c.Agents = s.Name, s.Source, orNone(slices.Sorted(slices.Values(s.Agents)))
		b.Skills = append(b.Skills, c)
	}
	return b, nil
}

func sorted[T any](xs []T, key func(T) string) []T {
	return slices.SortedFunc(slices.Values(xs), func(a, b T) int { return strings.Compare(key(a), key(b)) })
}

func orNone(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}

// readSkill is a skill folder's files, as WalkDir gives them: in order.
// Links in it, .git and what isn't a plain file stay out.
func readSkill(dir string, budget *int) *CarriedSkill {
	c := &CarriedSkill{Files: map[string][]byte{}}
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != dir && (d.Name() == ".git" || d.Name() == marker) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || d.Name() == marker {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		if fi.Size() > maxCarried || int(fi.Size()) > *budget {
			c.Left = append(c.Left, rel)
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		*budget -= len(data)
		c.Files[rel] = data
		if fi.Mode().Perm()&0o111 != 0 {
			c.Exec = append(c.Exec, rel)
		}
		return nil
	})
	slices.Sort(c.Exec) // WalkDir's order isn't the order of the paths
	slices.Sort(c.Left)
	return c
}

// check refuses a bundle that would write outside the library: a name
// that isn't one, a file's path that climbs out of its skill.
func (b *Bundle) check() error {
	for _, s := range b.Instructions.Sets {
		if err := checkName("set", s.ID); err != nil {
			return err
		}
	}
	for id := range b.Texts {
		if id != defaultSet && !slices.ContainsFunc(b.Instructions.Sets, func(s InstrSet) bool { return s.ID == id }) {
			return fmt.Errorf("a text for no set, %q", id)
		}
	}
	for id := range b.Extra {
		if err := checkAgent(id); err != nil {
			return err
		}
	}
	for _, s := range b.MCP {
		if s == nil {
			return fmt.Errorf("an empty server")
		}
		if err := s.check(); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for _, s := range b.Skills {
		if s == nil {
			return fmt.Errorf("an empty skill")
		}
		if err := checkName("skill", s.Name); err != nil {
			return err
		}
		if seen[s.Name] {
			return fmt.Errorf("the skill %s twice", s.Name)
		}
		seen[s.Name] = true
		for rel := range s.Files {
			if !localFile(rel) {
				return fmt.Errorf("skill %s: %q is not a file in its folder", s.Name, rel)
			}
		}
	}
	return nil
}

// localFile reports whether rel, with /, names a file inside a folder:
// not absolute, no .., no \ to be read as a separator elsewhere.
func localFile(rel string) bool {
	return rel != "" && !strings.ContainsAny(rel, "\\:\x00") && filepath.IsLocal(filepath.FromSlash(rel)) &&
		!slices.Contains(strings.Split(rel, "/"), "..")
}

// WithoutSecrets empties the values of the servers' environment variables
// and headers that secret says hold one; their names stay, to be filled in.
func (b *Bundle) WithoutSecrets(secret func(string) bool) {
	for i, s := range b.MCP {
		c := *s
		c.Env, c.Headers = blank(s.Env, secret), blank(s.Headers, secret)
		b.MCP[i] = &c
	}
}

func blank(m map[string]string, secret func(string) bool) map[string]string {
	if m == nil {
		return nil
	}
	out := map[string]string{}
	for k, v := range m {
		if secret(k) {
			v = ""
		}
		out[k] = v
	}
	return out
}

// WithSecrets is b with the values WithoutSecrets emptied filled from the
// servers of the same name in have, so a bundle made without keys doesn't
// take away the ones kept here.
func (b *Bundle) WithSecrets(have *Bundle, secret func(string) bool) *Bundle {
	if have == nil {
		return b
	}
	out := *b
	out.MCP = nil
	for _, s := range b.MCP {
		c := *s
		i := slices.IndexFunc(have.MCP, func(h *Server) bool { return h.Name == s.Name })
		if i >= 0 {
			c.Env, c.Headers = filled(s.Env, have.MCP[i].Env, secret), filled(s.Headers, have.MCP[i].Headers, secret)
		}
		out.MCP = append(out.MCP, &c)
	}
	return &out
}

func filled(m, have map[string]string, secret func(string) bool) map[string]string {
	if m == nil {
		return nil
	}
	out := maps.Clone(m)
	for k, v := range m {
		if v == "" && secret(k) && have[k] != "" {
			out[k] = have[k]
		}
	}
	return out
}

// Put makes the library what b says — its instructions, servers and
// skills in place of the ones here — and writes it into the agents, which
// keep whatever of theirs isn't magpie's. The skills are written aside
// first, and only once all of them are there does any go in; a skill
// replaced or taken out is kept with the backups, and so is an
// instructions file that changes. A skill that comes linked to a folder
// this computer has too is linked to it; else it comes as its files. A
// skill whose files are as they are here is left alone.
func Put(b *Bundle) (*Result, error) {
	if err := b.check(); err != nil {
		return nil, err
	}
	return change(func(l *Library) error {
		if err := os.MkdirAll(skillsDir(), 0o755); err != nil {
			return err
		}
		stage, err := os.MkdirTemp(Dir(), ".incoming-") // beside skills, to be renamed in
		if err != nil {
			return err
		}
		defer os.RemoveAll(stage)

		keep, link := map[string]bool{}, map[string]string{}
		for _, s := range b.Skills {
			if cur := l.skill(s.Name); cur != nil && sameSource(cur.Source, s.Source) {
				budget := maxCarriedAll
				if have := readSkill(realDir(skillDir(s.Name)), &budget); sameFiles(have, s) {
					keep[s.Name] = true
					continue
				}
			}
			if src := s.Source; src != nil && src.Kind == "folder" && src.Dir != "" && isDir(src.Dir) {
				if to, err := os.Readlink(skillDir(s.Name)); err == nil && filepath.Clean(to) == filepath.Clean(src.Dir) {
					keep[s.Name] = true
				} else {
					link[s.Name] = src.Dir
				}
				continue
			}
			if err := writeSkill(filepath.Join(stage, s.Name), s); err != nil {
				return fmt.Errorf("skill %s: %w", s.Name, err)
			}
		}

		// every skill's files are ready: out with the old ones
		aside := filepath.Join(BackupDir(), time.Now().Format("2006-01-02_15-04-05.000"), "skills")
		out := func(name string) error {
			p := skillDir(name)
			fi, err := os.Lstat(p)
			if err != nil {
				return nil
			}
			if fi.Mode()&fs.ModeSymlink != 0 {
				return os.Remove(p) // a folder of the user's: only the link goes
			}
			if err := os.MkdirAll(aside, 0o700); err != nil {
				return err
			}
			return move(p, filepath.Join(aside, name))
		}
		for _, s := range l.Skills {
			if !keep[s.Name] {
				if err := out(s.Name); err != nil {
					return err
				}
			}
		}
		skills := []*Skill{}
		for _, s := range b.Skills {
			skills = append(skills, &Skill{Name: s.Name, Source: s.Source, Agents: slices.Clone(orNone(s.Agents))})
			if keep[s.Name] {
				continue
			}
			if err := out(s.Name); err != nil { // one of no skill's, in the way
				return err
			}
			if dir, ok := link[s.Name]; ok {
				if err := os.Symlink(dir, skillDir(s.Name)); err != nil {
					if err := copyDir(dir, skillDir(s.Name)); err != nil {
						return err
					}
				}
				continue
			}
			if err := os.Rename(filepath.Join(stage, s.Name), skillDir(s.Name)); err != nil {
				return err
			}
		}
		l.Skills = skills

		// the instructions: every text here that b hasn't goes, kept first
		kept := newBackups()
		l.kept = kept
		texts := map[string]string{}
		for _, s := range l.sets() {
			texts[setPath(s.ID)] = ""
		}
		for id := range extras() {
			texts[extraPath(id)] = ""
		}
		for id, text := range b.Texts {
			texts[setPath(id)] = text
		}
		for id, text := range b.Extra {
			texts[extraPath(id)] = text
		}
		for _, p := range slices.Sorted(maps.Keys(texts)) {
			if readText(p) == strings.TrimSpace(texts[p]) {
				continue
			}
			if err := kept.keep("library", p); err != nil {
				return err
			}
			if err := writeText(p, texts[p]); err != nil {
				return err
			}
		}
		l.Instructions = Instructions{Agents: slices.Sorted(slices.Values(orNone(b.Instructions.Agents))),
			Sets: slices.Clone(b.Instructions.Sets), Active: b.Instructions.Active}

		l.MCP = nil
		for _, s := range b.MCP {
			c := *s
			c.Agents = slices.Sorted(slices.Values(orNone(s.Agents)))
			l.MCP = append(l.MCP, &c)
		}
		l.Icons = maps.Clone(b.Icons)
		return nil
	})
}

func sameSource(a, b *Source) bool {
	return (a == nil) == (b == nil) && (a == nil || *a == *b)
}

// sameFiles reports whether a folder here has what c carries.
func sameFiles(have, c *CarriedSkill) bool {
	return maps.EqualFunc(have.Files, c.Files, bytes.Equal) && slices.Equal(have.Exec, sorted(c.Exec, func(s string) string { return s }))
}

// writeSkill writes a carried skill's files into a new folder.
func writeSkill(dir string, s *CarriedSkill) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, rel := range slices.Sorted(maps.Keys(s.Files)) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		mode := fs.FileMode(0o644)
		if slices.Contains(s.Exec, rel) {
			mode = 0o755
		}
		if err := os.WriteFile(p, s.Files[rel], mode); err != nil {
			return err
		}
	}
	return nil
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// Changed is when the library was last changed: library.json, or a file
// of its own (links not followed).
func Changed() time.Time {
	var t time.Time
	if fi, err := os.Stat(path()); err == nil {
		t = fi.ModTime()
	}
	filepath.WalkDir(Dir(), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if fi, err := d.Info(); err == nil && fi.ModTime().After(t) {
			t = fi.ModTime()
		}
		return nil
	})
	return t
}

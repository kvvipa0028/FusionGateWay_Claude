package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

type PresetInput struct {
	Name  string          `json:"name"`
	Layer stageplan.Layer `json:"layer"`
}
type Preset struct {
	ProjectID string          `json:"project_id"`
	ID        string          `json:"id"`
	Revision  int64           `json:"revision"`
	Name      string          `json:"name"`
	Layer     stageplan.Layer `json:"layer"`
	Hash      string          `json:"hash"`
	CreatedAt time.Time       `json:"created_at"`
}
type PresetRef struct {
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
	Hash     string `json:"hash"`
}
type PresetReceipt struct {
	Preset  Preset `json:"preset"`
	Created bool   `json:"created"`
}

var ErrPresetCapacity = errors.New("preset capacity reached")

func presetID(v string) bool {
	return opaque(v) && utf8.ValidString(v) && !strings.ContainsAny(v, "/\\")
}
func canonicalPreset(in PresetInput) (PresetInput, error) {
	if !utf8.ValidString(in.Name) || strings.TrimSpace(in.Name) == "" || len(in.Name) > 256 || strings.ContainsFunc(in.Name, unicode.IsControl) {
		return PresetInput{}, ErrInvalid
	}
	l, e := stageplan.ExpandLayer(in.Layer)
	if e != nil {
		return PresetInput{}, ErrInvalid
	}
	if l.Roles == nil {
		l.Roles = map[stageplan.Role]stageplan.Binding{}
	}
	for _, r := range stageplan.AllRoles() {
		if _, ok := l.Roles[r]; !ok {
			l.Roles[r] = stageplan.Binding{Mode: stageplan.Inherit}
		}
	}
	return PresetInput{Name: in.Name, Layer: l}, nil
}
func presetHash(project, id string, revision int64, in PresetInput) string {
	raw, _ := json.Marshal(struct {
		ProjectID string      `json:"project_id"`
		ID        string      `json:"id"`
		Revision  int64       `json:"revision"`
		Input     PresetInput `json:"input"`
	}{project, id, revision, in})
	return hash(raw)
}
func presetIn(q queryRow, project, id string, revision int64) (Preset, error) {
	if revision == 0 {
		e := q.QueryRow("SELECT revision FROM preset_heads WHERE project_id=? AND id=?", project, id).Scan(&revision)
		if errors.Is(e, sql.ErrNoRows) {
			return Preset{}, ErrNotFound
		}
		if e != nil {
			return Preset{}, e
		}
	}
	p := Preset{ProjectID: project, ID: id, Revision: revision}
	var raw string
	var created int64
	e := q.QueryRow("SELECT name,layer_json,hash,created_at FROM preset_revisions WHERE project_id=? AND id=? AND revision=?", project, id, revision).Scan(&p.Name, &raw, &p.Hash, &created)
	if errors.Is(e, sql.ErrNoRows) {
		return Preset{}, ErrNotFound
	}
	if e != nil {
		return Preset{}, e
	}
	if json.Unmarshal([]byte(raw), &p.Layer) != nil {
		return Preset{}, ErrInvalid
	}
	in, e := canonicalPreset(PresetInput{Name: p.Name, Layer: p.Layer})
	if e != nil || p.Revision < 1 || presetHash(project, id, revision, in) != p.Hash {
		return Preset{}, ErrInvalid
	}
	p.Layer = in.Layer
	p.CreatedAt = time.UnixMilli(created).UTC()
	return p, nil
}

// SavePreset commits the immutable revision and latest head together. The
// expected base plus canonical payload is an idempotent PUT identity; a retry
// reads that exact historical version even after the latest head moves on.
func (s *Store) SavePreset(project, id string, base int64, in PresetInput) (PresetReceipt, error) {
	if !presetID(project) || !presetID(id) || base < 0 || base == int64(1<<63-1) {
		return PresetReceipt{}, ErrInvalid
	}
	in, e := canonicalPreset(in)
	if e != nil {
		return PresetReceipt{}, e
	}
	revision := base + 1
	h := presetHash(project, id, revision, in)
	var out PresetReceipt
	e = s.transaction(func(tx *sql.Tx) error {
		old, e := presetIn(tx, project, id, revision)
		if e == nil {
			if old.Hash != h {
				return ErrConflict
			}
			out.Preset = old
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		var latest int64
		e = tx.QueryRow("SELECT revision FROM preset_heads WHERE project_id=? AND id=?", project, id).Scan(&latest)
		if errors.Is(e, sql.ErrNoRows) {
			latest = 0
		} else if e != nil {
			return e
		}
		if latest != base {
			return ErrConflict
		}
		if latest == 0 {
			var count int
			if e = tx.QueryRow("SELECT COUNT(*) FROM preset_heads WHERE project_id=?", project).Scan(&count); e != nil {
				return e
			}
			if count >= 128 {
				return ErrPresetCapacity
			}
		}
		created := s.now().UTC().Truncate(time.Millisecond)
		raw, _ := json.Marshal(in.Layer)
		if _, e = tx.Exec("INSERT INTO preset_revisions VALUES(?,?,?,?,?,?,?)", project, id, revision, in.Name, string(raw), h, created.UnixMilli()); e != nil {
			return e
		}
		if latest == 0 {
			_, e = tx.Exec("INSERT INTO preset_heads VALUES(?,?,?)", project, id, revision)
		} else {
			_, e = tx.Exec("UPDATE preset_heads SET revision=? WHERE project_id=? AND id=? AND revision=?", revision, project, id, base)
		}
		if e != nil {
			return e
		}
		out = PresetReceipt{Preset: Preset{ProjectID: project, ID: id, Revision: revision, Name: in.Name, Layer: in.Layer, Hash: h, CreatedAt: created}, Created: true}
		return nil
	})
	return out, e
}

// revision 0 reads the current head for browsing, never for task application.
func (s *Store) Preset(project, id string, revision int64) (Preset, error) {
	if !presetID(project) || !presetID(id) || revision < 0 {
		return Preset{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return Preset{}, ErrClosed
	}
	return presetIn(s.db, project, id, revision)
}
func (s *Store) Presets(project string) ([]Preset, error) {
	if !presetID(project) {
		return nil, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, ErrClosed
	}
	rows, e := s.db.Query("SELECT id FROM preset_heads WHERE project_id=? ORDER BY id LIMIT 128", project)
	if e != nil {
		return nil, e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := []Preset{}
	for _, id := range ids {
		p, e := presetIn(s.db, project, id, 0)
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, nil
}
func (s *Store) TaskPreset(taskID string) (*PresetRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, ErrClosed
	}
	t, e := taskIn(s.db, taskID)
	if e != nil {
		return nil, e
	}
	var ref PresetRef
	e = s.db.QueryRow("SELECT id,revision,hash FROM task_preset_refs WHERE task_id=?", taskID).Scan(&ref.ID, &ref.Revision, &ref.Hash)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	p, e := presetIn(s.db, t.ProjectID, ref.ID, ref.Revision)
	if e != nil || p.Hash != ref.Hash {
		return nil, ErrInvalid
	}
	return &ref, nil
}

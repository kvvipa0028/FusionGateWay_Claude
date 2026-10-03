package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

type DefaultLayer struct {
	Scope     string          `json:"scope"`
	ID        string          `json:"id"`
	Revision  int64           `json:"revision"`
	Layer     stageplan.Layer `json:"layer"`
	Hash      string          `json:"hash"`
	CreatedAt time.Time       `json:"created_at"`
}
type DefaultLayerReceipt struct {
	DefaultLayer DefaultLayer `json:"default_layer"`
	Created      bool         `json:"created"`
}
type DefaultLayers struct{ Global, Project *DefaultLayer }

// DefaultStamp is an internal optimistic condition, not a task payload field.
// Empty hashes mean there was no saved layer, not an unconstrained wildcard.
type DefaultStamp struct{ GlobalHash, ProjectHash string }

func (d DefaultLayers) Stamp() (s DefaultStamp) {
	if d.Global != nil {
		s.GlobalHash = d.Global.Hash
	}
	if d.Project != nil {
		s.ProjectHash = d.Project.Hash
	}
	return
}

var ErrDefaultsChanged = errors.New("default layers changed")
var ErrDefaultsCapacity = errors.New("default layers capacity reached")

func validDefaultSubject(scope, id string) bool {
	return scope == "global" && id == "global" || scope == "project" && presetID(id)
}
func canonicalDefaults(in stageplan.Layer) (stageplan.Layer, error) {
	l, e := stageplan.ExpandLayer(in)
	if e != nil {
		return stageplan.Layer{}, ErrInvalid
	}
	if l.Roles == nil {
		l.Roles = map[stageplan.Role]stageplan.Binding{}
	}
	for _, role := range stageplan.AllRoles() {
		if _, ok := l.Roles[role]; !ok {
			l.Roles[role] = stageplan.Binding{Mode: stageplan.Inherit}
		}
	}
	l, e = stageplan.ExpandLayer(l)
	if e != nil {
		return stageplan.Layer{}, ErrInvalid
	}
	return l, nil
}
func defaultHash(scope, id string, revision int64, layer stageplan.Layer) string {
	raw, _ := json.Marshal(struct {
		Scope    string          `json:"scope"`
		ID       string          `json:"id"`
		Revision int64           `json:"revision"`
		Layer    stageplan.Layer `json:"layer"`
	}{scope, id, revision, layer})
	return hash(raw)
}
func defaultIn(q queryRow, scope, id string, revision int64) (DefaultLayer, error) {
	if revision == 0 {
		e := q.QueryRow("SELECT revision FROM default_layer_heads WHERE scope=? AND id=?", scope, id).Scan(&revision)
		if errors.Is(e, sql.ErrNoRows) {
			return DefaultLayer{}, ErrNotFound
		}
		if e != nil {
			return DefaultLayer{}, e
		}
	}
	d := DefaultLayer{Scope: scope, ID: id, Revision: revision}
	var raw string
	var created int64
	e := q.QueryRow("SELECT layer_json,hash,created_at FROM default_layer_revisions WHERE scope=? AND id=? AND revision=?", scope, id, revision).Scan(&raw, &d.Hash, &created)
	if errors.Is(e, sql.ErrNoRows) {
		return DefaultLayer{}, ErrNotFound
	}
	if e != nil {
		return DefaultLayer{}, e
	}
	if json.Unmarshal([]byte(raw), &d.Layer) != nil {
		return DefaultLayer{}, ErrInvalid
	}
	l, e := canonicalDefaults(d.Layer)
	if e != nil || revision < 1 || d.Hash != defaultHash(scope, id, revision, l) {
		return DefaultLayer{}, ErrInvalid
	}
	d.Layer = l
	d.CreatedAt = time.UnixMilli(created).UTC()
	return d, nil
}
func defaultsIn(q queryRow, project string) (DefaultLayers, error) {
	var out DefaultLayers
	g, e := defaultIn(q, "global", "global", 0)
	if e == nil {
		out.Global = &g
	} else if !errors.Is(e, ErrNotFound) {
		return out, e
	}
	p, e := defaultIn(q, "project", project, 0)
	if e == nil {
		out.Project = &p
	} else if !errors.Is(e, ErrNotFound) {
		return out, e
	}
	return out, nil
}
func checkDefaults(q queryRow, project string, expected *DefaultStamp) error {
	if expected == nil {
		return nil
	}
	d, e := defaultsIn(q, project)
	if e != nil {
		return e
	}
	if d.Stamp() != *expected {
		return ErrDefaultsChanged
	}
	return nil
}
func (s *Store) DefaultLayers(project string) (DefaultLayers, error) {
	if !presetID(project) {
		return DefaultLayers{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return DefaultLayers{}, ErrClosed
	}
	return defaultsIn(s.db, project)
}
func (s *Store) DefaultLayer(scope, id string, revision int64) (DefaultLayer, error) {
	if !validDefaultSubject(scope, id) || revision < 0 {
		return DefaultLayer{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return DefaultLayer{}, ErrClosed
	}
	return defaultIn(s.db, scope, id, revision)
}
func (s *Store) SaveDefaultLayer(scope, id string, base int64, in stageplan.Layer) (DefaultLayerReceipt, error) {
	if !validDefaultSubject(scope, id) || base < 0 || base == int64(1<<63-1) {
		return DefaultLayerReceipt{}, ErrInvalid
	}
	l, e := canonicalDefaults(in)
	if e != nil {
		return DefaultLayerReceipt{}, e
	}
	revision := base + 1
	h := defaultHash(scope, id, revision, l)
	var out DefaultLayerReceipt
	e = s.transaction(func(tx *sql.Tx) error {
		old, e := defaultIn(tx, scope, id, revision)
		if e == nil {
			if old.Hash != h {
				return ErrConflict
			}
			out.DefaultLayer = old
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		var latest int64
		e = tx.QueryRow("SELECT revision FROM default_layer_heads WHERE scope=? AND id=?", scope, id).Scan(&latest)
		if errors.Is(e, sql.ErrNoRows) {
			latest = 0
		} else if e != nil {
			return e
		}
		if latest != base {
			return ErrConflict
		}
		if latest == 0 && scope == "project" {
			var count int
			if e = tx.QueryRow("SELECT COUNT(*) FROM default_layer_heads WHERE scope='project'").Scan(&count); e != nil {
				return e
			}
			if count >= 128 {
				return ErrDefaultsCapacity
			}
		}
		created := s.now().UTC().Truncate(time.Millisecond)
		raw, _ := json.Marshal(l)
		if _, e = tx.Exec("INSERT INTO default_layer_revisions VALUES(?,?,?,?,?,?)", scope, id, revision, string(raw), h, created.UnixMilli()); e != nil {
			return e
		}
		if latest == 0 {
			_, e = tx.Exec("INSERT INTO default_layer_heads VALUES(?,?,?)", scope, id, revision)
		} else {
			var r sql.Result
			r, e = tx.Exec("UPDATE default_layer_heads SET revision=? WHERE scope=? AND id=? AND revision=?", revision, scope, id, base)
			if e == nil {
				var n int64
				n, e = r.RowsAffected()
				if e == nil && n != 1 {
					return ErrConflict
				}
			}
		}
		if e != nil {
			return e
		}
		out = DefaultLayerReceipt{DefaultLayer: DefaultLayer{Scope: scope, ID: id, Revision: revision, Layer: l, Hash: h, CreatedAt: created}, Created: true}
		return nil
	})
	return out, e
}

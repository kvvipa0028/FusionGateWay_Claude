// Package bootstrap reads operator-owned local registration for product startup.
// Declarations never grant route admission or verify upstream account identity.
package bootstrap

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/fusion/api"
	"github.com/yetone/magpie/internal/fusion/routes"
	"github.com/yetone/magpie/internal/fusion/stageplan"
)

const (
	MaxSourceBytes = 256 << 10
	MaxProjects    = 32
	MaxRoutes      = 64
)

var ErrRegistration = errors.New("local Fusion project registration unavailable")

type Document struct {
	SchemaVersion int                  `json:"schema_version"`
	Revision      int64                `json:"revision"`
	Global        stageplan.Layer      `json:"global"`
	Routes        []RouteDeclaration   `json:"routes"`
	Projects      []ProjectDeclaration `json:"projects"`
}
type RouteDeclaration struct {
	ID                 string   `json:"id"`
	Revision           int64    `json:"revision"`
	NativeRoute        string   `json:"native_route"`
	Model              string   `json:"model"`
	Account            string   `json:"account"`
	Workspace          string   `json:"workspace"`
	CredentialIdentity string   `json:"credential_identity"`
	RuntimeVersion     string   `json:"runtime_version"`
	Efforts            []string `json:"efforts,omitempty"`
	DefaultEffort      *string  `json:"default_effort,omitempty"`
	NoEffort           bool     `json:"no_effort"`
}
type ProjectDeclaration struct {
	ID           string               `json:"id"`
	Name         string               `json:"name"`
	Path         string               `json:"path"`
	Read         bool                 `json:"read"`
	Write        bool                 `json:"write"`
	Routes       []stageplan.RouteRef `json:"routes"`
	Layer        stageplan.Layer      `json:"layer"`
	MaxCalls     *int                 `json:"max_calls,omitempty"`
	MaxReworks   *int                 `json:"max_reworks,omitempty"`
	Independence []stageplan.RolePair `json:"independence,omitempty"`
}

// Project is an internal copied registration. Path/grants must not be accepted
// from task HTTP bodies; real execution still needs independent admission.
type Project struct {
	ID, Name, Path string
	Read, Write    bool
	Configuration  api.ProjectConfiguration
}
type fileIdentity struct{ device, inode uint64 }
type sourceRecord struct {
	fileIdentity
	raw []byte
}
type registeredProject struct {
	project Project
	folder  fileIdentity
}
type Loaded struct {
	path         string
	identity     fileIdentity
	digest       [32]byte
	revision     int64
	projects     map[string]registeredProject
	nativeRoutes map[stageplan.RouteRef]string
}

func (*Loaded) String() string   { return "Fusion local registration (redacted)" }
func (*Loaded) GoString() string { return "Loaded(<redacted>)" }
func (s *Loaded) Revision() int64 {
	if s == nil {
		return 0
	}
	return s.revision
}
func opaque(s string) bool {
	return s != "" && len(s) <= 256 && utf8.ValidString(s) && !strings.ContainsAny(s, "/\\") && !strings.ContainsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}
func declared(s string) bool {
	return opaque(s) && s != "unknown" && s != "undisclosed" && s != "default"
}
func overlaps(a, b string) bool {
	rel, e := filepath.Rel(a, b)
	return e == nil && (rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func Load(path string) (*Loaded, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) != "projects.json" || strings.ContainsRune(path, 0) {
		return nil, ErrRegistration
	}
	record, e := readSource(path)
	if e != nil {
		return nil, ErrRegistration
	}
	var d Document
	if !utf8.Valid(record.raw) || uniqueJSON(record.raw) != nil {
		return nil, ErrRegistration
	}
	dec := json.NewDecoder(bytes.NewReader(record.raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&d) != nil || d.SchemaVersion != 1 || d.Revision < 1 || len(d.Projects) < 1 || len(d.Projects) > MaxProjects || len(d.Routes) > MaxRoutes {
		return nil, ErrRegistration
	}
	declarations := map[stageplan.RouteRef]stageplan.Route{}
	for _, entry := range d.Routes {
		if !declared(entry.ID) || entry.Revision < 1 || !declared(entry.Model) || !declared(entry.Account) || !declared(entry.Workspace) || !declared(entry.CredentialIdentity) || !declared(entry.RuntimeVersion) || len(entry.Efforts) > 16 {
			return nil, ErrRegistration
		}
		var native *routes.Candidate
		for _, c := range routes.Builtins() {
			if entry.NativeRoute == c.ID {
				v := c
				native = &v
				break
			}
		}
		if native == nil {
			return nil, ErrRegistration
		}
		ref := stageplan.RouteRef{ID: entry.ID, Revision: entry.Revision}
		if _, exists := declarations[ref]; exists {
			return nil, ErrRegistration
		}
		efforts := map[string]bool{}
		for _, effort := range entry.Efforts {
			if !opaque(effort) || efforts[effort] {
				return nil, ErrRegistration
			}
			efforts[effort] = true
		}
		if entry.NoEffort && (len(entry.Efforts) != 0 || entry.DefaultEffort != nil) || entry.DefaultEffort != nil && !efforts[*entry.DefaultEffort] {
			return nil, ErrRegistration
		}
		declarations[ref] = stageplan.Route{ID: entry.ID, Revision: entry.Revision, Model: entry.Model, Account: entry.Account, Workspace: entry.Workspace, CredentialIdentity: entry.CredentialIdentity, RuntimeVersion: entry.RuntimeVersion, BillingPath: native.Billing, Efforts: entry.Efforts, DefaultEffort: entry.DefaultEffort, NoEffort: entry.NoEffort, LockEnforcement: stageplan.Unverified}
	}
	s := &Loaded{path: path, identity: record.fileIdentity, digest: sha256.Sum256(record.raw), revision: d.Revision, projects: map[string]registeredProject{}, nativeRoutes: map[stageplan.RouteRef]string{}}
	for _, entry := range d.Routes {
		s.nativeRoutes[stageplan.RouteRef{ID: entry.ID, Revision: entry.Revision}] = entry.NativeRoute
	}
	for _, entry := range d.Projects {
		if !opaque(entry.ID) || !utf8.ValidString(entry.Name) || strings.TrimSpace(entry.Name) == "" || len(entry.Name) > 256 || strings.ContainsFunc(entry.Name, unicode.IsControl) || !entry.Read || len(entry.Routes) > MaxRoutes {
			return nil, ErrRegistration
		}
		if _, exists := s.projects[entry.ID]; exists {
			return nil, ErrRegistration
		}
		if !filepath.IsAbs(entry.Path) || filepath.Clean(entry.Path) != entry.Path || strings.ContainsRune(entry.Path, 0) || overlaps(entry.Path, filepath.Dir(path)) || overlaps(filepath.Dir(path), entry.Path) {
			return nil, ErrRegistration
		}
		folder, e := readFolder(entry.Path)
		if e != nil {
			return nil, ErrRegistration
		}
		for _, old := range s.projects {
			if old.folder == folder || overlaps(old.project.Path, entry.Path) || overlaps(entry.Path, old.project.Path) {
				return nil, ErrRegistration
			}
		}
		var available []stageplan.Route
		seen := map[stageplan.RouteRef]stageplan.Route{}
		for _, ref := range entry.Routes {
			v, exists := declarations[ref]
			if _, duplicate := seen[ref]; !exists || duplicate {
				return nil, ErrRegistration
			}
			seen[ref] = v
			available = append(available, v)
		}
		global, e := registrationLayer(d.Global, seen)
		if e != nil {
			return nil, e
		}
		layer, e := registrationLayer(entry.Layer, seen)
		if e != nil {
			return nil, e
		}
		calls := 50
		if entry.MaxCalls != nil {
			calls = *entry.MaxCalls
		}
		reworks := 1
		if entry.MaxReworks != nil {
			reworks = *entry.MaxReworks
		}
		if calls < 1 || calls > 1000 || reworks < 0 || reworks > 1 {
			return nil, ErrRegistration
		}
		pairs, e := stageplan.CanonicalIndependence(entry.Independence)
		if e != nil {
			return nil, ErrRegistration
		}
		p := Project{ID: entry.ID, Name: entry.Name, Path: entry.Path, Read: entry.Read, Write: entry.Write, Configuration: api.ProjectConfiguration{Global: global, Project: layer, Routes: available, DefaultBudget: &api.BudgetLimits{MaxCalls: calls, MaxReworks: reworks}, Independence: pairs}}
		s.projects[entry.ID] = registeredProject{project: p, folder: folder}
	}
	return s, nil
}
func registrationLayer(layer stageplan.Layer, available map[stageplan.RouteRef]stageplan.Route) (stageplan.Layer, error) {
	l, e := stageplan.ExpandLayer(layer)
	if e != nil {
		return stageplan.Layer{}, ErrRegistration
	}
	if l.Roles == nil {
		l.Roles = map[stageplan.Role]stageplan.Binding{}
	}
	match := func(ref stageplan.RouteRef, model string) bool {
		r, ok := available[ref]
		return ok && r.Model == model
	}
	for _, b := range l.Roles {
		if b.Mode == stageplan.Locked && !match(*b.Route, b.Model) {
			return stageplan.Layer{}, ErrRegistration
		}
		for _, c := range b.Candidates {
			if !match(c.Route, c.Model) {
				return stageplan.Layer{}, ErrRegistration
			}
		}
	}
	for _, r := range stageplan.AllRoles() {
		if _, ok := l.Roles[r]; !ok {
			l.Roles[r] = stageplan.Binding{Mode: stageplan.Inherit}
		}
	}
	return l, nil
}
func (s *Loaded) Project(id string) (Project, error) {
	if s == nil {
		return Project{}, ErrRegistration
	}
	v, ok := s.projects[id]
	if !ok {
		return Project{}, ErrRegistration
	}
	raw, _ := json.Marshal(v.project)
	var out Project
	if json.Unmarshal(raw, &out) != nil {
		return Project{}, ErrRegistration
	}
	return out, nil
}
func (s *Loaded) Projects() []Project {
	if s == nil {
		return nil
	}
	ids := make([]string, 0, len(s.projects))
	for id := range s.projects {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Project, 0, len(ids))
	for _, id := range ids {
		v, _ := s.Project(id)
		out = append(out, v)
	}
	return out
}

// Current is a local authority check, not a quota query or upstream proof.
// Content/identity rotation and folder replacement require explicit reloading.
func (s *Loaded) Current(id string) bool {
	if s == nil {
		return false
	}
	p, ok := s.projects[id]
	if !ok {
		return false
	}
	r, e := readSource(s.path)
	if e != nil || r.fileIdentity != s.identity || sha256.Sum256(r.raw) != s.digest {
		return false
	}
	f, e := readFolder(p.project.Path)
	return e == nil && f == p.folder
}

// Reject duplicate/fold-alias object keys and trailing values before typed JSON.
func uniqueJSON(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	var value func(int) error
	value = func(depth int) error {
		if depth > 32 {
			return ErrRegistration
		}
		token, e := d.Token()
		if e != nil {
			return ErrRegistration
		}
		if delimiter, ok := token.(json.Delim); ok {
			switch delimiter {
			case '{':
				var keys []string
				for d.More() {
					k, e := d.Token()
					name, ok := k.(string)
					if e != nil || !ok || len(keys) >= 256 {
						return ErrRegistration
					}
					for _, old := range keys {
						if strings.EqualFold(old, name) {
							return ErrRegistration
						}
					}
					keys = append(keys, name)
					if e = value(depth + 1); e != nil {
						return e
					}
				}
				end, e := d.Token()
				if e != nil || end != json.Delim('}') {
					return ErrRegistration
				}
			case '[':
				for d.More() {
					if e = value(depth + 1); e != nil {
						return e
					}
				}
				end, e := d.Token()
				if e != nil || end != json.Delim(']') {
					return ErrRegistration
				}
			default:
				return ErrRegistration
			}
		}
		return nil
	}
	if e := value(0); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return ErrRegistration
	}
	return nil
}

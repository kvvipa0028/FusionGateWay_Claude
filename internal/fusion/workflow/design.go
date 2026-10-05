// Package workflow defines versioned finite playbooks and frozen design
// contracts. These values do not grant Runtime, network or publishing rights.
package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

var ErrInvalid = errors.New("invalid workflow contract")

type Kind string
type Definition struct {
	SchemaVersion int              `json:"schema_version"`
	Kind          Kind             `json:"kind"`
	RequiredRoles []stageplan.Role `json:"required_roles"`
	Hash          string           `json:"hash"`
}

func digest(namespace string, v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(append([]byte(namespace+"\x00"), b...))
	return hex.EncodeToString(h[:])
}
func DefinitionFor(kind Kind) (Definition, error) {
	d := Definition{SchemaVersion: 1, Kind: kind}
	switch kind {
	case "investigate":
		d.RequiredRoles = []stageplan.Role{stageplan.Design}
	case "review":
		d.RequiredRoles = []stageplan.Role{stageplan.Review}
	case "change", "bugfix":
		d.RequiredRoles = []stageplan.Role{stageplan.Design, stageplan.Implementation, stageplan.Testing, stageplan.Review, stageplan.Acceptance}
	default:
		return Definition{}, ErrInvalid
	}
	d.Hash = digest("fusion/workflow/1", d)
	return d, nil
}
func VerifyDefinition(d Definition) error {
	expected, e := DefinitionFor(d.Kind)
	if e != nil || !reflect.DeepEqual(d, expected) {
		return ErrInvalid
	}
	return nil
}

type DesignDocument struct {
	Goal        string   `json:"goal"`
	Scope       []string `json:"scope"`
	Constraints []string `json:"constraints"`
	Interfaces  []string `json:"interfaces"`
	Acceptance  []string `json:"acceptance"`
}
type DesignSnapshot struct {
	SchemaVersion  int            `json:"schema_version"`
	Document       DesignDocument `json:"document"`
	Hash           string         `json:"hash"`
	AcceptanceHash string         `json:"acceptance_hash"`
}

func text(v string) bool {
	return utf8.ValidString(v) && strings.TrimSpace(v) != "" && !strings.ContainsRune(v, 0)
}
func validDocument(d DesignDocument) bool {
	if !text(d.Goal) {
		return false
	}
	for _, items := range [][]string{d.Scope, d.Constraints, d.Interfaces, d.Acceptance} {
		if len(items) == 0 || len(items) > 128 {
			return false
		}
		for _, v := range items {
			if !text(v) {
				return false
			}
		}
	}
	seen := map[string]bool{}
	for _, scope := range d.Scope {
		if seen[scope] || path.IsAbs(scope) || path.Clean(scope) != scope || scope == ".." || strings.HasPrefix(scope, "../") || strings.ContainsAny(scope, "\\:") || strings.ContainsFunc(scope, unicode.IsControl) {
			return false
		}
		for _, part := range strings.Split(scope, "/") {
			if part == ".git" {
				return false
			}
		}
		seen[scope] = true
	}
	b, e := json.Marshal(d)
	return e == nil && len(b) <= 65536
}
func FreezeDesign(d DesignDocument) (DesignSnapshot, error) {
	if !validDocument(d) {
		return DesignSnapshot{}, ErrInvalid
	}
	d.Scope = append([]string(nil), d.Scope...)
	d.Constraints = append([]string(nil), d.Constraints...)
	d.Interfaces = append([]string(nil), d.Interfaces...)
	d.Acceptance = append([]string(nil), d.Acceptance...)
	return DesignSnapshot{SchemaVersion: 1, Document: d, Hash: digest("fusion/design/1", d), AcceptanceHash: digest("fusion/acceptance/1", d.Acceptance)}, nil
}
func VerifyDesign(d DesignSnapshot) error {
	expected, e := FreezeDesign(d.Document)
	if e != nil || !reflect.DeepEqual(d, expected) {
		return ErrInvalid
	}
	return nil
}

package handoff

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"

	"github.com/yetone/magpie/internal/fusion/workspace"
)

// Reference is stored only in the protected server Store. No HTTP importer or
// management DTO may treat a self-declared witness/hash as execution authority.
type Reference struct {
	Version       int                   `json:"version"`
	Binding       Binding               `json:"binding"`
	Path          string                `json:"path"`
	TreeHash      string                `json:"tree_hash"`
	BaseTreeHash  string                `json:"base_tree_hash"`
	StopProofHash string                `json:"stop_proof_hash"`
	HeaderHash    string                `json:"header_hash"`
	HeaderInfo    workspace.Fingerprint `json:"header_info"`
	Witness       json.RawMessage       `json:"witness"`
	WitnessHash   string                `json:"witness_hash"`
}

func (Reference) String() string   { return "durable handoff reference (redacted)" }
func (Reference) GoString() string { return "Reference(<redacted>)" }

func (h Bundle) Reference(stateRoot string) (Reference, error) {
	d, err := h.Read(h.binding)
	if err != nil {
		return Reference{}, err
	}
	w, err := h.artifact.ExportWitness(stateRoot)
	if err != nil {
		return Reference{}, ErrInvalid
	}
	fp, hash, err := h.artifact.HeaderFingerprint()
	if err != nil {
		return Reference{}, ErrInvalid
	}
	r := Reference{Version: 1, Binding: h.binding, Path: h.artifact.Path(), TreeHash: d.Artifact.TreeHash, BaseTreeHash: d.Artifact.BaseTreeHash, StopProofHash: d.Evidence.StopProofHash, HeaderHash: hash, HeaderInfo: fp, Witness: w, WitnessHash: Hash(w)}
	if _, err := h.Read(h.binding); err != nil {
		return Reference{}, err
	}
	return r, nil
}
func ValidReference(r Reference) bool {
	return r.Version == 1 && validBinding(r.Binding) && filepath.IsAbs(r.Path) && filepath.Clean(r.Path) == r.Path && digest(r.TreeHash) && digest(r.BaseTreeHash) && digest(r.StopProofHash) && digest(r.HeaderHash) && len(r.Witness) > 0 && len(r.Witness) <= 32<<20 && Hash(r.Witness) == r.WitnessHash
}

// Restore requires a reference fetched from the trusted Store's released-run
// query and independent current source/root registration. It does not authorize
// routes, Native execution, a role transition or engineering acceptance.
func Restore(r Reference, source, stateRoot string) (Bundle, error) {
	if !ValidReference(r) {
		return Bundle{}, ErrInvalid
	}
	a, err := workspace.RestoreWitness(r.Witness, r.WitnessHash, source, stateRoot)
	if err != nil || a.Path() != r.Path || a.Manifest().TreeHash != r.TreeHash || a.Manifest().BaseTreeHash != r.BaseTreeHash {
		return Bundle{}, ErrInvalid
	}
	fp, hash, err := a.HeaderFingerprint()
	if err != nil || !reflect.DeepEqual(fp, r.HeaderInfo) || hash != r.HeaderHash {
		return Bundle{}, ErrInvalid
	}
	i, err := os.Lstat(filepath.Join(a.Path(), "handoff.json"))
	if err != nil {
		return Bundle{}, ErrInvalid
	}
	h := Bundle{artifact: a, binding: r.Binding, info: i, hash: r.HeaderHash}
	d, err := h.Read(r.Binding)
	if err != nil || d.Evidence.StopProofHash != r.StopProofHash {
		return Bundle{}, ErrInvalid
	}
	return h, nil
}

func DerivedReference(child, parent Reference) bool {
	return ValidReference(child) && ValidReference(parent) && child.BaseTreeHash == parent.BaseTreeHash && workspace.WitnessDescends(child.Witness, parent.Witness)
}

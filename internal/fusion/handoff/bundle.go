// Package handoff transfers verified code between independent stage copies.
// A bundle is not route admission, a Native session or engineering acceptance.
package handoff

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/fusion/evidence"
	"github.com/yetone/magpie/internal/fusion/stageplan"
	"github.com/yetone/magpie/internal/fusion/workspace"
)

var ErrInvalid = errors.New("invalid stage handoff")

type Binding struct {
	TaskID       string         `json:"task_id"`
	PlanRevision int64          `json:"plan_revision"`
	PlanHash     string         `json:"plan_hash"`
	RunID        string         `json:"run_id"`
	Generation   int64          `json:"generation"`
	Role         stageplan.Role `json:"role"`
	TargetHash   string         `json:"target_hash"`
}
type Evidence struct {
	OutputHash    string `json:"output_hash"`
	StopProofHash string `json:"stop_proof_hash"`
}
type Document struct {
	Version int     `json:"version"`
	Binding Binding `json:"binding"`
	Task    struct {
		Goal string `json:"goal"`
	} `json:"task"`
	Artifact  workspace.ArtifactManifest `json:"artifact"`
	Decisions []string                   `json:"decisions"`
	Evidence  struct {
		Evidence
		Status        string `json:"status"`
		TestsExecuted bool   `json:"tests_executed"`
	} `json:"evidence"`
	Changes []workspace.Change `json:"changes"`
	Pending []string           `json:"pending"`
}

// Bundle retains both filesystem provenance and the exact published bytes.
// There is intentionally no unchecked JSON/path constructor or session field.
type Bundle struct {
	artifact workspace.FrozenArtifact
	binding  Binding
	info     os.FileInfo
	hash     string
}

func (Bundle) String() string   { return "stage handoff bundle (redacted)" }
func (Bundle) GoString() string { return "Bundle(<redacted>)" }

// Publish is trusted producer wiring. The caller must independently verify the
// actual stopped process and persisted Task/Plan/Run; hashes do not prove that.
// Model output remains advisory, never tests_executed or acceptance evidence.
func Publish(a workspace.FrozenArtifact, binding Binding, goal string, evidence Evidence) (Bundle, error) {
	return publish(a, binding, goal, evidence, nil)
}

// PublishVerified derives engineering status exclusively from an owned stopped
// verifier, never from Native model text or a caller-declared JSON summary.
func PublishVerified(a workspace.FrozenArtifact, binding Binding, goal string, native Evidence, result evidence.Result, spec evidence.Spec) (Bundle, error) {
	if binding.Role != stageplan.Testing {
		return Bundle{}, ErrInvalid
	}
	v, err := evidence.Export(result, a, spec)
	if err != nil {
		return Bundle{}, ErrInvalid
	}
	return publish(a, binding, goal, native, &v)
}

func publish(a workspace.FrozenArtifact, binding Binding, goal string, nativeEvidence Evidence, verified *evidence.Stored) (Bundle, error) {
	if !validBinding(binding) || !a.Current() || !utf8.ValidString(goal) || strings.TrimSpace(goal) == "" || len(goal) > 64<<10 || !digest(nativeEvidence.OutputHash) || !digest(nativeEvidence.StopProofHash) {
		return Bundle{}, ErrInvalid
	}
	doc := Document{Version: 1, Binding: binding, Artifact: a.Manifest(), Decisions: []string{}, Pending: []string{"design_decisions_not_extracted", "engineering_tests_not_executed", "independent_review_pending", "acceptance_pending"}}
	doc.Task.Goal = goal
	doc.Evidence.Evidence, doc.Evidence.Status = nativeEvidence, "unverified"
	if verified != nil {
		doc.Evidence.Status = string(verified.Verdict.Status)
		doc.Evidence.TestsExecuted = evidence.TestsExecuted(verified.Record)
		doc.Pending = []string{"design_decisions_not_extracted", "independent_review_pending", "acceptance_pending"}
		if !doc.Evidence.TestsExecuted {
			doc.Pending = append(doc.Pending, "engineering_tests_not_executed")
		}
	}
	doc.Changes = doc.Artifact.Changes
	raw, err := json.Marshal(doc)
	if err != nil || len(raw) > 8<<20 {
		return Bundle{}, ErrInvalid
	}
	r, err := os.OpenRoot(a.Path())
	if err != nil {
		return Bundle{}, ErrInvalid
	}
	defer r.Close()
	f, err := r.OpenFile("handoff.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0400)
	if err != nil {
		return Bundle{}, ErrInvalid
	}
	_, writeErr := f.Write(raw)
	syncErr := f.Sync()
	info, statErr := f.Stat()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || statErr != nil || closeErr != nil || !a.Current() {
		return Bundle{}, ErrInvalid
	}
	bound, err := a.WithHandoffHeader(Hash(raw))
	if err != nil {
		return Bundle{}, ErrInvalid
	}
	h := Bundle{artifact: bound, binding: binding, info: info, hash: Hash(raw)}
	if _, err := h.Read(binding); err != nil {
		return Bundle{}, err
	}
	return h, nil
}
func (h Bundle) Read(expected Binding) (Document, error) {
	if h.info == nil || !validBinding(expected) || !reflect.DeepEqual(expected, h.binding) || !h.artifact.Current() {
		return Document{}, ErrInvalid
	}
	r, err := os.OpenRoot(h.artifact.Path())
	if err != nil {
		return Document{}, ErrInvalid
	}
	defer r.Close()
	f, err := r.OpenFile("handoff.json", os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return Document{}, ErrInvalid
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !sameHeader(h.info, info) {
		return Document{}, ErrInvalid
	}
	raw, err := io.ReadAll(io.LimitReader(f, h.info.Size()+1))
	after, statErr := f.Stat()
	var doc Document
	if err != nil || statErr != nil || !sameHeader(h.info, after) || int64(len(raw)) != h.info.Size() || Hash(raw) != h.hash || json.Unmarshal(raw, &doc) != nil || doc.Version != 1 || !reflect.DeepEqual(doc.Binding, expected) || !reflect.DeepEqual(doc.Artifact, h.artifact.Manifest()) || !h.artifact.Current() {
		return Document{}, ErrInvalid
	}
	return doc, nil
}
func (h Bundle) Copy(expected Binding, root, name string) (workspace.Snapshot, error) {
	if _, err := h.Read(expected); err != nil {
		return workspace.Snapshot{}, err
	}
	s, err := h.artifact.Copy(root, name)
	if err != nil {
		return workspace.Snapshot{}, err
	}
	if _, err := h.Read(expected); err != nil {
		_ = os.RemoveAll(s.Path) // Only this freshly created, privately owned copy.
		return workspace.Snapshot{}, err
	}
	return s, nil
}

// Artifact exposes owned frozen provenance only after the exact bundle and
// header check. Public JSON metadata cannot construct a FrozenArtifact.
func (h Bundle) Artifact(expected Binding) (workspace.FrozenArtifact, error) {
	if _, err := h.Read(expected); err != nil {
		return workspace.FrozenArtifact{}, err
	}
	return h.artifact, nil
}
func validBinding(b Binding) bool {
	role := false
	for _, r := range stageplan.AllRoles() {
		role = role || b.Role == r
	}
	return opaque(b.TaskID) && opaque(b.RunID) && b.PlanRevision > 0 && b.Generation > 0 && role && digest(b.PlanHash) && digest(b.TargetHash)
}
func opaque(s string) bool {
	return len(s) > 0 && len(s) <= 128 && utf8.ValidString(s) && !strings.ContainsAny(s, "/\\\x00\r\n\t ")
}
func digest(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32 && strings.ToLower(s) == s
}
func sameHeader(a, b os.FileInfo) bool {
	x, ok := a.Sys().(*syscall.Stat_t)
	y, other := b.Sys().(*syscall.Stat_t)
	return ok && other && os.SameFile(a, b) && b.Mode().IsRegular() && b.Mode().Perm() == 0400 && a.Mode() == b.Mode() && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime()) && x.Uid == y.Uid && y.Uid == uint32(os.Getuid()) && x.Gid == y.Gid && y.Nlink == 1
}
func Hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

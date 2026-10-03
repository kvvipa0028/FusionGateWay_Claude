package glm

import (
	"context"
	"crypto/sha256"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

// FileCredentialScope is the controller's local credential registration. It
// pins bytes to a declared identity; it does not verify upstream account
// ownership, quota pool, subscription billing or generation admission.
type FileCredentialScope struct{ Account, Workspace, Identity string }
type credentialRecord struct {
	key           string
	device, inode uint64
}
type FileCredential struct {
	path          string
	scope         FileCredentialScope
	digest        [32]byte
	device, inode uint64
}

func (*FileCredential) String() string   { return "GLM private credential file (redacted)" }
func (*FileCredential) GoString() string { return "FileCredential(<redacted>)" }

func NewFileCredential(path string, scope FileCredentialScope) (*FileCredential, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) != "glm-coding-plan.key" || strings.ContainsRune(path, 0) {
		return nil, ErrIdentity
	}
	for _, s := range []string{scope.Account, scope.Workspace, scope.Identity} {
		if s == "" || len(s) > 256 || strings.ContainsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
			return nil, ErrIdentity
		}
	}
	r, e := readCredentialFile(path)
	if e != nil {
		return nil, e
	}
	return &FileCredential{path: path, scope: scope, digest: sha256.Sum256([]byte(r.key)), device: r.device, inode: r.inode}, nil
}

// Load never reads environment variables, browser state, legacy Keychain or
// another provider's credentials. Rotation requires a new identity/registration
// and plan; replacing even identical bytes in a different inode is refused.
func (c *FileCredential) Load(ctx context.Context, target stageplan.ExecutionTarget) (Credential, error) {
	if c == nil || ctx.Err() != nil || target.Account != c.scope.Account || target.Workspace != c.scope.Workspace || target.CredentialIdentity != c.scope.Identity || target.BillingPath != "coding_plan" {
		return Credential{}, ErrIdentity
	}
	r, e := readCredentialFile(c.path)
	if e != nil || ctx.Err() != nil || r.device != c.device || r.inode != c.inode || sha256.Sum256([]byte(r.key)) != c.digest {
		return Credential{}, ErrIdentity
	}
	return Credential{Identity: c.scope.Identity, Key: r.key}, nil
}

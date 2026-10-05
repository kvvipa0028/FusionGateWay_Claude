package codex

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

// FileCredentialScope is trusted local registration, not proof of upstream
// account ownership, region, subscription rights, billing or a quota pool.
// UpstreamAccount is an explicitly registered header binding; never JWT claims.
type FileCredentialScope struct {
	Route                                         stageplan.RouteRef
	Account, Workspace, Identity, UpstreamAccount string
}

type privateFileID struct{ device, inode uint64 }
type privateAuthFile struct {
	raw  []byte
	file privateFileID
	dirs [3]privateFileID
}
type FileCredential struct {
	path   string
	scope  FileCredentialScope
	digest [32]byte
	file   privateFileID
	dirs   [3]privateFileID
}

// The returned tokens remain controller-private for the subscription Forwarder.
// No token, account claim, refresh timestamp or file digest grants execution.
type Credential struct {
	Identity                                      string `json:"-"`
	accessToken, idToken, refreshToken, accountID string
}

func (FileCredential) String() string   { return "Codex private credential file (redacted)" }
func (FileCredential) GoString() string { return "codex.FileCredential(<redacted>)" }
func (Credential) String() string       { return "Codex private OAuth credential (redacted)" }
func (Credential) GoString() string     { return "codex.Credential(<redacted>)" }

func NewFileCredential(path string, scope FileCredentialScope) (*FileCredential, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsRune(path, 0) || filepath.Base(path) != "auth.json" || filepath.Base(filepath.Dir(path)) != ".codex" || filepath.Base(filepath.Dir(filepath.Dir(path))) != "home" || filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(path)))) != "codex" || scope.Route.Revision < 1 {
		return nil, ErrIdentity
	}
	for _, value := range []string{scope.Route.ID, scope.Account, scope.Workspace, scope.Identity, scope.UpstreamAccount} {
		if !gateID.MatchString(value) {
			return nil, ErrIdentity
		}
	}
	r, e := readPrivateAuthFile(path)
	if e != nil {
		return nil, ErrIdentity
	}
	if _, e = parsePrivateAuth(r.raw, scope); e != nil {
		return nil, ErrIdentity
	}
	return &FileCredential{path: path, scope: scope, digest: sha256.Sum256(r.raw), file: r.file, dirs: r.dirs}, nil
}

// Load does not consult ambient auth, Keychain, Native account/read or a JWT.
// Rotation/replacement requires a new trusted registration and frozen plan.
func (c *FileCredential) Load(ctx context.Context, target stageplan.ExecutionTarget) (Credential, error) {
	if c == nil || ctx == nil || ctx.Err() != nil || target.Route != c.scope.Route || target.Account != c.scope.Account || target.Workspace != c.scope.Workspace || target.CredentialIdentity != c.scope.Identity || target.BillingPath != "subscription" || target.RuntimeVersion != CLIVersion || target.PluginVersion != nil {
		return Credential{}, ErrIdentity
	}
	r, e := readPrivateAuthFile(c.path)
	if e != nil || ctx.Err() != nil || r.file != c.file || r.dirs != c.dirs || sha256.Sum256(r.raw) != c.digest {
		return Credential{}, ErrIdentity
	}
	credential, e := parsePrivateAuth(r.raw, c.scope)
	if e != nil || ctx.Err() != nil {
		return Credential{}, ErrIdentity
	}
	return credential, nil
}

func privateAuthString(raw json.RawMessage, maximum int) (string, error) {
	var value string
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil || len(value) < 1 || len(value) > maximum {
		return "", ErrIdentity
	}
	for _, c := range []byte(value) {
		if c < 33 || c > 126 {
			return "", ErrIdentity
		}
	}
	return value, nil
}

func parsePrivateAuth(raw []byte, scope FileCredentialScope) (Credential, error) {
	var top map[string]json.RawMessage
	if len(raw) > 256<<10 || decode(raw, &top) != nil || top == nil {
		return Credential{}, ErrIdentity
	}
	for key, value := range top {
		switch key {
		case "auth_mode", "tokens", "last_refresh":
		case "OPENAI_API_KEY", "agent_identity", "personal_access_token", "bedrock_api_key", "bedrock_access_keys":
			if !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return Credential{}, ErrIdentity
			}
		default:
			return Credential{}, ErrIdentity
		}
	}
	mode, e := privateAuthString(top["auth_mode"], 32)
	if e != nil || mode != "chatgpt" {
		return Credential{}, ErrIdentity
	}
	if stamp, exists := top["last_refresh"]; exists && !bytes.Equal(bytes.TrimSpace(stamp), []byte("null")) {
		s, e := privateAuthString(stamp, 64)
		if e != nil {
			return Credential{}, ErrIdentity
		}
		if _, e = time.Parse(time.RFC3339Nano, s); e != nil {
			return Credential{}, ErrIdentity
		}
	}
	var tokens map[string]json.RawMessage
	if decode(top["tokens"], &tokens) != nil || len(tokens) != 4 {
		return Credential{}, ErrIdentity
	}
	for key := range tokens {
		switch key {
		case "id_token", "access_token", "refresh_token", "account_id":
		default:
			return Credential{}, ErrIdentity
		}
	}
	id, e := privateAuthString(tokens["id_token"], 16<<10)
	if e != nil {
		return Credential{}, ErrIdentity
	}
	access, e := privateAuthString(tokens["access_token"], 16<<10)
	if e != nil {
		return Credential{}, ErrIdentity
	}
	refresh, e := privateAuthString(tokens["refresh_token"], 16<<10)
	if e != nil {
		return Credential{}, ErrIdentity
	}
	account, e := privateAuthString(tokens["account_id"], 128)
	if e != nil || account != scope.UpstreamAccount {
		return Credential{}, ErrIdentity
	}
	return Credential{Identity: scope.Identity, accessToken: access, idToken: id, refreshToken: refresh, accountID: account}, nil
}

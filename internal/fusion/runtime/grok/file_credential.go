package grok

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

// FileCredentialScope is trusted local registration, not proof of upstream
// account ownership, tier, region, billing or a quota pool. The issuer is the
// official accounts.x.ai sign-in origin the fixed CLI device flow stores
// under; Identity binds the route's declared credential revision, never JWT
// or token claims.
type FileCredentialScope struct {
	Route                                stageplan.RouteRef
	Account, Workspace, Identity, Issuer string
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

// The returned bearer remains controller-private for the subscription
// forwarder. No token, expiry or file digest grants execution or admission.
type Credential struct {
	Identity string `json:"-"`
	bearer   string
}

func (FileCredential) String() string   { return "Grok private credential file (redacted)" }
func (FileCredential) GoString() string { return "grok.FileCredential(<redacted>)" }
func (Credential) String() string       { return "Grok private OAuth credential (redacted)" }
func (Credential) GoString() string     { return "grok.Credential(<redacted>)" }

func validIssuer(issuer string) bool {
	if len(issuer) > 128 || !strings.HasPrefix(issuer, "https://") {
		return false
	}
	rest := issuer[len("https://"):]
	host := rest
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		host = rest[:i]
	}
	if host == "" || !strings.Contains(host, ".") || strings.Contains(rest, "//") || strings.HasSuffix(rest, "/") || strings.ContainsAny(issuer, " \t\r\n\x00\\") {
		return false
	}
	for _, c := range []byte(rest) {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}

func NewFileCredential(path string, scope FileCredentialScope) (*FileCredential, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsRune(path, 0) || filepath.Base(path) != "auth.json" || filepath.Base(filepath.Dir(path)) != ".grok" || filepath.Base(filepath.Dir(filepath.Dir(path))) != "home" || filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(path)))) != "grok" || scope.Route.Revision < 1 {
		return nil, ErrIdentity
	}
	for _, value := range []string{scope.Route.ID, scope.Account, scope.Workspace, scope.Identity} {
		if !credentialToken(value) {
			return nil, ErrIdentity
		}
	}
	if !validIssuer(scope.Issuer) {
		return nil, ErrIdentity
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

// Load does not consult ambient auth, Keychain, a Native account/read or any
// token claim. Rotation or replacement requires a new trusted registration.
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

func credentialToken(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, c := range []byte(value) {
		if c <= 32 || c > 126 {
			return false
		}
	}
	return true
}

// decodeStrict rejects empty, non-UTF8, duplicate-key, null-envelope and
// trailing-content JSON before unmarshalling, mirroring the codex reader.
func decodeStrict(raw []byte, out any) error {
	if len(raw) == 0 || len(raw) > 256<<10 || !utf8.Valid(raw) || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ErrIdentity
	}
	seen := 0
	dec := json.NewDecoder(bytes.NewReader(raw))
	var walk func(depth int) error
	walk = func(depth int) error {
		if depth > 8 {
			return ErrIdentity
		}
		tok, e := dec.Token()
		if e != nil {
			return ErrIdentity
		}
		switch v := tok.(type) {
		case json.Delim:
			switch v {
			case '{':
				keys := map[string]bool{}
				for dec.More() {
					kt, e := dec.Token()
					if e != nil {
						return ErrIdentity
					}
					key, ok := kt.(string)
					if !ok || key == "" || keys[key] {
						return ErrIdentity
					}
					keys[key] = true
					seen++
					if seen > 64 {
						return ErrIdentity
					}
					if e := walk(depth + 1); e != nil {
						return ErrIdentity
					}
				}
				if _, e := dec.Token(); e != nil {
					return ErrIdentity
				}
			case '[':
				for dec.More() {
					if e := walk(depth + 1); e != nil {
						return ErrIdentity
					}
				}
				if _, e := dec.Token(); e != nil {
					return ErrIdentity
				}
			}
		case string, float64, bool, nil:
		default:
			return ErrIdentity
		}
		return nil
	}
	if e := walk(0); e != nil {
		return ErrIdentity
	}
	if _, e := dec.Token(); e != io.EOF {
		return ErrIdentity
	}
	if json.Unmarshal(raw, out) != nil {
		return ErrIdentity
	}
	return nil
}

// parsePrivateAuth extracts only the opaque bearer stored under the official
// issuer key. Sibling fields are tolerated bounded (strict depth/count decode
// above) but never read: no expiry, scope or account authority is inferred.
// The official closed-source storage may add fields; an API-key style entry
// is still refused so a PAT never substitutes the subscription session.
func parsePrivateAuth(raw []byte, scope FileCredentialScope) (Credential, error) {
	var top map[string]json.RawMessage
	if decodeStrict(raw, &top) != nil || len(top) != 1 {
		return Credential{}, ErrIdentity
	}
	entry, exists := top[scope.Issuer]
	if !exists {
		return Credential{}, ErrIdentity
	}
	var fields map[string]json.RawMessage
	if decodeStrict(entry, &fields) != nil || len(fields) < 1 || len(fields) > 16 {
		return Credential{}, ErrIdentity
	}
	for name, value := range fields {
		switch name {
		case "api_key", "api-key", "XAI_API_KEY", "deployment_key":
			if !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return Credential{}, ErrIdentity
			}
		}
	}
	bearerRaw, exists := fields["key"]
	if !exists {
		return Credential{}, ErrIdentity
	}
	var bearer string
	if len(bearerRaw) == 0 || json.Unmarshal(bearerRaw, &bearer) != nil || len(bearer) < 1 || len(bearer) > 16<<10 {
		return Credential{}, ErrIdentity
	}
	for _, c := range []byte(bearer) {
		if c < 33 || c > 126 {
			return Credential{}, ErrIdentity
		}
	}
	return Credential{Identity: scope.Identity, bearer: bearer}, nil
}

package provider

// Google OAuth client credentials are supplied privately by the operator,
// never borrowed from upstream or another installed application's settings.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const googleOAuthFileName = "google-oauth-clients.json"

type googleOAuthClient struct {
	ID     string `json:"client_id"`
	Secret string `json:"client_secret"`
}

type googleOAuthConfig struct {
	Version int                          `json:"schema_version"`
	Clients map[string]googleOAuthClient `json:"clients"`
}

type googleOAuthSnapshot struct {
	config googleOAuthConfig
	digest [32]byte
	err    error
}

// A snapshot is frozen on first use of its private data root. Every use
// checks the file again, refusing a changed/deleted file until restart.
var googleOAuthSnapshots = struct {
	sync.Mutex
	byPath map[string]googleOAuthSnapshot
}{byPath: map[string]googleOAuthSnapshot{}}

func googleOAuthConfigPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "fusion-gateway", googleOAuthFileName)
}

func googleOAuthRead(path string) (googleOAuthSnapshot, error) {
	bad := errors.New("Google OAuth client is not configured: private client configuration is missing or invalid")
	parent, err := os.Lstat(filepath.Dir(path))
	if err != nil || !parent.IsDir() || parent.Mode()&os.ModeSymlink != 0 || parent.Mode().Perm()&0077 != 0 {
		return googleOAuthSnapshot{}, bad
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return googleOAuthSnapshot{}, bad
	}
	f, err := os.Open(path)
	if err != nil {
		return googleOAuthSnapshot{}, bad
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) || !opened.Mode().IsRegular() || opened.Mode().Perm()&0077 != 0 {
		return googleOAuthSnapshot{}, bad
	}
	data, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil || len(data) > 65536 {
		return googleOAuthSnapshot{}, bad
	}
	var c googleOAuthConfig
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if dec.Decode(&c) != nil || c.Version != 1 || c.Clients == nil {
		return googleOAuthSnapshot{}, bad
	}
	if dec.Decode(new(any)) != io.EOF {
		return googleOAuthSnapshot{}, bad
	}
	for agent, client := range c.Clients {
		if agent != "gemini" && agent != "antigravity" {
			return googleOAuthSnapshot{}, bad
		}
		if strings.TrimSpace(client.ID) == "" || strings.TrimSpace(client.Secret) == "" || strings.ContainsAny(client.ID+client.Secret, "\r\n\x00") {
			return googleOAuthSnapshot{}, bad
		}
	}
	if a, b := c.Clients["gemini"], c.Clients["antigravity"]; a.ID != "" && a.ID == b.ID {
		return googleOAuthSnapshot{}, bad
	}
	return googleOAuthSnapshot{config: c, digest: sha256.Sum256(data)}, nil
}

func googleOAuthCurrent(path string) googleOAuthSnapshot {
	googleOAuthSnapshots.Lock()
	defer googleOAuthSnapshots.Unlock()
	fresh, err := googleOAuthRead(path)
	old, exists := googleOAuthSnapshots.byPath[path]
	if !exists {
		fresh.err = err
		googleOAuthSnapshots.byPath[path] = fresh
		return fresh
	}
	if old.err != nil {
		return old
	}
	if err != nil || fresh.digest != old.digest {
		old.err = errors.New("Google OAuth client configuration changed or became unavailable; stop related tasks and restart before signing in again")
		googleOAuthSnapshots.byPath[path] = old
		return old
	}
	return old
}

func bindGoogleOAuth(app googleApp) googleApp {
	app.configPath = googleOAuthConfigPath()
	snapshot := googleOAuthCurrent(app.configPath)
	app.configErr = snapshot.err
	c, ok := snapshot.config.Clients[app.agent]
	if app.configErr != nil {
		return app
	}
	if !ok {
		app.configErr = errors.New("Google OAuth client is not configured for this application")
		return app
	}
	app.clientID, app.clientSecret = c.ID, c.Secret
	digest := sha256.Sum256(append(snapshot.digest[:], []byte("\x00"+app.agent)...))
	app.clientRevision = hex.EncodeToString(digest[:])
	return app
}

func (app googleApp) requireOAuth() error {
	if app.configErr != nil {
		return app.configErr
	}
	if app.configPath == "" || app.clientRevision == "" || app.clientID == "" || app.clientSecret == "" {
		return errors.New("Google OAuth client is not configured for this application")
	}
	snapshot := googleOAuthCurrent(app.configPath)
	if snapshot.err != nil {
		return snapshot.err
	}
	c, ok := snapshot.config.Clients[app.agent]
	digest := sha256.Sum256(append(snapshot.digest[:], []byte("\x00"+app.agent)...))
	if !ok || c.ID != app.clientID || c.Secret != app.clientSecret || hex.EncodeToString(digest[:]) != app.clientRevision {
		return errors.New("Google OAuth client identity changed; sign in again after restart")
	}
	return nil
}

func (g googleAccount) requireOAuth() error {
	if err := g.app.requireOAuth(); err != nil {
		return err
	}
	if g.auth.ClientRevision == "" || g.auth.ClientRevision != g.app.clientRevision {
		return errors.New("Google account OAuth client ownership is unverified or changed; sign in again with the configured client")
	}
	return nil
}

func (g googleAccount) cacheKey() string {
	return g.app.agent + "\x00" + g.app.clientRevision + "\x00" + g.auth.RefreshToken
}

// Provider error bodies can echo form credentials. Sanitize at this boundary
// before they enter account status, callbacks or logs.
func postGoogleOAuthToken(ctx context.Context, app googleApp, form url.Values, out any) error {
	if err := app.requireOAuth(); err != nil {
		return err
	}
	err := postToken(ctx, googleTokenURL, "application/x-www-form-urlencoded", []byte(form.Encode()), out)
	if err == nil {
		return nil
	}
	values := []string{app.clientID, app.clientSecret}
	for _, key := range []string{"refresh_token", "code", "code_verifier"} {
		values = append(values, form.Get(key))
	}
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	msg := err.Error()
	for _, value := range values {
		if value != "" {
			for _, encoded := range []string{value, url.QueryEscape(value), url.PathEscape(value)} {
				msg = strings.ReplaceAll(msg, encoded, "[redacted]")
			}
		}
	}
	return errors.New(msg)
}

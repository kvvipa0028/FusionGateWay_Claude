package policy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
)

type Credential struct{ Identity, Account, Secret string }

// HTTPJSONExecutor is an internal bounded JSON contract, not an arbitrary
// vendor API converter. Only revision-admitted server endpoints may use it.
// The real three subscription routes require their native Runtime adapters.
type HTTPJSONExecutor struct {
	Endpoint   string
	Transport  http.RoundTripper
	Credential func(context.Context) (Credential, error)
}

func (e *HTTPJSONExecutor) Call(ctx context.Context, in Invocation) (Reply, error) {
	var out Reply
	if e == nil || e.Transport == nil || e.Credential == nil {
		return out, ErrDispatchDenied
	}
	u, err := url.Parse(e.Endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && (u.Scheme != "http" || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback())) {
		return out, ErrDispatchDenied
	}
	cred, err := e.Credential(ctx)
	if err != nil || cred.Identity != in.Target.CredentialIdentity || cred.Account != in.Target.Account || cred.Secret == "" || strings.ContainsFunc(cred.Secret, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) {
		return out, ErrDispatchDenied
	}
	body, err := json.Marshal(in)
	if err != nil {
		return out, ErrDispatchDenied
	}
	r, err := http.NewRequestWithContext(ctx, "POST", e.Endpoint, bytes.NewReader(body))
	if err != nil {
		return out, ErrDispatchDenied
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+cred.Secret)
	client := &http.Client{Transport: e.Transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(r)
	if err != nil {
		return out, ErrCallFailed
	}
	defer resp.Body.Close()
	// This adapter never retries ambiguous network/status/partial-response errors.
	if resp.StatusCode != http.StatusOK {
		return out, ErrCallFailed
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024+1))
	if err != nil || len(raw) > 1024*1024 {
		return out, ErrCallFailed
	}
	// The contract has only two scalar fields. Reject duplicate and differently
	// cased keys before Go's otherwise case-insensitive struct decoding.
	if !replyKeys(raw) {
		return out, ErrCallFailed
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&out) != nil {
		return Reply{}, ErrCallFailed
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return Reply{}, ErrCallFailed
	}
	return out, nil
}

func replyKeys(raw []byte) bool {
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return false
	}
	seen := map[string]bool{}
	for d.More() {
		token, err = d.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] || (key != "text" && key != "upstream_reported_model") {
			return false
		}
		seen[key] = true
		var v json.RawMessage
		if d.Decode(&v) != nil {
			return false
		}
		if key == "text" && bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return false
		}
	}
	token, err = d.Token()
	if err != nil || token != json.Delim('}') || !seen["text"] {
		return false
	}
	_, err = d.Token()
	return err == io.EOF
}

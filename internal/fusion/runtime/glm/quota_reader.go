package glm

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/fusion/quota"
	"github.com/yetone/magpie/internal/fusion/stageplan"
)

// QuotaEndpoint is the CN personal-plan read endpoint used by the pinned
// Magpie planquota.go. It is independent of generation admission. No team,
// global-host, browser-cookie or general API fallback is attempted.
const QuotaEndpoint = "https://open.bigmodel.cn/api/monitor/usage/quota/limit"

// QuotaReaderConfig is controller wiring, not an HTTP DTO. QueryAllowed must
// verify current quota-purpose authorization, independently of generation.
// Loading and returning a percentage never establishes a verified physical
// pool, account ownership or Coding Plan billing for an engineering call.
type QuotaReaderConfig struct {
	Identity       quota.Identity
	Target         stageplan.ExecutionTarget
	QueryAllowed   func(context.Context, quota.Identity) bool
	LoadCredential func(context.Context, stageplan.ExecutionTarget) (Credential, error)
}
type QuotaReader struct {
	config    QuotaReaderConfig
	transport http.RoundTripper
}

func NewQuotaReader(c QuotaReaderConfig) (*QuotaReader, error) {
	i := c.Identity
	if c.QueryAllowed == nil || c.LoadCredential == nil || i.Provider != "bigmodel" || i.Region != "CN" || i.Generation < 1 || i.Account == "" || i.Workspace == "" || c.Target.Account != i.Account || c.Target.Workspace != i.Workspace || c.Target.CredentialIdentity == "" || c.Target.BillingPath != "coding_plan" {
		return nil, ErrUnverified
	}
	c.Target = clone(Binding{Target: c.Target}).Target
	// Fresh HTTP/1 connections prevent Transport's reused-connection retries.
	// Proxy=nil does not inherit proxy credentials or change the destination.
	tr := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second, MaxResponseHeaderBytes: 16 << 10, MaxConnsPerHost: 2, DisableKeepAlives: true, DisableCompression: true, ForceAttemptHTTP2: false, TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{}}
	return &QuotaReader{config: c, transport: tr}, nil
}

func (r *QuotaReader) live(ctx context.Context, i quota.Identity) bool {
	return r != nil && ctx.Err() == nil && r.config.Identity == i && r.config.QueryAllowed(ctx, i)
}

// Read performs at most one request. All errors are static and no upstream
// body, credential or account details are returned in errors. The snapshot
// deliberately remains unverified until a separate trusted pool collector
// establishes ownership, scope and completeness for the exact identity.
func (r *QuotaReader) Read(ctx context.Context, i quota.Identity) (quota.Snapshot, error) {
	failed := func(status quota.Status) (quota.Snapshot, error) {
		return quota.Snapshot{}, &quota.QueryError{Status: status}
	}
	if !r.live(ctx, i) {
		return failed(quota.Unverified)
	}
	work, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	key, e := r.config.LoadCredential(work, clone(Binding{Target: r.config.Target}).Target)
	if e != nil || key.Identity != r.config.Target.CredentialIdentity || len(key.Key) < 8 || len(key.Key) > 4096 || strings.ContainsAny(key.Key, " \t\r\n\x00") {
		return failed(quota.AuthRequired)
	}
	if !r.live(work, i) {
		return failed(quota.Unverified)
	}
	req, e := http.NewRequestWithContext(work, http.MethodGet, QuotaEndpoint, nil)
	if e != nil {
		return failed(quota.Unknown)
	}
	req.Header.Set("Authorization", key.Key)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cache-Control", "no-cache")
	started := time.Now().UTC()
	res, e := r.transport.RoundTrip(req)
	if e != nil {
		if res != nil && res.Body != nil {
			res.Body.Close()
		}
		return failed(quota.Unknown)
	}
	if res == nil || res.Body == nil {
		return failed(quota.Unknown)
	}
	defer res.Body.Close()
	if !r.live(work, i) {
		return failed(quota.Unverified)
	}
	if res.StatusCode != http.StatusOK {
		switch res.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return failed(quota.AuthRequired)
		case http.StatusNotFound, http.StatusMethodNotAllowed:
			return failed(quota.Unsupported)
		default:
			return failed(quota.Unknown)
		}
	}
	media, _, e := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if e != nil || media != "application/json" || res.Header.Get("Content-Encoding") != "" {
		return failed(quota.Unknown)
	}
	raw, e := io.ReadAll(io.LimitReader(res.Body, (64<<10)+1))
	if e != nil || len(raw) > 64<<10 || bytes.Contains(raw, []byte(key.Key)) {
		return failed(quota.Unknown)
	}
	var envelope struct {
		Success *bool `json:"success"`
		Data    *struct {
			Limits []json.RawMessage `json:"limits"`
		} `json:"data"`
	}
	if decode(raw, &envelope) != nil || envelope.Success == nil || !*envelope.Success || envelope.Data == nil || len(envelope.Data.Limits) == 0 || len(envelope.Data.Limits) > 32 {
		return failed(quota.Unknown)
	}
	if !r.live(work, i) {
		return failed(quota.Unverified)
	}
	received := time.Now().UTC()
	observed := started
	if value := res.Header.Get("Date"); value != "" {
		observed, e = http.ParseTime(value)
		if e != nil || observed.After(received) {
			return failed(quota.Unknown)
		}
	}
	// An intermediary cached observation is never stamped as a new read.
	if res.Header.Get("Age") != "" && res.Header.Get("Age") != "0" {
		return failed(quota.Stale)
	}
	s, e := quota.AdaptGLM(raw, quota.Meta{Identity: i, Source: QuotaEndpoint, ObservedAt: &observed, ReceivedAt: received, Complete: false, Pool: quota.Pool{Provider: i.Provider, Region: i.Region, Verified: false}})
	if e != nil {
		return failed(quota.Unknown)
	}
	return s, nil
}

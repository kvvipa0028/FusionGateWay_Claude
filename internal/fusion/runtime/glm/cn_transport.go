package glm

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

var ErrCNTransport = errors.New("GLM CN transport refused or failed")

// CNTransport is a controller-owned single-send HTTPS transport. It does not
// grant calls or load credentials: CallGate/Scheduler must authorize and spend
// each incoming model request before using it. There is no caller URL/config.
type CNTransport struct{ transport *http.Transport }

func NewCNTransport() *CNTransport {
	return &CNTransport{transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: time.Minute, MaxResponseHeaderBytes: 16 << 10, MaxConnsPerHost: 2, DisableKeepAlives: true, DisableCompression: true, ForceAttemptHTTP2: false, TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{}}}
}

func cnAllowed(r *http.Request) bool {
	if r.URL == nil || r.Method != http.MethodPost || r.URL.Scheme != "https" || r.URL.Host != "open.bigmodel.cn" || r.URL.User != nil || r.URL.Opaque != "" || r.URL.Fragment != "" || r.URL.RawPath != "" || r.URL.Path != "/api/anthropic/v1/messages" || r.URL.RawQuery != "" && r.URL.RawQuery != "beta=true" || r.URL.ForceQuery || r.Host != "" && r.Host != "open.bigmodel.cn" || r.RequestURI != "" || r.Body == nil || r.GetBody != nil || r.ContentLength < 1 || r.ContentLength > 1<<20 || len(r.TransferEncoding) != 0 || len(r.Trailer) != 0 {
		return false
	}
	key := r.Header.Get("X-Api-Key")
	if len(key) < 8 || len(key) > 4096 || r.Header.Get("Authorization") != "Bearer "+key || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Accept") != "text/event-stream" || r.Header.Get("Anthropic-Version") != "2023-06-01" {
		return false
	}
	for _, c := range key {
		if c < 33 || c > 126 {
			return false
		}
	}
	for name, values := range r.Header {
		if len(values) != 1 {
			return false
		}
		switch name {
		case "Authorization", "X-Api-Key", "Content-Type", "Accept", "Anthropic-Version":
		default:
			return false
		}
	}
	return true
}

// RoundTrip owns exactly one fresh HTTP/1 attempt. No reused-connection
// retries, HTTP/2 alternate connection retries, redirect client, environment
// proxy or fallback are involved. Deadline remains active through body EOF or
// Close, not merely until response headers. Errors never echo upstream/key.
func (t *CNTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	refused := func() (*http.Response, error) {
		if r != nil && r.Body != nil {
			r.Body.Close()
		}
		return nil, ErrCNTransport
	}
	if t == nil || t.transport == nil || r == nil || r.Context().Err() != nil {
		return refused()
	}
	work, cancel := context.WithTimeout(r.Context(), time.Minute)
	req := r.Clone(work)
	if !cnAllowed(req) {
		cancel()
		return refused()
	}
	req.Close = true
	res, e := t.transport.RoundTrip(req)
	if e != nil || res == nil || res.Body == nil {
		if res != nil && res.Body != nil {
			res.Body.Close()
		}
		cancel()
		return nil, ErrCNTransport
	}
	res.Request = req
	res.Body = &cnResponseBody{body: res.Body, cancel: cancel}
	return res, nil
}

type cnResponseBody struct {
	body     io.ReadCloser
	cancel   context.CancelFunc
	once     sync.Once
	closeErr error
}

func (b *cnResponseBody) Read(p []byte) (int, error) {
	n, e := b.body.Read(p)
	if e != nil {
		b.Close()
		if e != io.EOF {
			e = ErrCNTransport
		}
	}
	return n, e
}
func (b *cnResponseBody) Close() error {
	b.once.Do(func() {
		b.cancel()
		if b.body.Close() != nil {
			b.closeErr = ErrCNTransport
		}
	})
	return b.closeErr
}

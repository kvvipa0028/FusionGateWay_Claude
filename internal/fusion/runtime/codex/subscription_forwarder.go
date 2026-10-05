package codex

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

var ErrSubscriptionTransport = errors.New("Codex subscription transport refused or failed")

const subscriptionEndpoint = "https://chatgpt.com/backend-api/codex/responses"

type subscriptionCA struct {
	pool   *x509.CertPool
	file   privateFileID
	digest [32]byte
}

// SubscriptionForwarder is trusted controller wiring, not an execution grant.
// CallGate/Manager/Scheduler authorize and spend each attempt before Send.
// There is no caller URL/client, API key, proxy, redirect or retry fallback.
type SubscriptionForwarder struct {
	credential *FileCredential
	transport  *http.Transport
	ca         subscriptionCA
}

func (SubscriptionForwarder) String() string   { return "Codex subscription forwarder (private)" }
func (SubscriptionForwarder) GoString() string { return "codex.SubscriptionForwarder(<private>)" }

func NewSubscriptionForwarder(credential *FileCredential) (*SubscriptionForwarder, error) {
	if credential == nil || credential.path == "" {
		return nil, ErrSubscriptionTransport
	}
	ca, e := readSubscriptionCA()
	if e != nil {
		return nil, ErrSubscriptionTransport
	}
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: ca.pool}, TLSHandshakeTimeout: 5 * time.Second,
		ResponseHeaderTimeout: time.Minute, MaxResponseHeaderBytes: 16 << 10, MaxConnsPerHost: 2,
		DisableKeepAlives: true, DisableCompression: true, ForceAttemptHTTP2: false,
		TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{}}
	return &SubscriptionForwarder{credential: credential, transport: transport, ca: ca}, nil
}

func (f *SubscriptionForwarder) current(ctx context.Context, target stageplan.ExecutionTarget) bool {
	if f == nil || f.credential == nil || f.transport == nil || ctx == nil || ctx.Err() != nil {
		return false
	}
	if _, e := f.credential.Load(ctx, target); e != nil {
		return false
	}
	ca, e := readSubscriptionCA()
	return e == nil && ca.file == f.ca.file && ca.digest == f.ca.digest && ctx.Err() == nil
}

// Send buffers the bounded complete stream before delivery, matching CallGate's
// existing no-partial-output rule. Only upstream HTTP/SSE headers supply the
// actual model; ordinary response.model and the request cannot provide proof.
func (f *SubscriptionForwarder) Send(ctx context.Context, target stageplan.ExecutionTarget, raw []byte) (ForwardResponse, error) {
	refuse := func() (ForwardResponse, error) { return ForwardResponse{}, ErrSubscriptionTransport }
	if !f.current(ctx, target) || target.Effort.Value == nil || !gateID.MatchString(*target.Effort.Value) || (target.Effort.RequestedMode != stageplan.EffortExplicit && target.Effort.RequestedMode != stageplan.EffortDefault) || target.LockEnforcement != stageplan.ControlledCalls || !gateID.MatchString(target.RequestedModel) || target.RequestedModel != target.ResolvedModel || len(raw) == 0 || len(raw) > 1<<20 {
		return refuse()
	}
	validator := CallGate{binding: Binding{Target: target}}
	if !validator.request(raw) {
		return refuse()
	}
	credential, e := f.credential.Load(ctx, target)
	if e != nil {
		return refuse()
	}
	markers := [][]byte{[]byte(credential.accessToken), []byte(credential.idToken), []byte(credential.refreshToken), []byte(credential.accountID)}
	if privateJSON(raw, markers...) {
		return refuse()
	}
	work, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	req, e := http.NewRequestWithContext(work, http.MethodPost, subscriptionEndpoint, bytes.NewReader(append([]byte(nil), raw...)))
	if e != nil {
		return refuse()
	}
	req.GetBody, req.Close = nil, true // No replayable body or reused connection.
	req.Header.Set("Authorization", "Bearer "+credential.accessToken)
	req.Header.Set("ChatGPT-Account-ID", credential.accountID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("OpenAI-Beta", "responses=experimental")
	req.Header.Set("Originator", "codex_cli_rs")
	req.Header.Set("Version", CLIVersion)
	req.Header.Set("User-Agent", "codex_cli_rs/"+CLIVersion+" (Fusion Gateway; "+runtime.GOOS+"; "+runtime.GOARCH+")")
	var fields map[string]json.RawMessage
	_ = decode(raw, &fields)
	if key, exists := fields["prompt_cache_key"]; exists {
		value, e := privateAuthString(key, 256)
		if e != nil {
			req.Body.Close()
			return refuse()
		}
		req.Header.Set("Session_id", value)
		req.Header.Set("Conversation_id", value)
	}
	if !f.current(work, target) {
		req.Body.Close()
		return refuse()
	}
	// Direct RoundTrip: no client redirect, automatic auth refresh, API retry or
	// model selection. Fresh HTTP/1 + nil GetBody also forbid transport replay.
	res, e := f.transport.RoundTrip(req)
	if e != nil || res == nil || res.Body == nil {
		if res != nil && res.Body != nil {
			res.Body.Close()
		}
		return refuse()
	}
	defer res.Body.Close()
	if !f.current(work, target) {
		return refuse()
	}
	if res.StatusCode == 429 {
		// Never expose or drain a private upstream error. A later Native request
		// must obtain a new CallGate/Scheduler Permit; this forwarder never retries.
		return ForwardResponse{StatusCode: 429, Body: io.NopCloser(bytes.NewReader(nil))}, nil
	}
	media, _, e := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if res.StatusCode != 200 || e != nil || media != "text/event-stream" || len(res.Header.Values("Content-Type")) != 1 || len(res.Header.Values("Content-Encoding")) != 0 {
		return refuse()
	}
	model, ok := httpReportedModel(res.Header)
	if !ok {
		return refuse()
	}
	body, e := io.ReadAll(io.LimitReader(res.Body, (8<<20)+1))
	if e != nil || len(body) > 8<<20 || !f.current(work, target) {
		return refuse()
	}
	model, ok = streamReportedModel(body, model)
	if !ok || model == "" || model != target.ResolvedModel || !responsesStream(body, model, markers...) {
		return refuse()
	}
	return ForwardResponse{StatusCode: 200, ContentType: "text/event-stream", ReportedModel: model,
		Body:           &subscriptionBuffer{reader: bytes.NewReader(body), ctx: ctx, credential: f.credential, target: cloneGate(Binding{Target: target}).Target},
		PrivateMarkers: markers}, nil
}

func httpReportedModel(headers http.Header) (string, bool) {
	model := ""
	for name, values := range headers {
		if !strings.EqualFold(name, "OpenAI-Model") {
			continue
		}
		if model != "" || len(values) != 1 || !gateID.MatchString(values[0]) {
			return "", false
		}
		model = values[0]
	}
	return model, true
}

// Only documented model metadata is accepted/forwarded inside the SSE body.
// Alternate spelling X-OpenAI-Model is documented for SSE, not HTTP headers.
func sseReportedModel(raw []byte) (string, bool) {
	var fields map[string]json.RawMessage
	if decode(raw, &fields) != nil || len(fields) != 1 {
		return "", false
	}
	for name, value := range fields {
		if !strings.EqualFold(name, "OpenAI-Model") && !strings.EqualFold(name, "X-OpenAI-Model") {
			return "", false
		}
		model, e := privateAuthString(value, 128)
		return model, e == nil && gateID.MatchString(model)
	}
	return "", false
}

func streamReportedModel(raw []byte, model string) (string, bool) {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var data []string
	consume := func() bool {
		if len(data) == 0 {
			return true
		}
		var event map[string]json.RawMessage
		if decode([]byte(strings.Join(data, "\n")), &event) != nil {
			return false
		}
		data = nil
		if response, present := event["response"]; present {
			var fields map[string]json.RawMessage
			if decode(response, &fields) != nil || fields == nil {
				return false
			}
			if headers, present := fields["headers"]; present {
				reported, ok := sseReportedModel(headers)
				if !ok || model != "" && reported != model {
					return false
				}
				model = reported
			}
		}
		return true
	}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if !consume() {
				return "", false
			}
		} else if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	return model, scanner.Err() == nil && len(data) == 0
}

type subscriptionBuffer struct {
	mu         sync.Mutex
	reader     *bytes.Reader
	ctx        context.Context
	credential *FileCredential
	target     stageplan.ExecutionTarget
	closed     bool
}

func (b *subscriptionBuffer) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return 0, io.EOF
	}
	if _, e := b.credential.Load(b.ctx, b.target); e != nil {
		b.closed = true
		b.reader = nil
		return 0, ErrSubscriptionTransport
	}
	n, e := b.reader.Read(p)
	if e == io.EOF {
		b.closed = true
		b.reader = nil
	}
	return n, e
}
func (b *subscriptionBuffer) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	b.reader = nil
	return nil
}

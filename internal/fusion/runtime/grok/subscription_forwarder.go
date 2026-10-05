package grok

import (
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
	"sync"
	"time"

	"github.com/yetone/magpie/internal/fusion/stageplan"
)

var ErrSubscriptionTransport = errors.New("Grok subscription transport refused or failed")

const subscriptionEndpoint = "https://cli-chat-proxy.grok.com/v1/chat/completions"

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

func (SubscriptionForwarder) String() string   { return "Grok subscription forwarder (private)" }
func (SubscriptionForwarder) GoString() string { return "grok.SubscriptionForwarder(<private>)" }

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
// existing no-partial-output rule. The official proxy echoes the routed model
// in every chat.completion.chunk; a missing or drifting echo refuses delivery
// (echo consistency, not an independent header proof — see the contract).
func (f *SubscriptionForwarder) Send(ctx context.Context, target stageplan.ExecutionTarget, raw []byte) (ForwardResponse, error) {
	refuse := func() (ForwardResponse, error) { return ForwardResponse{}, ErrSubscriptionTransport }
	if !f.current(ctx, target) || target.Effort.RequestedMode == stageplan.EffortNone && target.Effort.Value != nil || target.Effort.RequestedMode != stageplan.EffortNone && (target.Effort.RequestedMode != stageplan.EffortExplicit && target.Effort.RequestedMode != stageplan.EffortDefault || target.Effort.Value == nil || !modelID.MatchString(*target.Effort.Value)) || target.LockEnforcement != stageplan.ControlledCalls || !modelID.MatchString(target.RequestedModel) || target.RequestedModel != target.ResolvedModel || len(raw) == 0 || len(raw) > 1<<20 {
		return refuse()
	}
	// Mirror the managed adapter's controlled read-only tool surface so the
	// gate's own request contract validates the frozen body unchanged.
	validator := CallGate{binding: GateBinding{Observer: Binding{Tools: []string{"read_file"}}, Target: target}}
	if !validator.request(raw) {
		return refuse()
	}
	credential, e := f.credential.Load(ctx, target)
	if e != nil {
		return refuse()
	}
	markers := [][]byte{[]byte(credential.bearer)}
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
	req.Header.Set("Authorization", "Bearer "+credential.bearer)
	req.Header.Set("X-XAI-Token-Auth", "xai-grok-cli")
	req.Header.Set("x-grok-model-override", target.ResolvedModel)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", "grok-cli/"+CLIVersion+" (Fusion Gateway; "+runtime.GOOS+"; "+runtime.GOARCH+")")
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
	body, e := io.ReadAll(io.LimitReader(res.Body, (8<<20)+1))
	if e != nil || len(body) > 8<<20 || !f.current(work, target) {
		return refuse()
	}
	if !chunksEchoModel(body, target.ResolvedModel, markers...) {
		return refuse()
	}
	return ForwardResponse{StatusCode: 200, ContentType: "text/event-stream", ReportedModel: target.ResolvedModel,
		Body:           &subscriptionBuffer{reader: bytes.NewReader(body), ctx: ctx, credential: f.credential, target: copyGate(GateBinding{Target: target}).Target},
		PrivateMarkers: markers}, nil
}

// chunksEchoModel requires every chat.completion.chunk data frame to echo the
// frozen model exactly, at least one frame to exist, the terminal [DONE]
// marker to be present and no credential reflection anywhere in the stream.
func chunksEchoModel(raw []byte, model string, markers ...[]byte) bool {
	echoed := false
	for _, frame := range bytes.Split(raw, []byte("\n\n")) {
		frame = bytes.TrimSuffix(frame, []byte("\n"))
		if len(bytes.TrimSpace(frame)) == 0 {
			continue
		}
		data := ""
		done := false
		for _, line := range bytes.Split(frame, []byte("\n")) {
			line = bytes.TrimSuffix(line, []byte("\r"))
			if !bytes.HasPrefix(line, []byte("data:")) {
				if len(bytes.TrimSpace(line)) != 0 {
					return false
				}
				continue
			}
			if data != "" || done {
				return false
			}
			payload := string(bytes.TrimPrefix(bytes.TrimPrefix(line, []byte("data:")), []byte(" ")))
			if payload == "[DONE]" {
				done = true
			} else {
				data = payload
			}
		}
		if data == "" {
			if !done {
				return false
			}
			continue
		}
		if done {
			return false
		}
		var fields map[string]json.RawMessage
		if decode([]byte(data), &fields) != nil || privateJSON([]byte(data), markers...) {
			return false
		}
		var echoedModel string
		if e := json.Unmarshal(fields["model"], &echoedModel); e != nil || echoedModel != model {
			return false
		}
		echoed = true
	}
	return echoed && bytes.HasSuffix(bytes.TrimSpace(raw), []byte("data: [DONE]"))
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

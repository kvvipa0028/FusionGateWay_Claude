package provider

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/yetone/magpie/internal/catalog"
)

// Which APIs a vendor or relay serves behind the one URL the user typed is
// found by asking (01huadalang on Discord: 一键检测支持什么协议，发一个最小
// 请求看是否返回200): each of the three magpie speaks upstream — Chat
// Completions, Responses and Anthropic Messages — is sent the smallest
// request Test sends, at that URL as the API takes it, and what answered
// is what the provider can be given. Gemini's isn't among them: magpie
// serves it to clients but never asks a vendor on it, so a URL for it
// would be one the provider has nowhere to keep.

// Detection is what one API answered at the URL it was asked at.
type Detection struct {
	Result
	// Base is the URL the provider keeps for it, as the editor's fields
	// take it: …/v1 for OpenAI's two, the root for Anthropic's.
	Base string `json:"base"`
}

// DetectBase is base as proto takes it: an endpoint pasted whole (…/v1/chat/
// completions) cut to its base, then the root for Anthropic, which adds
// /v1/messages itself, and …/v1 for OpenAI's two when base is a bare host —
// the editor's respellURL, in Go.
func DetectBase(base string, proto Protocol) string {
	u := strings.TrimRight(strings.TrimSpace(base), "/")
	for _, end := range []string{"/chat/completions", "/responses", "/v1/messages", "/messages"} {
		u = strings.TrimSuffix(u, end)
	}
	if u == "" {
		return ""
	}
	if proto == Anthropic {
		return strings.TrimSuffix(u, "/v1")
	}
	if pu, err := url.Parse(u); err == nil && strings.Trim(pu.Path, "/") == "" {
		return u + "/v1"
	}
	return u
}

// ErrNoURL is a detection with no URL to ask at.
var ErrNoURL = errors.New("type the base URL first")

// Detect asks each of Chat, Responses and Anthropic at the URL p has for
// it, else at base as it takes it (DetectBase), the smallest request, and
// says what each answered, in that order. model is asked on all three;
// with none, one from p's list is picked for each — a Claude model on
// Anthropic's, an OpenAI one on Responses — and with no list, the vendor
// is asked for one first (a GET of its models, which costs nothing).
// Only those URLs are asked, and only a key's provider: a sign-in's API
// is its agent's.
func (p Provider) Detect(ctx context.Context, base, model string) ([]Detection, error) {
	if p.Account != nil {
		return nil, errors.New("a sign-in is asked on its agent's own API; there is nothing to detect")
	}
	q := p
	for _, proto := range Protocols {
		u := strings.TrimSpace(p.Base(proto))
		if u == "" {
			u = DetectBase(base, proto)
		}
		if u != "" {
			if pu, err := url.Parse(u); err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Host == "" {
				return nil, errors.New("the base URL must start with http:// or https://, not " + u)
			}
		}
		switch proto {
		case Chat:
			q.Chat = u
		case Responses:
			q.Responses = u
		case Anthropic:
			q.Anthropic = u
		}
	}
	if len(q.Speaks()) == 0 {
		return nil, ErrNoURL
	}
	ctx = q.Via(ctx)
	model = strings.TrimSpace(model)
	var ids []string
	if model == "" {
		ids = q.detectModels(ctx)
	}
	out := make([]Detection, len(Protocols))
	var wg sync.WaitGroup
	for i, proto := range Protocols {
		m := model
		if m == "" {
			m = detectModel(ids, proto)
		}
		out[i] = Detection{Result: Result{Protocol: proto, Model: m}, Base: q.Base(proto)}
		if out[i].Base == "" {
			out[i].Error = "no URL to ask"
			continue
		}
		if m == "" {
			out[i].Error = "no model to try: type one the vendor serves"
			continue
		}
		k, ok := q.keyFor(proto)
		if !ok {
			out[i].Error = "no key is on for this endpoint"
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			u, body := tiny(k, proto, UpstreamName(q, m))
			out[i].Result = probe(ctx, k, proto, u, k.Prepare([]byte(body)), m, testWait)
		}()
	}
	wg.Wait()
	return out, nil
}

// detectModels are the models a detection may ask for: those p exposes and
// lists, else the vendor's list, asked for now and not kept.
func (p Provider) detectModels(ctx context.Context) []string {
	var ids []string
	if p.ID != "" {
		for _, ms := range [][]catalog.Model{p.Exposed(), p.Available()} {
			for _, m := range ms {
				if !slices.Contains(ids, m.ID) {
					ids = append(ids, m.ID)
				}
			}
		}
	}
	if len(ids) > 0 {
		return ids
	}
	ms, _, err := p.fetchOne(ctx)
	if err != nil {
		return nil
	}
	for _, m := range ms {
		ids = append(ids, m.ID)
	}
	return ids
}

// detectModel is the model of ids to ask proto for: one made for it — a
// Claude model on Anthropic's, an OpenAI one on Responses, any other on
// Chat — else the first that chats.
func detectModel(ids []string, proto Protocol) string {
	made := func(id string) bool {
		switch proto {
		case Anthropic:
			return isClaude(id)
		case Responses:
			return openAIModel(id)
		}
		return !isClaude(id)
	}
	for _, want := range []func(string) bool{made, func(string) bool { return true }} {
		for _, id := range ids {
			if want(id) && !catalog.ImagesAPI(id) {
				return id
			}
		}
	}
	if len(ids) > 0 {
		return ids[0]
	}
	return ""
}

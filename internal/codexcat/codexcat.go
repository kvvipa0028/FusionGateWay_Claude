// Package codexcat renders models the way Codex describes them: the entries
// of its models.json, for `model_catalog_json` and for the model list magpie
// hands Codex in place of the ChatGPT backend's.
package codexcat

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/yetone/magpie/internal/catalog"
)

// Prompt is Codex's generic system prompt (Apache-2.0, openai/codex,
// core/gpt-5.2-codex_prompt.md). Third-party models need one because the
// bundled catalog only carries prompts for OpenAI models.
//
//go:embed codex_prompt.md
var Prompt string

// DefaultEffort picks the middle of the road: "medium" or "high" when
// offered, else whatever the list starts with.
func DefaultEffort(e []string) string {
	for _, want := range []string{"medium", "high"} {
		if slices.Contains(e, want) {
			return want
		}
	}
	return e[0]
}

// Catalog renders models as a whole models.json.
func Catalog(ms []catalog.Model) []byte {
	out := struct {
		Models []any `json:"models"`
	}{Models: Entries(ms, 0)}
	if out.Models == nil {
		out.Models = []any{}
	}
	b, _ := json.MarshalIndent(out, "", " ")
	return b
}

// Entries renders models as models.json entries, ranked after the first
// `after`. Only fields Codex requires or that change behaviour are set; the
// rest take Codex's defaults.
func Entries(ms []catalog.Model, after int) []any {
	type level struct {
		Effort      string `json:"effort"`
		Description string `json:"description"`
	}
	type tier struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	type model struct {
		Slug          string  `json:"slug"`
		DisplayName   string  `json:"display_name"`
		Description   string  `json:"description"`
		Instructions  string  `json:"base_instructions"`
		DefaultEffort *string `json:"default_reasoning_level"`
		Efforts       []level `json:"supported_reasoning_levels"`
		Shell         string  `json:"shell_type"`
		Visibility    string  `json:"visibility"`
		InAPI         bool    `json:"supported_in_api"`
		Priority      int     `json:"priority"`
		Verbosity     bool    `json:"support_verbosity"`
		DefVerbosity  *string `json:"default_verbosity"`
		ApplyPatch    string  `json:"apply_patch_tool_type"`
		Truncation    struct {
			Mode  string `json:"mode"`
			Limit int    `json:"limit"`
		} `json:"truncation_policy"`
		Tools      []string `json:"experimental_supported_tools"`
		Modalities []string `json:"input_modalities"`
		Context    *int     `json:"context_window,omitempty"`
		Tiers      []tier   `json:"service_tiers"`
		// Without the search, Codex puts every MCP tool's schema (a
		// ChatGPT sign-in's apps' among them) in every request, 190K
		// tokens before the first word (#258); with it, they are named in
		// tool_search's description and handed over when searched for,
		// as Codex does for its own models. Magpie serves the search to
		// any model as a function (gateway/toolsearch.go). Code mode and
		// Responses Lite stay off: the one has the model write JavaScript
		// against Codex's tools, the other moves the tools and
		// instructions into the input, neither for a model not trained on
		// them.
		SearchTool bool `json:"supports_search_tool"`
		// Required from Codex 0.147 (#298: without it the whole catalog
		// fails to load); later Codex ask for parallel calls whatever it
		// says, so it says what they do.
		Parallel bool `json:"supports_parallel_tool_calls"`
	}
	own := CacheEntries()
	var entries []any
	for i, m := range ms {
		if raw, ok := own[strings.TrimPrefix(m.ID, "codex/")]; ok && strings.HasPrefix(m.ID, "codex/") {
			entries = append(entries, ownEntry(raw, m.ID, m.Name, after+i+1))
			continue
		}
		e := model{
			Slug: m.ID, DisplayName: m.Name, Description: m.Name + " via magpie",
			Instructions: Prompt, Efforts: []level{},
			Shell: "unified_exec", Visibility: "list", InAPI: true, Priority: after + i + 1,
			ApplyPatch: "freeform", Tools: []string{}, Modalities: []string{"text"},
			Tiers: []tier{}, SearchTool: true, Parallel: true,
		}
		// Fast mode: a ChatGPT account's GPT model Codex has no entry for,
		// or a group one is in, gets the tier Codex's own catalog gives its
		// GPT models
		if slug, ok := strings.CutPrefix(m.ID, "codex/"); m.Fast || ok && strings.HasPrefix(slug, "gpt-") {
			e.Tiers = append(e.Tiers, tier{ID: "priority", Name: "Fast", Description: "1.5x speed, increased usage"})
		}
		if m.Images {
			e.Modalities = append(e.Modalities, "image")
		}
		if c := m.Context; c > 0 {
			e.Context = &c
		}
		e.Truncation.Mode, e.Truncation.Limit = "tokens", 10000
		for _, ef := range m.Efforts {
			e.Efforts = append(e.Efforts, level{Effort: ef})
		}
		if len(m.Efforts) > 0 {
			d := DefaultEffort(m.Efforts)
			e.DefaultEffort = &d
		}
		entries = append(entries, e)
	}
	return entries
}

// CacheEntries is Codex's own models, as models_cache.json describes them
// for the ChatGPT account it last asked with, by slug.
func CacheEntries() map[string]map[string]any {
	home, _ := os.UserHomeDir()
	b, err := os.ReadFile(filepath.Join(home, ".codex", "models_cache.json"))
	if err != nil {
		return nil
	}
	var cache struct {
		ETag   string           `json:"etag"`
		Models []map[string]any `json:"models"`
	}
	if json.Unmarshal(b, &cache) != nil {
		return nil
	}
	out := map[string]map[string]any{}
	for _, m := range cache.Models {
		desc, _ := m["description"].(string)
		if slug, _ := m["slug"].(string); slug != "" && !catalog.MagpieAdded(cache.ETag, slug, desc) {
			out[slug] = m
		}
	}
	return out
}

// ownEntry is one of Codex's own models, reached through magpie with the
// ChatGPT sign-in: its entry as Codex has it (images, context window, tools,
// instructions), under magpie's id. The start-up notice and the upgrade
// prompt are left out; they name slugs the catalog does not have.
func ownEntry(raw map[string]any, id, name string, priority int) map[string]any {
	e := make(map[string]any, len(raw))
	for k, v := range raw {
		e[k] = v
	}
	delete(e, "availability_nux")
	delete(e, "upgrade")
	e["slug"], e["display_name"], e["priority"], e["visibility"] = id, name, priority, "list"
	if s, _ := e["base_instructions"].(string); s == "" {
		e["base_instructions"] = Prompt
	}
	return e
}

// Codex keeps the model list it was handed in models_cache.json, with the
// list's ETag, and asks again only once the cache has aged (minutes) — or
// when a reply's X-Models-Etag differs from it; the same one only makes the
// cache young again. Passed on as the ChatGPT backend gave it, the ETag
// said nothing of magpie's models, so a provider added since stayed out of
// Codex's /model for as long as Codex kept getting replies. The ETag magpie
// hands on carries a tag of its models too.

// Tag names a list of magpie's models: another list, another tag.
func Tag(ms []catalog.Model) string {
	b, _ := json.Marshal(ms)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:6])
}

// tagMark is where a tag starts in an ETag magpie made.
const tagMark = "+magpie-"

// WithTag is an ETag of the backend's (or none) with magpie's tag in it:
// W/"abc" becomes W/"abc+magpie-<tag>".
func WithTag(etag, tag string) string {
	if strings.HasSuffix(etag, `"`) && len(etag) > 1 {
		return strings.TrimSuffix(etag, `"`) + tagMark + tag + `"`
	}
	return etag + tagMark + tag
}

// Tagged reports whether an ETag carries this tag.
func Tagged(etag, tag string) bool {
	return strings.Contains(etag, tagMark+tag)
}

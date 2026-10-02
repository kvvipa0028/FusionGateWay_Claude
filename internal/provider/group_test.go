package provider

import "testing"

// Vendors spell the same model differently; a group magpie finds joins
// them all.
func TestAutoGroupsSameModel(t *testing.T) {
	for in, want := range map[string]string{
		"claude-opus-5-5":                 "claude-opus-5-5",
		"anthropic/claude-opus-5.5":       "claude-opus-5-5",
		"Claude-Opus-5.5":                 "claude-opus-5-5",
		"claude-opus-5-5-20260801":        "claude-opus-5-5",
		"anthropic/claude-opus-5.5:batch": "claude-opus-5-5:batch",
		"gpt-5.1-codex":                   "gpt-5-1-codex",
		"v1.beta":                         "v1.beta",
	} {
		if got := sameModel(in); got != want {
			t.Errorf("sameModel(%q) = %q, want %q", in, got, want)
		}
	}
	e := func(p, m, name string) Entry {
		return Entry{ID: p + "/" + m, Model: m, Name: name, Provider: Provider{ID: p}}
	}
	gs := autoGroups([]Entry{
		e("openrouter", "anthropic/claude-opus-5.5", "anthropic/claude-opus-5.5"),
		e("copilot", "claude-opus-5.5", "claude-opus-5.5"),
		e("claude", "claude-opus-5-5", "Claude Opus 5.5"),
	})
	if len(gs) != 1 || gs[0].ID != "auto-claude-opus-5-5" || len(gs[0].Members) != 3 || gs[0].Name != "Claude Opus 5.5" {
		t.Fatalf("groups: %+v", gs)
	}

}

// A group takes images only when every member does: any of them may be
// the one that answers.
func TestGroupImages(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	e := func(p, m string, images bool) Entry {
		return Entry{ID: p + "/" + m, Model: m, Name: m, Provider: Provider{ID: p}, Images: images}
	}
	gs := groupEntries([]Entry{
		e("a", "kimi-k3", true), e("b", "kimi-k3", true),
		e("a", "glm-5.3", true), e("b", "glm-5.3", false),
	})
	got := map[string]bool{}
	for _, g := range gs {
		got[g.ID] = g.Images
	}
	if len(got) != 2 || !got[GroupPrefix+"auto-kimi-k3"] || got[GroupPrefix+"auto-glm-5-3"] {
		t.Errorf("%v", got)
	}
}

func TestGroupImagesKeepUnknownMemberUnknown(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	yes := true
	entries := []Entry{
		{ID: "a/m", Model: "m", Provider: Provider{ID: "a"}, Images: true, ImageInput: &yes},
		{ID: "b/m", Model: "m", Provider: Provider{ID: "b"}, Images: true},
	}
	groups := groupEntries(entries)
	if len(groups) != 1 || !groups[0].Images || groups[0].ImageInput != nil {
		t.Fatalf("group with inferred image support lost images: %+v", groups)
	}
}

// A group whose first member is a ChatGPT account's model is still magpie's
// to list to a signed-in Codex: the backend lists the model, not the group.
func TestCodexListedKeepsGroups(t *testing.T) {
	acct := Provider{ID: "codex-me", Account: &Account{Agent: "codex"}}
	ms := codexListed([]Entry{
		{ID: GroupPrefix + "fast", Model: "gpt-5.5", Name: "fast", Provider: acct, Group: "fast"},
		{ID: "codex-me/gpt-5.5", Model: "gpt-5.5", Provider: acct},
		{ID: "or/glm-5.3", Model: "glm-5.3", Provider: Provider{ID: "or"}},
	}, func(id string) []Member {
		return []Member{{Provider: Provider{ID: "or"}, Model: "glm-5.3"}, {Provider: acct, Model: "gpt-5.5"}}
	})
	var ids []string
	for _, m := range ms {
		ids = append(ids, m.ID)
	}
	if len(ids) != 2 || ids[0] != GroupPrefix+"fast" || ids[1] != "or/glm-5.3" {
		t.Errorf("listed %v", ids)
	}
	// Codex offers /fast on the group, a ChatGPT GPT model being in it
	if !ms[0].Fast || ms[1].Fast {
		t.Errorf("fast: group %v, model %v", ms[0].Fast, ms[1].Fast)
	}
}

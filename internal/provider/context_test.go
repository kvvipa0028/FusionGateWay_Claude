package provider

import (
	"path/filepath"
	"testing"
)

func TestContextSetByUser(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	// set, not empty: a group saved lists what Devin's CLI reports, and
	// Devin given an empty one writes its config into the working directory
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	p := Provider{ID: "a", Contexts: map[string]int{"*": 272000, "big": 1000000}}
	if got := p.ContextOf("big"); got != 1000000 {
		t.Errorf("big: %d", got)
	}
	if got := p.ContextOf("other"); got != 272000 {
		t.Errorf("other: %d", got)
	}
	if got := (Provider{ID: "b"}).ContextOf("x"); got != 0 {
		t.Errorf("unset: %d", got)
	}

	e := func(p, m string, ctx int) Entry {
		return Entry{ID: p + "/" + m, Model: m, Name: m, Provider: Provider{ID: p}, Context: ctx}
	}
	entries := []Entry{e("a", "m1", 128000), e("b", "m2", 200000)}
	of := func() int {
		for _, g := range groupEntries(entries) {
			if g.ID == GroupPrefix+"g" {
				return g.Context
			}
		}
		t.Fatal("no group g")
		return 0
	}
	if err := SaveGroup(Group{ID: "g", Name: "g", Members: []string{"a/m1", "b/m2"}}); err != nil {
		t.Fatal(err)
	}
	if got := of(); got != 128000 {
		t.Errorf("shortest member's: %d", got)
	}
	if err := SaveGroup(Group{ID: "g", Name: "g", Members: []string{"a/m1", "b/m2"}, Context: 400000}); err != nil {
		t.Fatal(err)
	}
	if got := of(); got != 400000 {
		t.Errorf("set: %d", got)
	}
}

// #120: a subscription's models take the window the user sets on it, over
// the one its backend says (Codex: 272K) — kept through the account's save.
func TestContextSetOnAccount(t *testing.T) {
	signIn(t)
	of := func() int {
		for _, e := range Served() {
			if e.ID == "codex/gpt-5.5" {
				return e.Context
			}
		}
		t.Fatal("no codex/gpt-5.5")
		return 0
	}
	before := of()
	if err := Save(Provider{ID: "codex", Contexts: map[string]int{"*": 872000}}); err != nil {
		t.Fatal(err)
	}
	if got := of(); got != 872000 {
		t.Errorf("set: %d, was %d", got, before)
	}
}

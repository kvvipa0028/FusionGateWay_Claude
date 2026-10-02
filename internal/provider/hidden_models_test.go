package provider

import (
	"slices"
	"testing"

	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/settings"
)

func ids(es []Entry) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.ID)
	}
	return out
}

// Models taken out of one agent's lists one by one: gone from its catalog,
// every other agent's untouched, a new model shown, and the agents told.
func TestHiddenModels(t *testing.T) {
	isolate(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, id := range []string{"relay", "other"} {
		if err := Save(Provider{ID: id, Name: "My " + id, Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m1", "m2"}}); err != nil {
			t.Fatal(err)
		}
	}
	told := 0
	was := catalog.Changed
	catalog.Changed = func() { told++ }
	t.Cleanup(func() { catalog.Changed = was })

	all, _ := CatalogFor("codex")
	if err := SetHiddenModels("Codex", []string{" relay/m2 ", "other/m1", "relay/m2", ""}); err != nil {
		t.Fatal(err)
	}
	if got := settings.Load().HiddenModels["codex"]; !slices.Equal(got, []string{"other/m1", "relay/m2"}) {
		t.Fatalf("saved %v", got)
	}
	shown, hidden := CatalogFor("codex")
	if slices.Contains(ids(shown), "relay/m2") || slices.Contains(ids(shown), "other/m1") ||
		len(shown) != len(all)-2 || !slices.Equal(slices.Sorted(slices.Values(ids(hidden))), []string{"other/m1", "relay/m2"}) {
		t.Fatalf("shown %v hidden %v", ids(shown), ids(hidden))
	}
	// what it may pick among is still every one
	if listed, _ := ListedFor("codex"); len(listed) != len(all) {
		t.Fatalf("listed %v", ids(listed))
	}
	if other, _ := CatalogFor("claude"); len(other) != len(all) {
		t.Fatalf("another agent's list narrowed: %v", ids(other))
	}
	if told != 1 {
		t.Fatalf("agents told %d times", told)
	}
	// the same again changes nothing and tells no one
	if err := SetHiddenModels("codex", []string{"relay/m2", "other/m1"}); err != nil || told != 1 {
		t.Fatalf("%v told %d", err, told)
	}
	// a model new to a provider is shown
	if err := Save(Provider{ID: "relay", Name: "My relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m1", "m2", "m3"}}); err != nil {
		t.Fatal(err)
	}
	if shown, _ := CatalogFor("codex"); !slices.Contains(ids(shown), "relay/m3") {
		t.Fatalf("new model not shown: %v", ids(shown))
	}
	// a renamed provider's hidden models follow it
	if err := Rename("relay", "fast"); err != nil {
		t.Fatal(err)
	}
	if got := settings.Load().HiddenModels["codex"]; !slices.Contains(got, "fast/m2") || slices.Contains(got, "relay/m2") {
		t.Fatalf("after rename %v", got)
	}
	// none puts every one back
	if err := SetHiddenModels("codex", nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := settings.Load().HiddenModels["codex"]; ok {
		t.Fatal("empty list kept")
	}
	if _, hidden := CatalogFor("codex"); len(hidden) != 0 {
		t.Fatalf("still hidden %v", ids(hidden))
	}
}

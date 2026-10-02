package provider

import (
	"slices"
	"testing"
)

// #499: a provider added later can be put first. The order is the list's
// and the one they are tried in: a bare model id goes to the first that
// serves it, and the group magpie finds of a model several serve tries
// the first one first. One added after stays last; a rename keeps its place.
func TestProviderOrder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, id := range []string{"a", "b", "c"} {
		if err := Save(Provider{ID: id, Name: id, Key: "sk-" + id, Chat: "https://" + id + ".example.com/v1", Models: []string{"m"}}); err != nil {
			t.Fatal(err)
		}
	}
	ids := func() []string {
		var out []string
		for _, p := range All() {
			out = append(out, p.ID)
		}
		return out
	}
	if got := ids(); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("as added: %v", got)
	}
	if err := SetOrder([]string{"c", "a"}); err != nil {
		t.Fatal(err)
	}
	if got := ids(); !slices.Equal(got, []string{"c", "a", "b"}) {
		t.Fatalf("after SetOrder(c, a): %v", got)
	}
	if p, _, ok := Resolve("m"); !ok || p.ID != "c" {
		t.Fatalf("m resolves to %q, not c", p.ID)
	}
	g := slices.IndexFunc(autoGroups(providerEntries()), func(g Group) bool { return g.ID == AutoGroupID("m") })
	if g < 0 {
		t.Fatal("no group found of m")
	}
	if ms := autoGroups(providerEntries())[g].Members; !slices.Equal(ms, []string{"c/m", "a/m", "b/m"}) {
		t.Fatalf("group of m tries %v", ms)
	}
	if err := Save(Provider{ID: "d", Name: "d", Key: "sk-d", Chat: "https://d.example.com/v1", Models: []string{"m"}}); err != nil {
		t.Fatal(err)
	}
	if got := ids(); !slices.Equal(got, []string{"c", "a", "b", "d"}) {
		t.Fatalf("d added: %v", got)
	}
	if err := Rename("c", "cc"); err != nil {
		t.Fatal(err)
	}
	if got := ids(); !slices.Equal(got, []string{"cc", "a", "b", "d"}) {
		t.Fatalf("c renamed: %v", got)
	}
	for _, bad := range [][]string{{"a", "a"}, {"nope"}} {
		if err := SetOrder(bad); err == nil {
			t.Fatalf("SetOrder(%v) taken", bad)
		}
	}
}

// Sync's order (MirrorOrder): the other computer's ids first, as it has
// them, then those named here alone (an account only this computer has)
// in the order they have here; none from the other leaves this one's.
func TestMirrorOrder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, id := range []string{"a", "b", "c", "d"} {
		if err := Save(Provider{ID: id, Name: id, Key: "sk-" + id, Chat: "https://" + id + ".example.com/v1"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := SetOrder([]string{"d", "a", "c", "b"}); err != nil {
		t.Fatal(err)
	}
	if err := MirrorOrder(nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := StoredOrder(); !slices.Equal(got, []string{"d", "a", "c", "b"}) {
		t.Fatalf("none from the other: %v", got)
	}
	if err := MirrorOrder([]string{"c", "a", "elsewhere"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := StoredOrder(); !slices.Equal(got, []string{"c", "a", "elsewhere", "d", "b"}) {
		t.Fatalf("mirrored: %v", got)
	}
	var listed []string
	for _, p := range All() {
		listed = append(listed, p.ID)
	}
	if !slices.Equal(listed, []string{"c", "a", "d", "b"}) {
		t.Fatalf("listed: %v", listed)
	}
}

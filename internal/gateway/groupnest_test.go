package gateway

import (
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/provider"
)

// A group in a group routes by its own routing and rules, in its place in
// the group; the trace tells which group each one tried is of.
func TestGroupsInGroups(t *testing.T) {
	fresh(t)
	a, b, c := &keyed{}, &keyed{}, &keyed{}
	serveOn(t, "a", "ka", []string{"m"}, a)
	serveOn(t, "b", "kb", []string{"m"}, b)
	serveOn(t, "c", "kc", []string{"m"}, c)
	if err := provider.SaveGroup(provider.Group{Name: "Fast", Members: []string{"b/m", "c/m"}, Routing: provider.Ordered}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SaveGroup(provider.Group{Name: "Top", Members: []string{"a/m", "group/fast"}, Routing: provider.Ordered}); err != nil {
		t.Fatal(err)
	}
	s := New()
	order := func() string {
		r := s.trace.routes[len(s.trace.routes)-1]
		var out []string
		for _, w := range r.Order {
			out = append(out, w.Provider+"["+strings.Join(w.Via, ",")+"]")
		}
		return strings.Join(out, " ")
	}
	code, body := postAs(t, s, "one", `{"model":"group/top","messages":[{"role":"user","content":"hi"}]}`)
	if code != 200 || !strings.Contains(body, "from ka") {
		t.Fatalf("%d %s", code, body)
	}
	if o := order(); o != "a[] b[fast] c[fast]" {
		t.Fatal(o)
	}
	r := s.trace.routes[len(s.trace.routes)-1]
	if len(r.Group.Subs) != 1 || r.Group.Subs[0].ID != "fast" || r.Group.Subs[0].In != "top" || r.Group.Subs[0].Routing != provider.Ordered {
		t.Fatalf("subs: %+v", r.Group.Subs)
	}

	// the group's rule sends the turn to fast, and fast's own to c
	fast, _, _ := provider.FindGroup("group/fast")
	fast.Rules = []provider.Rule{{Use: "c/m", Tokens: 1}}
	if err := provider.SaveGroup(fast); err != nil {
		t.Fatal(err)
	}
	top, _, _ := provider.FindGroup("group/top")
	top.Rules = []provider.Rule{{Use: "group/fast", Tokens: 1}}
	if err := provider.SaveGroup(top); err != nil {
		t.Fatal(err)
	}
	code, body = postAs(t, s, "two", `{"model":"group/top","messages":[{"role":"user","content":"hello there"}]}`)
	if code != 200 || !strings.Contains(body, "from kc") {
		t.Fatalf("%d %s", code, body)
	}
	if o := order(); o != "c[fast] b[fast] a[]" {
		t.Fatal(o)
	}
	r = s.trace.routes[len(s.trace.routes)-1]
	if r.Rule == nil || r.Rule.Use != "group/fast" || len(r.Nested) != 1 || r.Nested[0].Group != "fast" || r.Nested[0].Rule.Use != "c/m" {
		t.Fatalf("rules: %+v %+v", r.Rule, r.Nested)
	}

	// pooled, fast's models are weighed with a's as one
	top.Rules, top.Routing = nil, provider.Rotate
	if err := provider.SaveGroup(top); err != nil {
		t.Fatal(err)
	}
	postAs(t, s, "three", `{"model":"group/top","messages":[{"role":"user","content":"hey"}]}`)
	if o := order(); !strings.Contains(o, "a[]") || !strings.Contains(o, "b[fast]") || !strings.Contains(o, "c[fast]") {
		t.Fatal(o)
	}
}

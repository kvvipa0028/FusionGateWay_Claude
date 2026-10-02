package provider

import "testing"

// The account in use, as the menu bar's "account in use" card follows it:
// the one the agent is signed in to, unless paused, else the first on.
func TestInUseOf(t *testing.T) {
	for _, c := range []struct {
		ls   []Login
		want string
	}{
		{[]Login{{User: "a", On: true}, {User: "b", Active: true, On: true}}, "b"},
		{[]Login{{User: "b", Active: true, On: true, Paused: true}, {User: "c"}, {User: "d", On: true}}, "d"},
		{[]Login{{User: "c"}, {User: "e", On: true}}, "e"},
		{[]Login{{User: "c"}}, ""},
		{nil, ""},
	} {
		if got := inUseOf(c.ls); got != c.want {
			t.Errorf("%+v: %q, want %q", c.ls, got, c.want)
		}
	}
}

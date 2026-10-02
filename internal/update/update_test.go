package update

import "testing"

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.2.0", "0.1.0", true},
		{"0.1.0", "0.2.0", false},
		{"0.1.0", "0.1.0", false},
		{"v0.10.0", "0.9.9", true},
		{"1.0.0", "1.0.0-rc.1", true},
		{"1.0.0-rc.1", "1.0.0", false},
		{"1.0.0-rc.2", "1.0.0-rc.1", true},
		{"0.2.0", "dev", false},
		{"0.2.0", "0bcb2cc-dirty", false},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestReleased(t *testing.T) {
	for v, want := range map[string]bool{"0.1.0": true, "v1.2.3": true, "dev": false, "0bcb2cc-dirty": false, "v0.1.0-3-g0bcb2cc": false} {
		if Released(v) != want {
			t.Errorf("Released(%q) != %v", v, want)
		}
	}
}

func TestHomebrew(t *testing.T) {
	for exe, want := range map[string]bool{
		"/opt/homebrew/Cellar/magpie/0.1.126/bin/magpie":              true,
		"/home/linuxbrew/.linuxbrew/Cellar/magpie/0.1.126/bin/magpie": true,
		"/usr/local/bin/magpie":                                       false,
		"/Applications/magpie.app/Contents/MacOS/magpie":              false,
	} {
		if Homebrew(exe) != want {
			t.Errorf("%s: %v", exe, !want)
		}
	}
}

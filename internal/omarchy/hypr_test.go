package omarchy

import "testing"

func TestPanelAt(t *testing.T) {
	// 2528x960 at 1.6 is 1580x600 in layout coordinates; Omarchy's bar
	// takes 26 at the top. A second monitor to its right, turned, at 1.
	mons := []hyprMonitor{
		{Width: 2528, Height: 960, Scale: 1.6, Reserved: [4]int{0, 26, 0, 0}},
		{X: 1580, Width: 1920, Height: 1080, Scale: 1, Transform: 1},
	}
	for _, c := range []struct {
		name       string
		cx, cy     int
		x, y, room int
		ok         bool
	}{
		{"middle", 800, 10, 580, 32, 562, true},
		{"right edge", 1570, 10, 1580 - 6 - 440, 32, 562, true},
		{"left edge", 20, 10, 6, 32, 562, true},
		{"turned monitor", 1700, 1500, 1586, 6, 1920 - 12, true},
		{"off every monitor", -5, 10, 0, 0, 0, false},
	} {
		x, y, room, ok := panelAt(440, c.cx, c.cy, mons)
		if x != c.x || y != c.y || room != c.room || ok != c.ok {
			t.Errorf("%s: %d,%d room %d %v; want %d,%d room %d %v", c.name, x, y, room, ok, c.x, c.y, c.room, c.ok)
		}
	}
}

func TestRegexpQuote(t *testing.T) {
	for in, want := range map[string]string{
		"magpie panel": "magpie panel",
		"a.b(c)":       `a\.b\(c\)`,
		"x+y*":         `x\+y\*`,
	} {
		if got := regexpQuote(in); got != want {
			t.Errorf("regexpQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOutside(t *testing.T) {
	at, size := [2]int{1134, 32}, [2]int{440, 520}
	for _, c := range []struct {
		name string
		x, y int
		want bool
	}{
		{"on the panel", 1300, 300, false},
		{"on its top-left corner", 1134, 32, false},
		{"left of it", 900, 300, true},
		{"under it", 1300, 560, true},
		{"on the bar", 1400, 10, false},
		{"in the gap under the bar", 900, 28, true},
	} {
		if got := outside(c.x, c.y, at, size); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

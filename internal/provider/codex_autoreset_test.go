package provider

import (
	"slices"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/settings"
)

// Only the week's window being used up counts: the five hours pass on
// their own, and a window whose end went by says nothing.
func TestWeekUsedUp(t *testing.T) {
	now := time.Now()
	later, gone := now.Add(48*time.Hour), now.Add(-time.Minute)
	five := QuotaWindow{Span: 5 * time.Hour, Used: 100, ResetsAt: &later}
	week := func(used float64, at *time.Time) QuotaWindow {
		return QuotaWindow{Span: 7 * 24 * time.Hour, Used: used, ResetsAt: at}
	}
	for name, c := range map[string]struct {
		ws   []QuotaWindow
		want bool
	}{
		"week used up":        {[]QuotaWindow{five, week(100, &later)}, true},
		"five hours only":     {[]QuotaWindow{five, week(99.5, &later)}, false},
		"week ended":          {[]QuotaWindow{week(100, &gone)}, false},
		"week end not known":  {[]QuotaWindow{week(100, nil)}, false},
		"on-demand, not week": {[]QuotaWindow{{Span: 30 * 24 * time.Hour, Used: 100, ResetsAt: &later, Aside: true}}, false},
	} {
		if got := weekUsedUp(c.ws, now) != nil; got != c.want {
			t.Errorf("%s: %v", name, got)
		}
	}
}

// The setting is kept by account, whatever its case, and turned off again.
func TestSetCodexAutoReset(t *testing.T) {
	signIn(t)
	if err := SetCodexAutoReset(" Me@Example.com ", true); err != nil {
		t.Fatal(err)
	}
	if got := settings.Load().CodexAutoReset; !slices.Equal(got, []string{"me@example.com"}) {
		t.Fatalf("kept %v", got)
	}
	if !CodexAutoReset("ME@example.com") || CodexAutoReset("other@example.com") || CodexAutoReset("") {
		t.Fatal("read back wrong")
	}
	if err := SetCodexAutoReset("me@example.com", false); err != nil {
		t.Fatal(err)
	}
	if CodexAutoReset("me@example.com") || len(settings.Load().CodexAutoReset) != 0 {
		t.Fatal("still on")
	}
}

// A reset spent by itself is kept past a restart: none is spent again
// until the week it was spent in ends.
func TestAutoResetKeptPastRestart(t *testing.T) {
	signIn(t)
	if err := SetCodexAutoReset("me@example.com", true); err != nil {
		t.Fatal(err)
	}
	a := &autoReset
	a.Lock()
	a.path = "" // read afresh
	a.load()
	a.spent["me@example.com"] = autoResetSpent{Until: time.Now().Add(72 * time.Hour), At: time.Now().Add(-time.Hour), Out: ResetOutcome{Code: "reset", Windows: 2}}
	a.save()
	a.path = "" // as a new magpie would
	a.Unlock()
	fakeCodexResets(t, 1, nil) // would say the week is not used up, if asked
	out, err := AutoUseCodexReset(t.Context(), "me@example.com")
	if err != nil || out.Code != "" {
		t.Fatalf("%+v %v", out, err)
	}
	a.Lock()
	_, kept := a.spent["me@example.com"]
	a.Unlock()
	if !kept {
		t.Fatal("what was spent was forgotten")
	}
}

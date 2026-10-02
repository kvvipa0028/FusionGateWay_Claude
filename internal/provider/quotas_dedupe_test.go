package provider

import (
	"testing"
	"time"
)

// A Zhipu plan read with a key of the account ZCode is signed in to is
// shown once, on ZCode's card; another account's plan stays.
func TestPlanOfASignedInAccountShownOnce(t *testing.T) {
	at := func(h int) *time.Time {
		x := time.UnixMilli(1790000000000).Add(time.Duration(h) * time.Hour)
		return &x
	}
	five, week := 5*time.Hour, 7*24*time.Hour
	zcode := SubscriptionQuota{Provider: "zcode", Windows: []QuotaWindow{
		{Name: "5 hours", Span: five, Used: 1, ResetsAt: at(2)},
		{Name: "Weekly", Span: week, Used: 32, ResetsAt: at(90)},
	}}
	same := SubscriptionQuota{Provider: "zhipu", Plan: "Max", Windows: []QuotaWindow{
		{Name: "5 hours", Span: five, Used: 4, ResetsAt: at(2)},
		{Name: "7 days", Span: week, Used: 32, ResetsAt: at(90)},
		{Name: "MCP · Month", Aside: true, ResetsAt: at(300)},
	}}
	other := SubscriptionQuota{Provider: "zai", Plan: "Pro", Windows: []QuotaWindow{
		{Name: "5 hours", Span: five, ResetsAt: at(3)},
		{Name: "7 days", Span: week, ResetsAt: at(90)},
	}}
	unknown := SubscriptionQuota{Provider: "zai", Plan: "Lite", Windows: []QuotaWindow{{Name: "5 hours", Span: five}}}
	got := notShown([]SubscriptionQuota{same, other, unknown}, []SubscriptionQuota{zcode})
	if len(got) != 2 || got[0].Provider != "zai" || got[1].Plan != "Lite" {
		t.Fatalf("%+v", got)
	}
	// ZCode not answering: the plan is the only card there is
	zcode.Error = "timeout"
	if got := notShown([]SubscriptionQuota{same}, []SubscriptionQuota{zcode}); len(got) != 1 {
		t.Fatalf("%+v", got)
	}
}

// Leaving a plan out doesn't touch the list it was left out of, which is
// PlanQuotas' cache.
func TestPlanLeftOutOfACopy(t *testing.T) {
	r := time.Now()
	w := []QuotaWindow{{Span: time.Hour, ResetsAt: &r}}
	plans := []SubscriptionQuota{{Provider: "a", Windows: w}, {Provider: "b"}}
	notShown(plans, []SubscriptionQuota{{Windows: w}})
	if plans[0].Provider != "a" || plans[1].Provider != "b" {
		t.Fatalf("%+v", plans)
	}
}

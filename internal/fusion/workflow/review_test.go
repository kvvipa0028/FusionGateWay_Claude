package workflow

import (
	"strings"
	"testing"
)

func TestReviewDocumentRequiresExplicitBoundedConsistentModelOpinion(t *testing.T) {
	valid := `{"version":1,"verdict":"approve","findings":[]}`
	doc, err := ParseReview(valid)
	if err != nil || doc.Verdict != "approve" || len(doc.Findings) != 0 {
		t.Fatal("explicit advisory opinion", doc, err)
	}
	for _, raw := range []string{
		`passed`, `{"version":1,"verdict":"approve"}`, valid + valid,
		`{"version":1,"verdict":"approve","verdict":"changes_required","findings":[]}`,
		`{"version":1,"verdict":"approve","findings":[],"tests_passed":true}`,
		`{"Version":1,"verdict":"approve","findings":[]}`,
		`{"version":1,"verdict":"approve","findings":[{"id":"a","Severity":"nonblocking","summary":"note"}]}`,
		`{"version":2,"verdict":"approve","findings":[]}`,
		`{"version":1,"verdict":"accepted","findings":[]}`,
		`{"version":1,"verdict":"changes_required","findings":[]}`,
		`{"version":1,"verdict":"approve","findings":[{"id":"a","severity":"blocking","summary":"must fix"}]}`,
		`{"version":1,"verdict":"changes_required","findings":[{"id":"a","severity":"blocking","summary":"must fix"},{"id":"a","severity":"blocking","summary":"duplicate"}]}`,
		`{"version":1,"verdict":"unverified","findings":[{"id":"a","severity":"unknown","summary":"uncertain"}]}`,
		strings.Repeat(" ", 64<<10) + valid,
	} {
		if _, err := ParseReview(raw); err == nil {
			t.Fatal("untrusted/contradictory model opinion accepted", raw[:min(len(raw), 256)])
		}
	}
	for _, raw := range []string{
		`{"version":1,"verdict":"changes_required","findings":[{"id":"a","severity":"blocking","summary":"needs repair"}]}`,
		`{"version":1,"verdict":"approve","findings":[{"id":"a","severity":"nonblocking","summary":"optional improvement"}]}`,
		`{"version":1,"verdict":"unverified","findings":[]}`,
	} {
		if _, err := ParseReview(raw); err != nil {
			t.Fatal("valid opinion rejected", err)
		}
	}
}

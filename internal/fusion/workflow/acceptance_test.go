package workflow

import "testing"

func TestAcceptanceOpinionRequiresExactApprovedCriterionCoverageWithoutHumanAuthority(t *testing.T) {
	valid := `{"version":1,"verdict":"accepted","criteria":[{"index":0,"status":"met","reason":"checked pinned report"},{"index":1,"status":"met","reason":"reviewed actual code"}]}`
	doc, err := ParseAcceptance(valid, 2)
	if err != nil || doc.Verdict != "accepted" || len(doc.Criteria) != 2 {
		t.Fatal("explicit advisory conclusion", doc, err)
	}
	for _, raw := range []string{
		`accepted`, valid + valid,
		`{"version":1,"verdict":"accepted","criteria":[{"index":null,"status":"met","reason":"null index"},{"index":1,"status":"met","reason":"pass"}]}`,
		`{"version":1,"verdict":"accepted","criteria":[{"index":2,"status":"met","reason":"foreign criterion"},{"index":1,"status":"met","reason":"pass"}]}`,
		`{"version":1,"verdict":"accepted","criteria":[]}`,
		`{"version":1,"verdict":"accepted","criteria":[{"index":0,"status":"met","reason":"pass"}]}`,
		`{"version":1,"verdict":"accepted","criteria":[{"index":0,"status":"met","reason":"pass"},{"index":0,"status":"met","reason":"duplicate"}]}`,
		`{"version":1,"verdict":"accepted","criteria":[{"status":"met","reason":"missing index"},{"index":1,"status":"met","reason":"pass"}]}`,
		`{"version":1,"verdict":"accepted","criteria":[{"index":0,"status":"unverified","reason":"no actual HIL"},{"index":1,"status":"met","reason":"pass"}]}`,
		`{"version":1,"verdict":"accepted","criteria":[{"index":0,"status":"not_met","reason":"failed"},{"index":1,"status":"met","reason":"pass"}]}`,
		`{"version":1,"verdict":"rejected","criteria":[{"index":0,"status":"met","reason":"pass"},{"index":1,"status":"met","reason":"pass"}]}`,
		`{"Version":1,"verdict":"accepted","criteria":[]}`,
		`{"version":1,"verdict":"accepted","verdict":"unverified","criteria":[]}`,
		`{"version":1,"verdict":"accepted","criteria":[],"human_accepted":true}`,
	} {
		if _, err := ParseAcceptance(raw, 2); err == nil {
			t.Fatal("missing/contradictory/forged authority accepted", raw)
		}
	}
	for _, raw := range []string{
		`{"version":1,"verdict":"rejected","criteria":[{"index":0,"status":"not_met","reason":"defect"}]}`,
		`{"version":1,"verdict":"unverified","criteria":[{"index":0,"status":"unverified","reason":"actual HIL absent"}]}`,
	} {
		if _, err := ParseAcceptance(raw, 1); err != nil {
			t.Fatal("explicit negative opinion lost", err)
		}
	}
	if _, err := ParseAcceptance(valid, 0); err == nil {
		t.Fatal("no approved criteria accepted")
	}
}

package evidence

import "testing"

func TestJUnitRejectsForgedAggregatesHiddenFailuresAndTruncatedReports(t *testing.T) {
	for _, raw := range []string{
		`<testsuite tests="2"><testcase name="one"/></testsuite>`,
		`<testsuite tests="1" failures="0"><testcase name="one"><failure>failed</failure></testcase></testsuite>`,
		`<testsuite tests="1"><unknown><failure>hidden</failure></unknown><testcase name="one"/></testsuite>`,
		`<testsuite tests="0"/><testsuite tests="0"/>`,
		`<!DOCTYPE testsuite [<!ENTITY x "forged">]><testsuite tests="0"/>`,
		`<testsuite tests="1" tests="1"><testcase name="one"/></testsuite>`,
		`<testsuite tests="1"><testcase name="one"><failure/><skipped/></testcase></testsuite>`,
		`<testsuite tests="2"><testcase name="one"/><testcase name="one"/></testsuite>`,
		`<testsuite tests="-1"/>`,
		`<testsuite tests="0">`,
		`<testsuite xmlns="foreign" tests="0"/>`,
	} {
		if _, err := parseJUnit([]byte(raw)); err == nil {
			t.Fatalf("accepted ambiguous report %s", raw)
		}
	}
}

func TestJUnitAggregatesActualNestedCasesWithoutRequiringWrapperCounters(t *testing.T) {
	raw := `<testsuites><testsuite tests="2"><testcase classname="a" name="one"/><testcase classname="a" name="two"><failure>real failure</failure></testcase></testsuite><testsuite tests="1"><testcase classname="b" name="one"><skipped/></testcase></testsuite></testsuites>`
	c, err := parseJUnit([]byte(raw))
	if err != nil || c.tests != 3 || c.failures != 1 || c.skipped != 1 {
		t.Fatal(c, err)
	}
}

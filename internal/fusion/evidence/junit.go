package evidence

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strconv"
)

var errReport = errors.New("invalid verification report")

type counts struct{ tests, failures, errors, skipped int }
type junitCase struct {
	Name    string   `xml:"name,attr"`
	Class   string   `xml:"classname,attr"`
	Failure []string `xml:"failure"`
	Error   []string `xml:"error"`
	Skipped []string `xml:"skipped"`
}
type junitSuite struct {
	Tests    string       `xml:"tests,attr"`
	Failures string       `xml:"failures,attr"`
	Errors   string       `xml:"errors,attr"`
	Skipped  string       `xml:"skipped,attr"`
	Cases    []junitCase  `xml:"testcase"`
	Suites   []junitSuite `xml:"testsuite"`
}

// Strict structure checks prevent ignored XML children, duplicate counters,
// trailing reports and hidden failures from becoming a passing summary.
func parseJUnit(raw []byte) (counts, error) {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return counts{}, errReport
	}
	d := xml.NewDecoder(bytes.NewReader(raw))
	var stack []string
	root := ""
	elements := 0
	allowed := map[string]map[string]bool{"": {"testsuite": true, "testsuites": true}, "testsuites": {"testsuite": true}, "testsuite": {"testsuite": true, "testcase": true, "properties": true, "system-out": true, "system-err": true}, "testcase": {"failure": true, "error": true, "skipped": true, "system-out": true, "system-err": true, "properties": true}, "properties": {"property": true}}
	for {
		tok, e := d.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return counts{}, errReport
		}
		switch v := tok.(type) {
		case xml.StartElement:
			parent := ""
			if len(stack) > 0 {
				parent = stack[len(stack)-1]
			}
			if !allowed[parent][v.Name.Local] || v.Name.Space != "" || len(stack) > 64 || elements >= 100000 || parent == "" && root != "" {
				return counts{}, errReport
			}
			seen := map[string]bool{}
			for _, a := range v.Attr {
				if a.Name.Space != "" || seen[a.Name.Local] {
					return counts{}, errReport
				}
				seen[a.Name.Local] = true
			}
			if root == "" {
				root = v.Name.Local
			}
			stack = append(stack, v.Name.Local)
			elements++
		case xml.EndElement:
			if len(stack) == 0 {
				return counts{}, errReport
			}
			stack = stack[:len(stack)-1]
		case xml.Directive:
			return counts{}, errReport
		case xml.CharData:
			if len(stack) == 0 && len(bytes.TrimSpace(v)) != 0 {
				return counts{}, errReport
			}
		}
	}
	if root == "" || len(stack) != 0 {
		return counts{}, errReport
	}
	var suite junitSuite
	if xml.Unmarshal(raw, &suite) != nil {
		return counts{}, errReport
	}
	if root == "testsuites" && len(suite.Cases) != 0 {
		return counts{}, errReport
	}
	return suiteCounts(suite, map[string]bool{}, root == "testsuite")
}
func suiteCounts(s junitSuite, seen map[string]bool, require bool) (counts, error) {
	c := counts{}
	for _, t := range s.Cases {
		key := t.Class + "\x00" + t.Name
		if t.Name == "" || seen[key] || len(t.Failure) > 1 || len(t.Error) > 1 || len(t.Skipped) > 1 || len(t.Failure)+len(t.Error)+len(t.Skipped) > 1 {
			return counts{}, errReport
		}
		seen[key] = true
		c.tests++
		c.failures += len(t.Failure)
		c.errors += len(t.Error)
		c.skipped += len(t.Skipped)
	}
	for _, child := range s.Suites {
		x, e := suiteCounts(child, seen, true)
		if e != nil {
			return counts{}, e
		}
		c.tests += x.tests
		c.failures += x.failures
		c.errors += x.errors
		c.skipped += x.skipped
	}
	for _, v := range []struct {
		raw      string
		actual   int
		required bool
	}{{s.Tests, c.tests, require}, {s.Failures, c.failures, false}, {s.Errors, c.errors, false}, {s.Skipped, c.skipped, false}} {
		if v.raw == "" {
			if v.required {
				return counts{}, errReport
			}
			continue
		}
		n, e := strconv.Atoi(v.raw)
		if e != nil || n < 0 || n != v.actual {
			return counts{}, errReport
		}
	}
	return c, nil
}

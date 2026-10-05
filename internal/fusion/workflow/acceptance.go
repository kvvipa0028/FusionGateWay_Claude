package workflow

import (
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"
)

// AcceptanceDocument is model opinion against frozen approved criteria. It
// does not assert hard results or record a human accepting the delivery.
type AcceptanceDocument struct {
	Version  int                `json:"version"`
	Verdict  string             `json:"verdict"`
	Criteria []CriterionOpinion `json:"criteria"`
}
type CriterionOpinion struct {
	Index  *int   `json:"index"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

func ParseAcceptance(raw string, count int) (AcceptanceDocument, error) {
	if count < 1 || count > 128 || len(raw) == 0 || len(raw) > 64<<10 || !utf8.ValidString(raw) {
		return AcceptanceDocument{}, ErrInvalid
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	if !opinionJSONValue(dec, 0, []string{"version", "verdict", "criteria"}, []string{"index", "status", "reason"}) {
		return AcceptanceDocument{}, ErrInvalid
	}
	if _, err := dec.Token(); err != io.EOF {
		return AcceptanceDocument{}, ErrInvalid
	}
	dec = json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	var d AcceptanceDocument
	if dec.Decode(&d) != nil || d.Version != 1 || len(d.Criteria) != count {
		return AcceptanceDocument{}, ErrInvalid
	}
	if d.Verdict != "accepted" && d.Verdict != "rejected" && d.Verdict != "unverified" {
		return AcceptanceDocument{}, ErrInvalid
	}
	seen := map[int]bool{}
	met, failed := 0, 0
	for _, c := range d.Criteria {
		if c.Index == nil || *c.Index < 0 || *c.Index >= count || seen[*c.Index] || !text(c.Reason) || len(c.Reason) > 8192 {
			return AcceptanceDocument{}, ErrInvalid
		}
		seen[*c.Index] = true
		switch c.Status {
		case "met":
			met++
		case "not_met":
			failed++
		case "unverified":
		default:
			return AcceptanceDocument{}, ErrInvalid
		}
	}
	if d.Verdict == "accepted" && met != count || d.Verdict == "rejected" && failed == 0 {
		return AcceptanceDocument{}, ErrInvalid
	}
	return d, nil
}

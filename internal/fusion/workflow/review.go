package workflow

import (
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"
)

// ReviewDocument is advisory model output. It cannot authorize writes, alter
// acceptance criteria, override a hard check, or record human acceptance.
type ReviewDocument struct {
	Version  int             `json:"version"`
	Verdict  string          `json:"verdict"`
	Findings []ReviewFinding `json:"findings"`
}
type ReviewFinding struct {
	ID       string `json:"id"`
	Severity string `json:"severity"`
	Summary  string `json:"summary"`
}

func ParseReview(raw string) (ReviewDocument, error) {
	if len(raw) == 0 || len(raw) > 64<<10 || !utf8.ValidString(raw) {
		return ReviewDocument{}, ErrInvalid
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	if !reviewJSONValue(dec, 0) {
		return ReviewDocument{}, ErrInvalid
	}
	if _, err := dec.Token(); err != io.EOF {
		return ReviewDocument{}, ErrInvalid
	}
	dec = json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	var d ReviewDocument
	if dec.Decode(&d) != nil || d.Version != 1 || d.Findings == nil || len(d.Findings) > 128 {
		return ReviewDocument{}, ErrInvalid
	}
	switch d.Verdict {
	case "approve", "changes_required", "unverified":
	default:
		return ReviewDocument{}, ErrInvalid
	}
	if d.Verdict == "changes_required" && len(d.Findings) == 0 {
		return ReviewDocument{}, ErrInvalid
	}
	seen := map[string]bool{}
	for _, f := range d.Findings {
		if !text(f.ID) || len(f.ID) > 128 || seen[f.ID] || !text(f.Summary) || len(f.Summary) > 8192 || f.Severity != "blocking" && f.Severity != "nonblocking" || d.Verdict == "approve" && f.Severity == "blocking" {
			return ReviewDocument{}, ErrInvalid
		}
		seen[f.ID] = true
	}
	return d, nil
}

// encoding/json otherwise accepts duplicate keys and overwrites old values.
func reviewJSONValue(dec *json.Decoder, depth int) bool {
	if depth > 16 {
		return false
	}
	token, err := dec.Token()
	if err != nil {
		return false
	}
	delim, container := token.(json.Delim)
	if !container {
		return true
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			key, err := dec.Token()
			name, ok := key.(string)
			known := depth == 0 && (name == "version" || name == "verdict" || name == "findings") || depth == 2 && (name == "id" || name == "severity" || name == "summary")
			if err != nil || !ok || !known || seen[name] || !reviewJSONValue(dec, depth+1) {
				return false
			}
			seen[name] = true
		}
	case '[':
		for dec.More() {
			if !reviewJSONValue(dec, depth+1) {
				return false
			}
		}
	default:
		return false
	}
	end, err := dec.Token()
	return err == nil && (delim == '{' && end == json.Delim('}') || delim == '[' && end == json.Delim(']'))
}

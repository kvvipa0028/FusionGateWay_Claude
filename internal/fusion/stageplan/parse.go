package stageplan

import (
	"bytes"
	"encoding/json"
	"io"
)

const MaxPlanBytes = 64 * 1024

func ParsePlan(raw []byte) (Input, error) {
	if len(raw) > MaxPlanBytes || len(raw) == 0 {
		return Input{}, invalid("input_size")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if e := uniqueJSON(d, 0); e != nil {
		return Input{}, e
	}
	if _, e := d.Token(); e != io.EOF {
		return Input{}, invalid("trailing_json")
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var in Input
	if e := d.Decode(&in); e != nil {
		return Input{}, invalid("input_fields")
	}
	if in.SchemaVersion != 1 || in.Revision < 1 || len(in.RequiredRoles) == 0 {
		return Input{}, invalid("input_header")
	}
	seen := map[Role]bool{}
	for _, r := range in.RequiredRoles {
		if !knownRole(r) || seen[r] {
			return Input{}, invalid("required_roles_invalid")
		}
		seen[r] = true
	}
	for _, l := range []Layer{in.Global, in.Project, in.Task} {
		if _, e := expand(l); e != nil {
			return Input{}, e
		}
	}
	return in, nil
}
func uniqueJSON(d *json.Decoder, depth int) error {
	if depth > 32 {
		return invalid("json_depth")
	}
	token, e := d.Token()
	if e != nil {
		return invalid("json_syntax")
	}
	if token == nil {
		return invalid("json_null_setting")
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			token, e = d.Token()
			key, ok := token.(string)
			if e != nil || !ok || seen[key] {
				return invalid("json_duplicate_key")
			}
			seen[key] = true
			if e = uniqueJSON(d, depth+1); e != nil {
				return e
			}
		}
		token, e = d.Token()
		if e != nil || token != json.Delim('}') {
			return invalid("json_syntax")
		}
	case '[':
		for d.More() {
			if e = uniqueJSON(d, depth+1); e != nil {
				return e
			}
		}
		token, e = d.Token()
		if e != nil || token != json.Delim(']') {
			return invalid("json_syntax")
		}
	default:
		return invalid("json_syntax")
	}
	return nil
}

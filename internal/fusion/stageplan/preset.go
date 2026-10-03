package stageplan

import "encoding/json"

// ExpandLayer uses the same group/role rules as Compile and returns independent
// copied role bindings. It validates structure without granting route admission.
func ExpandLayer(in Layer) (Layer, error) {
	raw, e := json.Marshal(in)
	if e != nil || len(raw) > MaxPlanBytes {
		return Layer{}, invalid("input_size")
	}
	var copied Layer
	if json.Unmarshal(raw, &copied) != nil {
		return Layer{}, invalid("input_fields")
	}
	roles, e := expand(copied)
	if e != nil {
		return Layer{}, e
	}
	// A group can share pointers between two roles. JSON-copy the expanded
	// result so editing one role cannot mutate its sibling.
	raw, e = json.Marshal(Layer{Roles: roles})
	if e != nil || len(raw) > MaxPlanBytes {
		return Layer{}, invalid("input_size")
	}
	if json.Unmarshal(raw, &copied) != nil {
		return Layer{}, invalid("input_fields")
	}
	copied.Groups = nil
	return copied, nil
}

// ApplyPreset expands each layer before replacing complete task bindings.
// Explicit task inherit removes a preset binding and resolves lower defaults.
func ApplyPreset(preset, task Layer) (Layer, error) {
	base, e := ExpandLayer(preset)
	if e != nil {
		return Layer{}, e
	}
	overrides, e := ExpandLayer(task)
	if e != nil {
		return Layer{}, e
	}
	if base.Roles == nil {
		base.Roles = map[Role]Binding{}
	}
	for r, b := range overrides.Roles {
		base.Roles[r] = b
	}
	return ExpandLayer(base)
}

package agent

import (
	"reflect"
	"testing"
)

// Every one of Pi's levels is in the map, the model's own mapped and the
// rest null, so Pi's /thinking offers the model's levels alone (#243); off
// is the model's none, and left for Pi to offer on Anthropic's Messages
// API, where it turns thinking off. A model with no known levels gets none.
func TestPiThinkingLevels(t *testing.T) {
	for _, c := range []struct {
		efforts   []string
		anthropic bool
		want      map[string]any
	}{
		{[]string{"low", "high", "max"}, false, map[string]any{
			"off": nil, "minimal": nil, "low": "low", "medium": nil, "high": "high", "xhigh": nil, "max": "max"}},
		{[]string{"low", "medium", "high", "xhigh"}, false, map[string]any{
			"off": nil, "minimal": nil, "low": "low", "medium": "medium", "high": "high", "xhigh": "xhigh", "max": nil}},
		{[]string{"none", "low", "high", "xhigh", "max"}, false, map[string]any{
			"off": "none", "minimal": nil, "low": "low", "medium": nil, "high": "high", "xhigh": "xhigh", "max": "max"}},
		{[]string{"none", "minimal", "low", "medium", "high"}, false, map[string]any{
			"off": "none", "minimal": "minimal", "low": "low", "medium": "medium", "high": "high", "xhigh": nil, "max": nil}},
		// Claude keeps off (thinking disabled) and its xhigh and max
		{[]string{"low", "medium", "high", "xhigh", "max"}, true, map[string]any{
			"minimal": nil, "low": "low", "medium": "medium", "high": "high", "xhigh": "xhigh", "max": "max"}},
		{[]string{"none", "high"}, true, map[string]any{
			"off": "none", "minimal": nil, "low": nil, "medium": nil, "high": "high", "xhigh": nil, "max": nil}},
		{nil, false, nil},
		{nil, true, nil},
	} {
		if got := piThinkingLevels(c.efforts, c.anthropic); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%v (anthropic %v): got %v, want %v", c.efforts, c.anthropic, got, c.want)
		}
	}
}

package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/provider"
)

// A group that names its own levels (#295) lists them on /v1/models, and a
// level its member at hand lacks is sent as the one it has nearest — asked
// in the vendor's own API (a Chat request to a Chat vendor, relayed as it
// is) or translated (a Responses request to it) — so offering more than
// every member has never sends a vendor a level it turns away.
func TestGroupLevelsNamed(t *testing.T) {
	s, a, b := ruled(t)
	if err := provider.SetModelEfforts("a/small", []string{"low", "high", "max"}); err != nil { // deepseek-flash
		t.Fatal(err)
	}
	all := []string{"none", "low", "medium", "high", "xhigh", "max"}
	if err := provider.SetModelEfforts("b/big", all); err != nil { // gpt-6-sol
		t.Fatal(err)
	}
	g, _, _ := provider.FindGroup("group/r")
	g.Levels = all
	if err := provider.SaveGroup(g); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models", nil))
	var list struct {
		Data []struct {
			ID     string `json:"id"`
			Levels []struct {
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("%v: %s", err, rec.Body)
	}
	var listed []string
	for _, m := range list.Data {
		if m.ID == "group/r" {
			for _, l := range m.Levels {
				listed = append(listed, l.Effort)
			}
		}
	}
	if !slices.Equal(listed, all) {
		t.Fatalf("group/r lists %v, want %v", listed, all)
	}

	responses := func(effort string) map[string]any {
		t.Helper()
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"group/r","input":"hi","reasoning":{"effort":"`+effort+`"}}`)))
		if rec.Code != 200 {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		return sentBody(t, a)
	}
	for asked, want := range map[string]string{"none": "low", "low": "low", "medium": "high", "high": "high", "xhigh": "max", "max": "max"} {
		out, r := postOK(t, s, "", chat("hi", nil, 0, `,"reasoning_effort":"`+asked+`"`))
		if sent := sentBody(t, a); !strings.Contains(out, "from ka") || sent["reasoning_effort"] != want || r.Tries[0].Effort != want {
			t.Errorf("chat asked %s: sent %v, traced %+v", asked, sent["reasoning_effort"], r.Tries)
		}
		if sent := responses(asked); sent["model"] != "small" || sent["reasoning_effort"] != want {
			t.Errorf("responses asked %s: sent %v", asked, sent["reasoning_effort"])
		}
	}
	// the member that has them all is sent each as asked
	a.mu.Lock()
	a.fail = 500
	a.mu.Unlock()
	for _, asked := range []string{"medium", "xhigh"} {
		postOK(t, s, "", chat("hi", nil, 0, `,"reasoning_effort":"`+asked+`"`))
		if sent := sentBody(t, b); sent["reasoning_effort"] != asked {
			t.Errorf("big asked %s: sent %v", asked, sent["reasoning_effort"])
		}
	}
}

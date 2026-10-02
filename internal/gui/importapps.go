package gui

import (
	"encoding/json"
	"net/http"

	"github.com/yetone/magpie/internal/provider"
)

// Bringing over the providers another app (CC Switch, Alma) has set up:
// the page lists them, keys masked, and posts back the ones the user
// picked; the keys are read again here, never sent to the page.

func importAppsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/importapps", func(rw http.ResponseWriter, r *http.Request) {
		sources := provider.ImportSources()
		for i := range sources {
			for j := range sources[i].Items {
				p := &sources[i].Items[j].Provider
				p.Key, p.Keys = provider.Mask(p.Key), nil
			}
		}
		writeJSON(rw, sources)
	})
	mux.HandleFunc("POST /api/importapps", func(rw http.ResponseWriter, r *http.Request) {
		var in struct{ Picks []provider.AppPick }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			fail(rw, err)
			return
		}
		added, err := provider.ImportFromApps(in.Picks)
		if err != nil {
			fail(rw, err)
			return
		}
		writeJSON(rw, struct {
			Added []string `json:"added"`
			State any      `json:"state"`
		}{added, providersState()})
	})
}

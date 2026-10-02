package gui

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/yetone/magpie/internal/omarchy"
)

// onOmarchy is whether magpie runs on Omarchy, asked once: its windows then
// take Omarchy's look and its bar's place instead of the tray's.
var onOmarchy = omarchy.Detect()

// omarchyTheme is the Omarchy theme the page draws with, when on Omarchy.
func omarchyTheme() (omarchy.Theme, bool) {
	if !onOmarchy {
		return omarchy.Theme{}, false
	}
	return omarchy.Current()
}

// barIcon is whether magpie can put its icon in Omarchy's bar (the desktop
// app on Omarchy's Hyprland; not a browser's page), and whether it has.
func barIcon(w Windows) map[string]bool {
	ok := onOmarchy && !isWeb(w) && omarchy.Hyprland()
	return map[string]bool{"available": ok, "on": ok && omarchy.WidgetOn()}
}

// omarchyRoutes are Settings' Bar icon: magpie's icon in Omarchy's bar
// beside its own, opening the quick panel, instead of in the tray's drawer.
func omarchyRoutes(mux *http.ServeMux, w Windows) {
	mux.HandleFunc("GET /api/omarchy/widget", func(rw http.ResponseWriter, r *http.Request) {
		writeJSON(rw, barIcon(w))
	})
	mux.HandleFunc("POST /api/omarchy/widget", func(rw http.ResponseWriter, r *http.Request) {
		var in struct{ On bool }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			fail(rw, err)
			return
		}
		if !barIcon(w)["available"] {
			http.Error(rw, "Omarchy's bar isn't here", http.StatusConflict)
			return
		}
		var err error
		if in.On {
			var exe string
			if exe, err = os.Executable(); err == nil {
				err = omarchy.AddWidget(exe)
			}
		} else {
			err = omarchy.RemoveWidget()
		}
		if err != nil {
			fail(rw, err)
			return
		}
		writeJSON(rw, barIcon(w))
	})
	// a widget put in by an older magpie, or one elsewhere, runs this one
	if barIcon(w)["on"] {
		go func() {
			if exe, err := os.Executable(); err == nil {
				if err := omarchy.KeepWidget(exe); err != nil {
					log.Println("omarchy bar icon:", err)
				}
			}
		}()
	}
}

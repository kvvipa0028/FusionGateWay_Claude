// Package fusionassets serves explicitly embedded stage assets and the original
// Magpie stylesheet through a fixed path.
// Its caller must authenticate the request; it never reads a local credential.
package fusionassets

import (
	"embed"
	"net/http"

	magpieassets "github.com/yetone/magpie/internal/gui/assets"
)

//go:embed index.html editor.mjs model.mjs editor.css
var assets embed.FS

func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" || r.URL.RawPath != "" {
			http.Error(w, "Bad Request", 400)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method Not Allowed", 405)
			return
		}
		name := ""
		switch r.URL.Path {
		case "/fusion/", "/fusion/index.html":
			name = "index.html"
		case "/fusion/editor.mjs":
			name = "editor.mjs"
		case "/fusion/model.mjs":
			name = "model.mjs"
		case "/fusion/editor.css":
			name = "editor.css"
		case "/fusion/app.css":
			name = "app.css"
		default:
			http.NotFound(w, r)
			return
		}
		files := assets
		if name == "app.css" {
			files = magpieassets.Styles
		}
		b, err := files.ReadFile(name)
		if err != nil {
			http.Error(w, "Service Unavailable", 503)
			return
		}
		kind := "text/javascript; charset=utf-8"
		if name == "index.html" {
			kind = "text/html; charset=utf-8"
		} else if name == "editor.css" || name == "app.css" {
			kind = "text/css; charset=utf-8"
		}
		w.Header().Set("Content-Type", kind)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
		if r.Method == http.MethodGet {
			w.Write(b)
		}
	})
}

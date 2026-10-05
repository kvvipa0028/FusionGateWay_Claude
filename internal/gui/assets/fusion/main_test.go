package fusionassets

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestMainPageUsesOriginalMagpieHeaderWithOneFusionTab(t *testing.T) {
	source, err := os.ReadFile("../index.html")
	if err != nil {
		t.Fatal(err)
	}
	original := string(source)
	_, tail, ok := strings.Cut(original, `<header class="top"`)
	if !ok {
		t.Fatal("original header")
	}
	header, _, ok := strings.Cut(tail, "</header>")
	if !ok {
		t.Fatal("original header end")
	}
	birdStart := strings.Index(header, `<span class="logo"`)
	birdEnd := strings.Index(header[birdStart:], "</span>") + birdStart + len("</span>")
	w := httptest.NewRecorder()
	Handler().ServeHTTP(w, httptest.NewRequest("GET", "/fusion/", nil))
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, header[birdStart:birdEnd]) {
		t.Fatal("original bird markup not reused")
	}
	for _, view := range []string{"agents", "providers", "gateway", "routing", "usage", "sessions", "library", "plugins", "fusion"} {
		if strings.Count(body, `data-view="`+view+`"`) != 1 {
			t.Fatalf("main navigation missing or duplicate: %s", view)
		}
	}
	if !strings.Contains(body, `id="view-unavailable"`) || !strings.Contains(body, `src="./main.mjs"`) || strings.Count(body, `id="view-fusion"`) != 1 {
		t.Fatal("integrated view missing")
	}
	for _, disallowed := range []string{`src="boot.js"`, `src="app.js"`, `id="agents"`, `id="save" data-t`, "onclick=", "fgm_"} {
		if strings.Contains(body, disallowed) {
			t.Fatal("legacy scripts, duplicate controls or secret in page", disallowed)
		}
	}
	if strings.Contains(w.Header().Get("Content-Security-Policy"), "unsafe-inline") {
		t.Fatal("CSP weakened")
	}
}

func TestMainIntegrationFailsClosedOnUpstreamOrPanelDrift(t *testing.T) {
	raw, err := os.ReadFile("../index.html")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	panel, err := assets.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, broken := range []string{
		strings.Replace(source, `<header class="top"`, `<header class="changed"`, 1),
		strings.Replace(source, `id="nav"`, `id="other-nav"`, 1),
		strings.Replace(source, `data-view="agents" class="on"`, `data-view="agents"`, 1),
		strings.Replace(source, `id="sync"`, `id="other-sync"`, 1),
		source + "</header>",
	} {
		if page, err := integrateMainPage(broken, string(panel)); err == nil || len(page) != 0 {
			t.Fatal("drift served fallback or exposed original page")
		}
	}
	for _, broken := range []string{strings.ReplaceAll(string(panel), "<!-- MAGPIE_HEADER -->", ""), string(panel) + "<!-- MAGPIE_HEADER -->"} {
		if page, err := integrateMainPage(source, broken); err == nil || len(page) != 0 {
			t.Fatal("panel drift accepted")
		}
	}
}

func TestMainScriptStaysInFixedAssetAuthority(t *testing.T) {
	for _, method := range []string{"GET", "HEAD"} {
		w := httptest.NewRecorder()
		Handler().ServeHTTP(w, httptest.NewRequest(method, "/fusion/main.mjs", nil))
		if w.Code != 200 || w.Header().Get("Content-Type") != "text/javascript; charset=utf-8" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("main script", method, w.Code)
		}
		if method == "HEAD" && w.Body.Len() != 0 {
			t.Fatal("HEAD body")
		}
	}
	for _, path := range []string{"/", "/fusion/app.js", "/fusion/boot.js", "/fusion/main.mjs?view=providers", "/fusion/%6dain.mjs"} {
		w := httptest.NewRecorder()
		Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code == 200 {
			t.Fatal("asset authority expanded", path)
		}
	}
}

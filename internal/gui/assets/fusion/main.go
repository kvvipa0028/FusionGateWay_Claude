package fusionassets

import (
	"errors"
	"strings"

	magpieassets "github.com/yetone/magpie/internal/gui/assets"
)

// Derive only the original header, not boot.js/app.js or legacy controls.
// Exact anchors fail closed when upstream changes instead of silently falling
// back to a separately maintained shell or exposing legacy authority.
func mainPage(panel []byte) ([]byte, error) {
	source, err := magpieassets.MainShell.ReadFile("index.html")
	if err != nil {
		return nil, err
	}
	return integrateMainPage(string(source), string(panel))
}

func integrateMainPage(source, panel string) ([]byte, error) {
	const marker = "<!-- MAGPIE_HEADER -->"
	const start = `<header class="top" style="--wails-draggable:drag">`
	const nav = `<nav class="seg" id="nav" style="--wails-draggable:no-drag">`
	if strings.Count(panel, marker) != 1 || strings.Count(source, start) != 1 || strings.Count(source, "</header>") != 1 {
		return nil, errors.New("main shell unavailable")
	}
	_, after, _ := strings.Cut(source, start)
	content, _, _ := strings.Cut(after, "</header>")
	header := start + content + "</header>"
	if strings.Count(header, nav) != 1 || strings.Count(header, "</nav>") != 1 || strings.Count(header, `data-view="agents" class="on"`) != 1 {
		return nil, errors.New("main navigation unavailable")
	}
	header = strings.Replace(header, `data-view="agents" class="on"`, `data-view="agents"`, 1)
	header = strings.Replace(header, "</nav>", `<button data-view="fusion" class="on" aria-current="page" type="button">阶段模型配置</button></nav>`, 1)
	// Same drag rules are applied by editor.css, keeping the strict CSP intact.
	header = strings.ReplaceAll(header, ` style="--wails-draggable:drag"`, "")
	header = strings.ReplaceAll(header, ` style="--wails-draggable:no-drag"`, "")
	for _, id := range []string{"update", "updateHide", "sync", "open", "winclose"} {
		if strings.Count(header, `id="`+id+`"`) != 1 {
			return nil, errors.New("main actions unavailable")
		}
		header = strings.Replace(header, `id="`+id+`"`, `id="`+id+`" disabled`, 1)
	}
	return []byte(strings.Replace(panel, marker, header, 1)), nil
}

//go:build fusion && !nogui && darwin

package gui

import "testing"

func TestFusionNativeNavigationUsesCanonicalLocalStageURLs(t *testing.T) {
	for _, uri := range []string{"wails://localhost/fusion/", "wails://localhost/fusion/index.html"} {
		if !nativeStageNavigationAllowed(uri, true) {
			t.Fatal("local stage navigation denied")
		}
		if nativeStageNavigationAllowed(uri, false) {
			t.Fatal("subframe accepted")
		}
	}
	for _, uri := range []string{"https://evil.example/", "file:///tmp/page.html", "data:text/html,fixture", "about:blank", "wails://evil.example/fusion/", "wails://user@localhost/fusion/", "wails://localhost:12/fusion/", "wails://localhost/fusion/?token=fixture", "wails://localhost/fusion/#fixture", "wails://localhost/api/state", "wails://localhost/fusion/editor.mjs", "wails://localhost/fusion/%69ndex.html", "wails://localhost/fusion/../fusion/index.html", "", "not a URI"} {
		if nativeStageNavigationAllowed(uri, true) {
			t.Fatal("unsafe navigation accepted", uri)
		}
	}
}

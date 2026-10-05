package workspace

import (
	"strings"
	"testing"
)

func TestWriteScopeValidatesPathsAndKeepsPrefixBoundaries(t *testing.T) {
	for _, paths := range [][]string{nil, {"."}, {"src", "tests/unit"}} {
		if !ValidWritePaths(paths) {
			t.Fatal("valid scope refused", paths)
		}
	}
	for _, paths := range [][]string{{""}, {"/tmp"}, {"../src"}, {"tests/../src"}, {"a\\b"}, {"a\n"}, {".git"}, {"src/.claude"}, {"a", "a"}, {string([]byte{255})}, {strings.Repeat("x", 4097)}} {
		if ValidWritePaths(paths) {
			t.Fatal("invalid scope accepted")
		}
	}
	if !WritePathAllowed("tests/unit/a.go", []string{"tests"}) || WritePathAllowed("tests2/a.go", []string{"tests"}) || WritePathAllowed("src/a.go", []string{"tests"}) {
		t.Fatal("scope prefix escaped")
	}
	before := []ArtifactEntry{{Path: ".", Kind: "directory"}, {Path: "src/a.go", Kind: "file", Hash: "old"}}
	after := append([]ArtifactEntry(nil), before...)
	after = append(after, ArtifactEntry{Path: "tests/a_test.go", Kind: "file", Hash: "new"})
	if !ChangesWithin(before, after, []string{"tests"}) {
		t.Fatal("approved test addition refused")
	}
	after[1].Hash = "changed product"
	if ChangesWithin(before, after, []string{"tests"}) {
		t.Fatal("product edit accepted as test-only")
	}
}

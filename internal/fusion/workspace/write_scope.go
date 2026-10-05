package workspace

import (
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Write paths are trusted relative subtrees, not glob patterns or model input.
// Nil preserves the existing unrestricted writable diagnostic contract.
func ValidWritePaths(paths []string) bool {
	if len(paths) > 128 {
		return false
	}
	seen := map[string]bool{}
	for _, p := range paths {
		if p == "" || len(p) > 4096 || !utf8.ValidString(p) || path.IsAbs(p) || path.Clean(p) != p || p == ".." || strings.HasPrefix(p, "../") || strings.ContainsAny(p, "\\:") || strings.ContainsFunc(p, unicode.IsControl) || seen[p] {
			return false
		}
		for _, part := range strings.Split(p, "/") {
			switch part {
			case ".git", ".claude", ".codex", ".grok", ".fusion-dev", ".env":
				return false
			}
		}
		seen[p] = true
	}
	return true
}

func WritePathAllowed(p string, paths []string) bool {
	if !ValidWritePaths(paths) || p == "" || path.IsAbs(p) || path.Clean(p) != p || p == ".." || strings.HasPrefix(p, "../") {
		return false
	}
	for _, root := range paths {
		if root == "." || p == root || strings.HasPrefix(p, root+"/") {
			return true
		}
	}
	return false
}

// ChangesWithin compares actual input/output code entries. It does not grant
// authority to caller-declared manifests; the Factory uses verified artifacts.
func ChangesWithin(before, after []ArtifactEntry, paths []string) bool {
	if len(paths) == 0 || !ValidWritePaths(paths) {
		return false
	}
	for _, c := range treeChanges(before, after) {
		if !WritePathAllowed(c.Path, paths) {
			return false
		}
	}
	return true
}

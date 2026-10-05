// Package magpieassets shares the original Magpie stylesheet and main shell
// without importing the desktop GUI or executing its legacy APIs.
package magpieassets

import "embed"

// Styles contains the original file, not a second copy of its design tokens.
//
//go:embed app.css
var Styles embed.FS

// MainShell is the original homepage source; Fusion derives its header from it.
//
//go:embed index.html
var MainShell embed.FS

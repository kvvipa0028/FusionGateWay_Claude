// Package magpieassets makes the original Magpie stylesheet available to the
// isolated Fusion view without importing the desktop GUI or its legacy APIs.
package magpieassets

import "embed"

// Styles contains the original file, not a second copy of its design tokens.
//
//go:embed app.css
var Styles embed.FS

// Package ui provides the embedded web UI.
package ui

import "embed"

//go:embed all:dist

// FS contains the embedded web UI files.
var FS embed.FS

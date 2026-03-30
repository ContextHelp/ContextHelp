package ui

import "embed"

// FS holds the compiled Vite output, embedded at build time.
// Keep dist/ in sync with the web UI build pipeline before shipping UI changes.
//
//go:embed all:dist
var FS embed.FS

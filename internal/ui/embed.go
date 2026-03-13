package ui

import "embed"

// FS holds the compiled Vite output, embedded at build time.
// The dist/ directory is populated by `make build-ui`.
//
//go:embed all:dist
var FS embed.FS

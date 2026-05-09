package daemon

import "errors"

// ErrNotWired is the sentinel returned by daemon entry points whose
// real implementation has not yet landed. Subsequent tasks
// (T-0313..T-0321) replace each surface that returns this error with
// real wiring; until they do, callers can grep on the sentinel to
// triage "still pending" reports without inspecting individual
// subcommand bodies.
//
// Exported because cmd/ctxt/cmd/lateral.go re-uses it to keep the
// CLI's user-visible error stable across the wiring window.
var ErrNotWired = errors.New("lateral: daemon not yet wired (track lateral-daemon-wiring-20260509)")

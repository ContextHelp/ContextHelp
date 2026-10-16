// Package tui is the Bubble Tea terminal interface behind `ctxt tui`.
package tui

import (
	tea "charm.land/bubbletea/v2"
)

// StartOpts is the state the TUI launches with.
type StartOpts struct {
	// Profile is the active focus profile, already resolved by the caller
	// (--profile over profile.default); "" means none. The TUI shows it
	// verbatim and never resolves a profile of its own.
	Profile string
	// InitialQuery pre-populates the search input and triggers a search on start.
	InitialQuery string
	// InitialObjectID opens the preview pane focused on this object ID on start.
	InitialObjectID string
}

// Run starts the Bubble Tea TUI program with opts. It blocks until the
// user quits. adapter must not be nil.
func Run(adapter ServiceAdapter, opts StartOpts) error {
	_, err := tea.NewProgram(New(adapter, opts)).Run()
	return err
}

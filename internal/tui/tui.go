package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/ideacrafterslabs/ctxt/internal/config"
)

// StartOpts controls optional initial state when launching the TUI.
type StartOpts struct {
	// InitialQuery pre-populates the search input and triggers a search on start.
	InitialQuery string
	// InitialObjectID opens the preview pane focused on this object ID on start.
	InitialObjectID string
}

// Run starts the Bubble Tea TUI program. It blocks until the user quits.
// adapter must not be nil. cfg may be nil (uses zero-value defaults).
func Run(adapter ServiceAdapter, cfg *config.Config) error {
	return RunWithOpts(adapter, cfg, StartOpts{})
}

// RunWithOpts starts the TUI with optional initial state (pre-filled query or
// focused object). adapter must not be nil. cfg may be nil.
func RunWithOpts(adapter ServiceAdapter, cfg *config.Config, opts StartOpts) error {
	m := New(adapter, cfg)
	m.initialQuery = opts.InitialQuery
	m.initialObjectID = opts.InitialObjectID
	if opts.InitialObjectID != "" {
		m.activePane = PanePreview
	}
	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}

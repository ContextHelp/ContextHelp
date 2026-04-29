package panes

import tea "charm.land/bubbletea/v2"

// Pane is the interface satisfied by every TUI pane (search, preview, graph).
// Panes are value types passed by pointer; Update returns the updated pane and
// any commands to run — following Bubble Tea's immutable model convention.
type Pane interface {
	// Update processes one Bubble Tea message and returns the updated pane and
	// any side-effect command to fire next.
	Update(msg tea.Msg) (Pane, tea.Cmd)

	// View renders the pane into a string constrained to w columns and h rows.
	View(width, height int) string

	// Focus marks the pane as active (typically changes border color).
	Focus()

	// Blur marks the pane as inactive.
	Blur()

	// IsFocused reports whether the pane currently has focus.
	IsFocused() bool
}

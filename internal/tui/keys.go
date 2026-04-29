package tui

import "charm.land/bubbles/v2/key"

// KeyMap holds all key bindings for the TUI.
type KeyMap struct {
	Search       key.Binding
	NextPane     key.Binding
	PrevPane     key.Binding
	Capture      key.Binding
	Compose      key.Binding
	Quit         key.Binding
	Help         key.Binding
	CopyID       key.Binding
	SubviewCycle key.Binding
	Refresh      key.Binding
}

// DefaultKeyMap returns the application key map with default bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Search: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "search"),
		),
		NextPane: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "next pane"),
		),
		PrevPane: key.NewBinding(
			key.WithKeys("shift+tab"),
			key.WithHelp("shift+tab", "prev pane"),
		),
		Capture: key.NewBinding(
			key.WithKeys("ctrl+n"),
			key.WithHelp("ctrl+n", "capture"),
		),
		Compose: key.NewBinding(
			key.WithKeys("ctrl+shift+n"),
			key.WithHelp("ctrl+shift+n", "compose"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		CopyID: key.NewBinding(
			key.WithKeys("y"),
			key.WithHelp("y", "copy ID"),
		),
		SubviewCycle: key.NewBinding(
			key.WithKeys("d", "t", "m", "s"),
			key.WithHelp("d/t/m/s", "subview"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("ctrl+r"),
			key.WithHelp("ctrl+r", "refresh"),
		),
	}
}

// ShortHelp implements help.KeyMap — returned in the compact status bar hint.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Search, k.Capture, k.Compose, k.Help, k.Quit}
}

// FullHelp implements help.KeyMap — returned when the user presses `?`.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Search, k.NextPane, k.PrevPane, k.Refresh},
		{k.Capture, k.Compose, k.CopyID, k.SubviewCycle},
		{k.Help, k.Quit},
	}
}

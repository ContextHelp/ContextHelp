package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ideacrafterslabs/ctxt/internal/config"
)

// Run starts the Bubble Tea TUI program. It blocks until the user quits.
// adapter must not be nil. cfg may be nil (uses zero-value defaults).
func Run(adapter ServiceAdapter, cfg *config.Config) error {
	m := New(adapter, cfg)
	opts := []tea.ProgramOption{
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	}
	p := tea.NewProgram(m, opts...)
	_, err := p.Run()
	return err
}

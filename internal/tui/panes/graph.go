package panes

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ideacrafterslabs/ctxt/internal/tui/types"
)

// GraphPane is a placeholder for the future knowledge graph visualization.
type GraphPane struct {
	focused bool
	theme   types.Theme
}

// NewGraphPane constructs a GraphPane.
func NewGraphPane(theme types.Theme) *GraphPane {
	return &GraphPane{theme: theme}
}

// Focus implements Pane.
func (g *GraphPane) Focus() { g.focused = true }

// Blur implements Pane.
func (g *GraphPane) Blur() { g.focused = false }

// IsFocused implements Pane.
func (g *GraphPane) IsFocused() bool { return g.focused }

// Update implements Pane.
func (g *GraphPane) Update(_ tea.Msg) (Pane, tea.Cmd) {
	return g, nil
}

// View implements Pane.
func (g *GraphPane) View(width, height int) string {
	borderStyle := g.theme.Blurred
	if g.focused {
		borderStyle = g.theme.Focused
	}

	content := lipgloss.NewStyle().
		Width(width - 4).
		Height(height - 4).
		Align(lipgloss.Center, lipgloss.Center).
		Render("graph view\ncoming soon")

	return borderStyle.Width(width - 2).Height(height - 2).Render(content)
}

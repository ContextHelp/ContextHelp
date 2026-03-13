package panes

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/ideacrafterslabs/ctxt/internal/tui/types"
)

// RenderStatusBar renders a single-row status bar padded exactly to `width` columns.
func RenderStatusBar(width int, jobCount int, profile string, hint string, theme types.Theme) string {
	jobStr := jobIndicator(jobCount)
	centerStr := fmt.Sprintf(" profile: %s ", profile)
	rightStr := fmt.Sprintf(" %s ", hint)

	usedWidth := lipgloss.Width(jobStr) + lipgloss.Width(centerStr) + lipgloss.Width(rightStr)
	padWidth := width - usedWidth
	if padWidth < 0 {
		padWidth = 0
	}

	bar := jobStr +
		centerStr +
		lipgloss.NewStyle().Width(padWidth).Render("") +
		rightStr

	return theme.StatusBar.Width(width).Render(bar)
}

func jobIndicator(count int) string {
	if count == 0 {
		return " no jobs "
	}
	return fmt.Sprintf(" %d job(s) ", count)
}

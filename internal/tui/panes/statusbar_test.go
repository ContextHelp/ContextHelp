package panes_test

import (
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/ideacrafterslabs/ctxt/internal/tui"
	"github.com/ideacrafterslabs/ctxt/internal/tui/panes"
	"github.com/stretchr/testify/assert"
)

func TestStatusBarWidth(t *testing.T) {
	theme := tui.DefaultTheme()

	for _, width := range []int{80, 100, 120, 160} {
		bar := panes.RenderStatusBar(width, 3, "engineer", "? for help", theme)
		// lipgloss.Width strips ANSI codes and counts display columns.
		rendered := lipgloss.Width(bar)
		assert.Equal(t, width, rendered,
			"status bar width mismatch at terminal width %d (got %d)", width, rendered)
	}
}

func TestStatusBarContainsProfile(t *testing.T) {
	theme := tui.DefaultTheme()
	bar := panes.RenderStatusBar(120, 0, "researcher", "? for help", theme)
	assert.Contains(t, bar, "researcher")
}

func TestStatusBarJobCount(t *testing.T) {
	theme := tui.DefaultTheme()
	bar := panes.RenderStatusBar(120, 5, "default", "? for help", theme)
	assert.Contains(t, bar, "5")
}

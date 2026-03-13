package tui_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/tui"
	"github.com/stretchr/testify/assert"
)

func TestDefaultThemeNonZero(t *testing.T) {
	theme := tui.DefaultTheme()

	// Each style must be non-empty (lipgloss styles are zero-value structs
	// with no rendering until Render() is called; we verify they are non-zero
	// by rendering a test string and checking output is non-empty).
	assert.NotEmpty(t, theme.Focused.Render("x"))
	assert.NotEmpty(t, theme.Blurred.Render("x"))
	assert.NotEmpty(t, theme.Selected.Render("x"))
	assert.NotEmpty(t, theme.StatusBar.Render("x"))
	assert.NotEmpty(t, theme.Tag.Render("x"))
	assert.NotEmpty(t, theme.Title.Render("x"))
	assert.NotEmpty(t, theme.Muted.Render("x"))
	assert.NotEmpty(t, theme.Error.Render("x"))
	assert.NotEmpty(t, theme.Success.Render("x"))
}

func TestBreakpointConstants(t *testing.T) {
	assert.Equal(t, 100, tui.BreakpointStacked)
	assert.Equal(t, 160, tui.BreakpointWide)
	assert.Less(t, tui.BreakpointStacked, tui.BreakpointWide)
}

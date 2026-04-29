package tui

import (
	"charm.land/lipgloss/v2"
	"github.com/ideacrafterslabs/ctxt/internal/tui/types"
)

// Layout breakpoints (terminal column counts).
const (
	BreakpointStacked = types.BreakpointStacked
	BreakpointWide    = types.BreakpointWide
)

// Theme holds all lipgloss styles used by the TUI. Re-exported from types.
type Theme = types.Theme

// DefaultTheme returns the default TUI color theme.
func DefaultTheme() Theme {
	return types.DefaultTheme()
}

// Keep unexported color vars for any direct use within this package.
var (
	_ = lipgloss.Color("#5C5CFF") // colorPrimary — defined in types
)

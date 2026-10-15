package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Each binding handleKey dispatches, and its effect on the model.
func TestHandleKeyBindings(t *testing.T) {
	press := func(code rune, mod tea.KeyMod) tea.KeyMsg {
		return tea.KeyPressMsg{Code: code, Mod: mod}
	}
	for _, tc := range []struct {
		name     string
		start    PaneID
		key      tea.KeyMsg
		wantQuit bool
		wantCmd  bool
		check    func(t *testing.T, m Model)
	}{
		{name: "q quits", key: press('q', 0), wantQuit: true},
		{name: "ctrl+c quits", key: press('c', tea.ModCtrl), wantQuit: true},
		{name: "? toggles help", key: press('?', 0), check: func(t *testing.T, m Model) {
			if !m.showHelp {
				t.Error("help not shown")
			}
		}},
		{name: "ctrl+n opens capture", key: press('n', tea.ModCtrl), check: func(t *testing.T, m Model) {
			if !m.captureModal.IsActive() || m.composeModal.IsActive() {
				t.Error("want capture modal only")
			}
		}},
		{name: "ctrl+shift+n opens compose", key: press('n', tea.ModCtrl|tea.ModShift), check: func(t *testing.T, m Model) {
			if !m.composeModal.IsActive() || m.captureModal.IsActive() {
				t.Error("want compose modal only")
			}
		}},
		{name: "tab cycles forward", start: PaneSearch, key: press(tea.KeyTab, 0), check: wantPane(PanePreview)},
		{name: "shift+tab cycles backward", start: PaneSearch, key: press(tea.KeyTab, tea.ModShift), check: wantPane(PaneGraph)},
		{name: "/ focuses search", start: PaneGraph, key: press('/', 0), check: wantPane(PaneSearch)},
		{name: "ctrl+r refreshes jobs", start: PanePreview, key: press('r', tea.ModCtrl), wantCmd: true, check: wantPane(PanePreview)},
		{name: "unbound key reaches the focused pane", start: PaneGraph, key: press('x', 0), check: func(t *testing.T, m Model) {
			if m.showHelp || m.captureModal.IsActive() || m.composeModal.IsActive() || m.activePane != PaneGraph {
				t.Error("unbound key changed app state")
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New(nil, StartOpts{})
			m.setActivePane(tc.start)

			next, cmd := m.handleKey(tc.key)
			got, ok := next.(Model)
			if !ok {
				t.Fatalf("handleKey returned %T, want Model", next)
			}
			if tc.wantQuit {
				if cmd == nil || cmd() != tea.Quit() {
					t.Fatal("want tea.Quit")
				}
				return
			}
			if tc.wantCmd && cmd == nil {
				t.Fatal("want a command")
			}
			if tc.check != nil {
				tc.check(t, got)
			}
		})
	}
}

func wantPane(id PaneID) func(t *testing.T, m Model) {
	return func(t *testing.T, m Model) {
		t.Helper()
		if m.activePane != id {
			t.Errorf("active pane = %d, want %d", m.activePane, id)
		}
	}
}

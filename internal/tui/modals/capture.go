package modals

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ideacrafterslabs/ctxt/internal/tui/types"
)

// CaptureModal is a full-screen overlay for capturing new content.
type CaptureModal struct {
	textarea textarea.Model
	adapter  types.ServiceAdapter
	theme    types.Theme
	active   bool
	pipeline string
}

// NewCaptureModal constructs a CaptureModal.
func NewCaptureModal(adapter types.ServiceAdapter, theme types.Theme) *CaptureModal {
	ta := textarea.New()
	ta.Placeholder = "Paste or type content to capture..."
	ta.ShowLineNumbers = false
	ta.CharLimit = 100000
	ta.SetWidth(70)
	ta.SetHeight(15)

	return &CaptureModal{
		textarea: ta,
		adapter:  adapter,
		theme:    theme,
	}
}

// Open activates the modal and focuses the textarea.
func (m *CaptureModal) Open() {
	m.active = true
	m.textarea.Focus()
}

// IsActive reports whether the modal is currently open.
func (m *CaptureModal) IsActive() bool { return m.active }

// SetContent sets the textarea value (useful in tests).
func (m *CaptureModal) SetContent(s string) {
	m.textarea.SetValue(s)
}

// Submit closes the modal and returns a tea.Cmd that fires CaptureSubmittedMsg.
// Returns nil if the textarea is empty.
func (m *CaptureModal) Submit() tea.Cmd {
	content := m.textarea.Value()
	if content == "" {
		return nil
	}
	m.active = false
	m.textarea.Reset()

	adapter := m.adapter
	pipeline := m.pipeline
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		jobID, err := adapter.Analyze(ctx, types.AnalyzeRequest{
			Content:  content,
			Type:     "text",
			Pipeline: pipeline,
		})
		if err != nil {
			return types.ErrorMsg{Err: err}
		}
		return types.CaptureSubmittedMsg{JobID: jobID}
	}
}

// Update processes Bubble Tea messages.
func (m *CaptureModal) Update(msg tea.Msg) (*CaptureModal, tea.Cmd) {
	if !m.active {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEsc:
			m.active = false
			m.textarea.Reset()
			return m, nil
		case tea.KeyCtrlS:
			return m, m.Submit()
		}
		if msg.Type == tea.KeyEnter && msg.Alt {
			return m, m.Submit()
		}
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

// View renders the modal overlay centred in a terminal of the given dimensions.
func (m *CaptureModal) View(width, height int) string {
	if !m.active {
		return ""
	}

	modalWidth := width - 8
	if modalWidth > 100 {
		modalWidth = 100
	}
	if modalWidth < 40 {
		modalWidth = 40
	}

	m.textarea.SetWidth(modalWidth - 4)

	title := m.theme.Title.Render(" Capture — Ctrl+S to submit, Esc to cancel ")
	body := m.textarea.View()
	hint := m.theme.Muted.Render("Pipeline: auto-detect | Ctrl+S or Alt+Enter to submit")

	modal := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#5C5CFF")).
		Padding(1, 2).
		Width(modalWidth).
		Render(lipgloss.JoinVertical(lipgloss.Left, title, body, hint))

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}

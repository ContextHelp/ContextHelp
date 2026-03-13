package modals

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/tui/types"
)

var compositionTypes = []string{"brief", "plan", "summary", "draft"}

type typeItem string

func (t typeItem) Title() string       { return string(t) }
func (t typeItem) Description() string { return "" }
func (t typeItem) FilterValue() string { return string(t) }

// ComposeModal is a full-screen modal for initiating composition.
type ComposeModal struct {
	typeList    list.Model
	tagInput    textinput.Model
	adapter     types.ServiceAdapter
	theme       types.Theme
	active      bool
	activeField int
	compType    string
}

// NewComposeModal constructs a ComposeModal.
func NewComposeModal(adapter types.ServiceAdapter, theme types.Theme) *ComposeModal {
	items := make([]list.Item, len(compositionTypes))
	for i, t := range compositionTypes {
		items[i] = typeItem(t)
	}

	l := list.New(items, list.NewDefaultDelegate(), 40, 8)
	l.Title = "Composition Type"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)

	ti := textinput.New()
	ti.Placeholder = "Filter by tag (optional)"
	ti.CharLimit = 64

	return &ComposeModal{
		typeList: l,
		tagInput: ti,
		adapter:  adapter,
		theme:    theme,
		compType: "summary",
	}
}

// Open activates the modal.
func (m *ComposeModal) Open() {
	m.active = true
	m.activeField = 0
}

// IsActive reports whether the modal is currently open.
func (m *ComposeModal) IsActive() bool { return m.active }

// SetType sets the selected composition type (useful in tests).
func (m *ComposeModal) SetType(t string) { m.compType = t }

// Submit closes the modal and returns a tea.Cmd that fires ComposeResultMsg.
func (m *ComposeModal) Submit() tea.Cmd {
	if item, ok := m.typeList.SelectedItem().(typeItem); ok {
		m.compType = string(item)
	}
	compType := m.compType
	tag := m.tagInput.Value()
	m.active = false

	adapter := m.adapter
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		filter := storage.ObjectFilter{Limit: 20}
		if tag != "" {
			filter.Tag = tag
		}
		objs, _, err := adapter.ListObjects(ctx, filter)
		if err != nil {
			return types.ErrorMsg{Err: err}
		}
		markdown, err := adapter.Compose(ctx, objs, compType)
		if err != nil {
			return types.ErrorMsg{Err: err}
		}
		return types.ComposeResultMsg{Markdown: markdown}
	}
}

// Update processes Bubble Tea messages.
func (m *ComposeModal) Update(msg tea.Msg) (*ComposeModal, tea.Cmd) {
	if !m.active {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEsc:
			m.active = false
			return m, nil
		case tea.KeyCtrlS:
			return m, m.Submit()
		case tea.KeyTab:
			m.activeField = (m.activeField + 1) % 2
			if m.activeField == 1 {
				m.tagInput.Focus()
			} else {
				m.tagInput.Blur()
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	if m.activeField == 0 {
		m.typeList, cmd = m.typeList.Update(msg)
	} else {
		m.tagInput, cmd = m.tagInput.Update(msg)
	}
	return m, cmd
}

// View renders the compose modal.
func (m *ComposeModal) View(width, height int) string {
	if !m.active {
		return ""
	}

	modalWidth := width - 12
	if modalWidth > 90 {
		modalWidth = 90
	}
	if modalWidth < 50 {
		modalWidth = 50
	}

	m.typeList.SetWidth(modalWidth - 4)
	title := m.theme.Title.Render(" Compose — select type and press Ctrl+S ")
	typeView := m.typeList.View()
	tagView := m.tagInput.View()
	hint := m.theme.Muted.Render("Tab to switch field | Ctrl+S to compose | Esc to cancel")

	modal := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#5C5CFF")).
		Padding(1, 2).
		Width(modalWidth).
		Render(lipgloss.JoinVertical(lipgloss.Left, title, typeView, tagView, hint))

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}

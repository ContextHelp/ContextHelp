package tui

import (
	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/tui/modals"
	"github.com/ideacrafterslabs/ctxt/internal/tui/panes"
	"github.com/ideacrafterslabs/ctxt/internal/tui/types"
)

// PaneID identifies which pane is currently focused.
type PaneID int

const (
	PaneSearch  PaneID = iota
	PanePreview PaneID = iota
	PaneGraph   PaneID = iota
)

// Model is the root Bubble Tea application model.
type Model struct {
	width, height   int
	activePane      PaneID
	search          *panes.SearchPane
	preview         *panes.PreviewPane
	graph           *panes.GraphPane
	captureModal    *modals.CaptureModal
	composeModal    *modals.ComposeModal
	jobCount        int
	profile         string
	adapter         ServiceAdapter
	theme           Theme
	keys            KeyMap
	jobs            []*storage.Job
	help            help.Model
	showHelp        bool
	errorMsg        string
	initialQuery    string
	initialObjectID string
}

// New constructs the root model. cfg may be nil (uses zero-value defaults).
func New(adapter ServiceAdapter, cfg *config.Config) Model {
	theme := DefaultTheme()
	profile := ""
	if cfg != nil {
		profile = cfg.Profile.Default
	}

	return Model{
		adapter:      adapter,
		theme:        theme,
		keys:         DefaultKeyMap(),
		search:       panes.NewSearchPane(adapter, theme),
		preview:      panes.NewPreviewPane(theme),
		graph:        panes.NewGraphPane(theme),
		captureModal: modals.NewCaptureModal(adapter, theme),
		composeModal: modals.NewComposeModal(adapter, theme),
		activePane:   PaneSearch,
		profile:      profile,
		help:         help.New(),
	}
}

// Init implements tea.Model. Starts the job poller and fires any initial
// query/object load requested via RunWithOpts.
func (m Model) Init() tea.Cmd {
	m.search.Focus()
	cmds := []tea.Cmd{PollJobsCmd(m.adapter)}
	if m.initialQuery != "" {
		cmds = append(cmds, SearchCmd(m.adapter, m.initialQuery))
	}
	if m.initialObjectID != "" {
		cmds = append(cmds, LoadObjectCmd(m.adapter, m.initialObjectID))
	}
	return tea.Batch(cmds...)
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Modals get priority when active.
	if m.captureModal.IsActive() {
		updated, cmd := m.captureModal.Update(msg)
		m.captureModal = updated
		return m, cmd
	}
	if m.composeModal.IsActive() {
		updated, cmd := m.composeModal.Update(msg)
		m.composeModal = updated
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case types.JobsUpdatedMsg:
		m.jobs = msg.Jobs
		m.jobCount = len(msg.Jobs)
		m.errorMsg = ""
		return m, PollJobsCmd(m.adapter)

	case types.SearchResultsMsg:
		p, cmd := m.search.Update(msg)
		m.search = p.(*panes.SearchPane)
		return m, cmd

	case types.ObjectLoadedMsg:
		p, cmd := m.preview.Update(msg)
		m.preview = p.(*panes.PreviewPane)
		return m, cmd

	case types.CaptureSubmittedMsg:
		m.errorMsg = ""
		return m, PollJobsCmd(m.adapter)

	case types.ComposeResultMsg:
		return m, nil

	case types.ErrorMsg:
		m.errorMsg = msg.Err.Error()
		return m, nil
	}

	return m.updateFocusedPane(msg)
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case m.keys.Quit.Enabled() && (msg.String() == "q" || msg.Type == tea.KeyCtrlC):
		return m, tea.Quit

	case m.keys.Help.Enabled() && msg.String() == "?":
		m.showHelp = !m.showHelp
		return m, nil

	case m.keys.Capture.Enabled() && msg.Type == tea.KeyCtrlN:
		m.captureModal.Open()
		return m, nil

	case m.keys.Compose.Enabled() && msg.String() == "ctrl+shift+n":
		m.composeModal.Open()
		return m, nil

	case m.keys.NextPane.Enabled() && msg.Type == tea.KeyTab:
		m.cyclePaneForward()
		return m, nil

	case m.keys.PrevPane.Enabled() && msg.Type == tea.KeyShiftTab:
		m.cyclePaneBackward()
		return m, nil

	case m.keys.Search.Enabled() && msg.String() == "/":
		m.setActivePane(PaneSearch)
		return m, nil

	case m.keys.Refresh.Enabled() && msg.Type == tea.KeyCtrlR:
		return m, PollJobsCmd(m.adapter)
	}

	return m.updateFocusedPane(msg)
}

func (m Model) updateFocusedPane(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.activePane {
	case PaneSearch:
		updated, cmd := m.search.Update(msg)
		m.search = updated.(*panes.SearchPane)

		if km, ok := msg.(tea.KeyMsg); ok && (km.Type == tea.KeyEnter || km.String() == " ") {
			if obj := m.search.SelectedObject(); obj != nil {
				return m, tea.Batch(cmd, LoadObjectCmd(m.adapter, obj.ID))
			}
		}
		return m, cmd

	case PanePreview:
		updated, cmd := m.preview.Update(msg)
		m.preview = updated.(*panes.PreviewPane)
		return m, cmd

	case PaneGraph:
		updated, cmd := m.graph.Update(msg)
		m.graph = updated.(*panes.GraphPane)
		return m, cmd
	}
	return m, nil
}

func (m *Model) setActivePane(id PaneID) {
	m.activePane = id
	m.search.Blur()
	m.preview.Blur()
	m.graph.Blur()
	switch id {
	case PaneSearch:
		m.search.Focus()
	case PanePreview:
		m.preview.Focus()
	case PaneGraph:
		m.graph.Focus()
	}
}

func (m *Model) cyclePaneForward() {
	next := (m.activePane + 1) % 3
	m.setActivePane(next)
}

func (m *Model) cyclePaneBackward() {
	prev := (m.activePane + 2) % 3
	m.setActivePane(prev)
}

// View implements tea.Model.
func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	if m.captureModal.IsActive() {
		return m.captureModal.View(m.width, m.height)
	}
	if m.composeModal.IsActive() {
		return m.composeModal.View(m.width, m.height)
	}

	statusH := 1
	helpH := 0
	if m.showHelp {
		helpH = 4
	}
	panesH := m.height - statusH - helpH

	var body string
	switch {
	case m.width < BreakpointStacked:
		body = m.renderStacked(m.width, panesH)
	case m.width < BreakpointWide:
		body = m.renderSplit(m.width, panesH)
	default:
		body = m.renderThreePanes(m.width, panesH)
	}

	hint := "? help"
	if m.showHelp {
		hint = m.help.View(m.keys)
	}

	statusBar := panes.RenderStatusBar(m.width, m.jobCount, m.profile, hint, m.theme)

	if m.errorMsg != "" {
		statusBar = m.theme.Error.Width(m.width).Render("Error: " + m.errorMsg)
	}

	return lipgloss.JoinVertical(lipgloss.Left, body, statusBar)
}

func (m Model) renderStacked(w, h int) string {
	halfH := h / 2
	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.search.View(w, halfH),
		m.preview.View(w, h-halfH),
	)
}

func (m Model) renderSplit(w, h int) string {
	leftW := w * 40 / 100
	rightW := w - leftW
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.search.View(leftW, h),
		m.preview.View(rightW, h),
	)
}

func (m Model) renderThreePanes(w, h int) string {
	leftW := w * 30 / 100
	midW := w * 40 / 100
	rightW := w - leftW - midW
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.search.View(leftW, h),
		m.preview.View(midW, h),
		m.graph.View(rightW, h),
	)
}

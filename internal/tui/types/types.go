// Package types contains shared types used by all tui subpackages.
// It must NOT import any other tui subpackage to avoid import cycles.
package types

import (
	"context"

	"charm.land/lipgloss/v2"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Layout breakpoints (terminal column counts).
const (
	BreakpointStacked = 100
	BreakpointWide    = 160
)

// AnalyzeRequest passed to ServiceAdapter.Analyze.
type AnalyzeRequest struct {
	Content  string
	Type     string
	Pipeline string
	Source   string
}

// ServiceAdapter is the narrow interface the TUI uses to talk to the backend.
type ServiceAdapter interface {
	Search(ctx context.Context, query string, limit int) ([]*storage.KnowledgeObject, error)
	GetObject(ctx context.Context, id string) (*storage.KnowledgeObject, error)
	ListObjects(ctx context.Context, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, int, error)
	ListJobs(ctx context.Context, filter storage.JobFilter) ([]*storage.Job, int, error)
	RetryJob(ctx context.Context, id string) error
	CancelJob(ctx context.Context, id string) error
	Analyze(ctx context.Context, req AnalyzeRequest) (string, error)
	Compose(ctx context.Context, objects []*storage.KnowledgeObject, t string) (string, error)
}

// --- Message types ---

// SearchResultsMsg is emitted when a search completes.
type SearchResultsMsg struct {
	Results []*storage.KnowledgeObject
}

// ObjectLoadedMsg is emitted when a single object has been fetched.
type ObjectLoadedMsg struct {
	Object *storage.KnowledgeObject
}

// JobsUpdatedMsg is emitted on each job-poll tick.
type JobsUpdatedMsg struct {
	Jobs []*storage.Job
}

// CaptureSubmittedMsg is emitted after the capture modal submits content.
type CaptureSubmittedMsg struct {
	JobID string
}

// ComposeResultMsg is emitted after a composition completes.
type ComposeResultMsg struct {
	Markdown string
}

// ErrorMsg wraps any error that should surface in the TUI status bar.
type ErrorMsg struct {
	Err error
}

// --- Theme ---

// Theme holds all lipgloss styles used by the TUI.
type Theme struct {
	Focused   lipgloss.Style
	Blurred   lipgloss.Style
	Selected  lipgloss.Style
	StatusBar lipgloss.Style
	Tag       lipgloss.Style
	Title     lipgloss.Style
	Muted     lipgloss.Style
	Error     lipgloss.Style
	Success   lipgloss.Style
	Running   lipgloss.Style
	Accent    lipgloss.Style
}

// DefaultTheme returns the default TUI color theme.
func DefaultTheme() Theme {
	colorPrimary := lipgloss.Color("#5C5CFF")
	colorForeground := lipgloss.Color("#FFFFFF")
	colorBorder := lipgloss.Color("238")
	colorMuted := lipgloss.Color("8")
	colorSuccess := lipgloss.Color("2")
	colorError := lipgloss.Color("1")
	colorWarning := lipgloss.Color("3")
	colorAccent := lipgloss.Color("86")
	colorSelected := lipgloss.Color("205")

	return Theme{
		Focused: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorPrimary),
		Blurred: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder),
		Selected: lipgloss.NewStyle().
			Foreground(colorSelected).
			Bold(true),
		StatusBar: lipgloss.NewStyle().
			Background(colorBorder).
			Foreground(colorForeground).
			Padding(0, 1),
		Tag: lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true),
		Title: lipgloss.NewStyle().
			Foreground(colorForeground).
			Background(colorPrimary).
			Bold(true).
			Padding(0, 1),
		Muted:   lipgloss.NewStyle().Foreground(colorMuted),
		Error:   lipgloss.NewStyle().Foreground(colorError).Bold(true),
		Success: lipgloss.NewStyle().Foreground(colorSuccess).Bold(true),
		Running: lipgloss.NewStyle().Foreground(colorWarning).Bold(true),
		Accent:  lipgloss.NewStyle().Foreground(colorAccent).Bold(true),
	}
}

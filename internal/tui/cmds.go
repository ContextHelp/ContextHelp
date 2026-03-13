package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

const (
	searchLimit     = 50
	jobPollInterval = 2 * time.Second
)

// SearchCmd returns a tea.Cmd that performs a full-text search.
// The caller is responsible for debouncing repeated calls (250ms recommended).
func SearchCmd(adapter ServiceAdapter, query string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		results, err := adapter.Search(ctx, query, searchLimit)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		return SearchResultsMsg{Results: results}
	}
}

// LoadObjectCmd returns a tea.Cmd that fetches a single KnowledgeObject by ID.
func LoadObjectCmd(adapter ServiceAdapter, id string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		obj, err := adapter.GetObject(ctx, id)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		return ObjectLoadedMsg{Object: obj}
	}
}

// PollJobsCmd returns a tea.Cmd that fetches the current job list.
// The root model must re-issue PollJobsCmd after each JobsUpdatedMsg to keep
// the ticker self-sustaining (Bubble Tea's tick pattern).
func PollJobsCmd(adapter ServiceAdapter) tea.Cmd {
	return tea.Tick(jobPollInterval, func(_ time.Time) tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		jobs, _, err := adapter.ListJobs(ctx, storage.JobFilter{Limit: 100})
		if err != nil {
			return ErrorMsg{Err: err}
		}
		return JobsUpdatedMsg{Jobs: jobs}
	})
}

package tui

import "github.com/ideacrafterslabs/ctxt/internal/tui/types"

// Re-export message types from tui/types for backward compatibility.

// SearchResultsMsg is emitted when a search completes.
type SearchResultsMsg = types.SearchResultsMsg

// ObjectLoadedMsg is emitted when a single object has been fetched.
type ObjectLoadedMsg = types.ObjectLoadedMsg

// JobsUpdatedMsg is emitted on each job-poll tick.
type JobsUpdatedMsg = types.JobsUpdatedMsg

// CaptureSubmittedMsg is emitted after the capture modal submits content.
type CaptureSubmittedMsg = types.CaptureSubmittedMsg

// ComposeResultMsg is emitted after a composition completes.
type ComposeResultMsg = types.ComposeResultMsg

// ErrorMsg wraps any error that should surface in the TUI status bar.
type ErrorMsg = types.ErrorMsg

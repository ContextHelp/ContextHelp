package ingest

import (
	"context"
	"time"
)

// Object is the adapter-output contract. Adapters emit a slice of
// these; the runner handles dedup, storage, and indexing.
type Object struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Content  string         `json:"content"`
	Tags     []string       `json:"tags,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Adapter is the contract external ingestion adapters implement.
// Fetch returns objects from the source. The runner calls Fetch
// once per poll cycle (or once if --watch is not set).
type Adapter interface {
	// Name returns the adapter identifier (e.g. "cardamum").
	Name() string
	// Fetch retrieves objects from the source. Implementations
	// should respect ctx cancellation for long-running sources.
	Fetch(ctx context.Context) ([]Object, error)
}

// Result summarises a single ingestion run.
type Result struct {
	Source   string    `json:"source"`
	Total   int       `json:"total"`
	Created int       `json:"created"`
	Skipped int       `json:"skipped"`
	Errors  int       `json:"errors"`
	Elapsed time.Duration `json:"elapsed"`
}

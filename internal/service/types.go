package service

import (
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/citation"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	"hop.top/uri"
)

// AnalyzeRequest represents a request to analyze content.
type AnalyzeRequest struct {
	Content     string `json:"content"`
	Type        string `json:"type"`
	Pipeline    string `json:"pipeline,omitempty"`
	Source      string `json:"source,omitempty"`
	KnownHash   string `json:"known_hash,omitempty"`   // pre-computed content hash; skips re-hashing
	SourceTitle string `json:"source_title,omitempty"` // human-readable title of source (e.g. page title)
	AuthState   string `json:"auth_state,omitempty"`   // opaque auth state token (browser extension)
	Raw         bool   `json:"raw,omitempty"`          // skip AI enrichment; store object immediately in raw state
	NoFanout    bool   `json:"no_fanout,omitempty"`    // skip post-ingest fan-out enrichment
	SourceKey   string `json:"source_key,omitempty"`   // external dedup key (Slack ts, tweet ID, etc.)
	Force       bool   `json:"force,omitempty"`        // bypass duplicate detection
	// Mentions are caller-asserted @namespace.slug strings (T-0190). They
	// are merged with auto-extracted mentions during pipeline execution and
	// become real mention edges + thin entity rows. Caller-supplied wins on
	// conflict — the merge step dedupes by slug.
	Mentions []string `json:"mentions,omitempty"`
}

// CreatePipelineRequest represents a request to create a custom pipeline.
type CreatePipelineRequest struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Steps       string                 `json:"steps"`
	Sandbox     *storage.SandboxConfig `json:"sandbox,omitempty"`
}

// CompositionResult is the structured output of ComposeWithCitations.
type CompositionResult struct {
	// Type is the requested composition type (brief, plan, summary, draft).
	Type string `json:"type"`
	// Content is the full markdown body including inline [ref:ID] markers
	// and an appended reference table.
	Content string `json:"content"`
	// Citations is the parsed list of inline citations found in Content.
	Citations []citation.Citation `json:"citations"`
	// SourceIDs are the IDs of every object used as input.
	SourceIDs []string `json:"source_ids"`
	// GeneratedAt records when the composition was produced.
	GeneratedAt time.Time `json:"generated_at"`
}

// InboxCaptureRequest carries data for a fast-path inbox capture.
type InboxCaptureRequest struct {
	Content     string
	Type        string
	Source      string
	InboxNote   string
	Hints       string
	Mentions []uri.URI
}

// InboxFilter specifies criteria for listing inbox items.
type InboxFilter struct {
	Limit  int
	Offset int
	Before time.Time
	After  time.Time
}

// TriageRequest carries parameters to promote an inbox object into the pipeline.
type TriageRequest struct {
	Pipeline string
	Hints    string
	Mentions []string
}

// InboxQueueFilter specifies criteria for the combined inbox queue view.
type InboxQueueFilter struct {
	Pending bool
	Failed  bool
	Raw     bool
	Limit   int
	Offset  int
}

// ScoreBreakdown holds per-signal scores explaining why a result ranked where it did.
type ScoreBreakdown struct {
	// FTS is the Reciprocal Rank Fusion contribution from the full-text search leg.
	FTS float64 `json:"fts"`
	// Vector is the RRF contribution from the vector similarity leg.
	Vector float64 `json:"vector"`
	// MentionBoost is the additive bonus from outbound mention count.
	MentionBoost float64 `json:"mention_boost"`
	// GraphRelevance is the additive bonus from inbound backlinks (direct + 2-hop).
	GraphRelevance float64 `json:"graph_relevance"`
	// WordOverlap is the additive bonus from projection-aware word overlap scoring.
	WordOverlap float64 `json:"word_overlap"`
	// Total is the sum of all signal contributions.
	Total float64 `json:"total"`
}

// HybridResult pairs a KnowledgeObject with its score breakdown.
// DocumentView is populated for all results; use it for display surfaces.
type HybridResult struct {
	Object       *storage.KnowledgeObject    `json:"object"`
	Breakdown    ScoreBreakdown              `json:"score_breakdown"`
	DocumentView pluginapi.DocumentProjection `json:"document_view"`
}

// InboxQueueItem is a row in the combined inbox queue view.
// Represents either a job (pending/failed) or a raw object.
type InboxQueueItem struct {
	// ID is the job ID or object ID depending on Kind.
	ID string `json:"id"`
	// Kind is "job" or "object".
	Kind string `json:"kind"`
	// Status is job status ("pending", "running", "failed") or object status ("raw").
	Status string `json:"status"`
	// Type is the content type (e.g., "text", "url", "image").
	Type string `json:"type"`
	// Source is the content origin (URL, filename, etc).
	Source string `json:"source"`
	// Pipeline is the pipeline name if set.
	Pipeline string `json:"pipeline"`
	// CreatedAt is when the item was created.
	CreatedAt time.Time `json:"created_at"`
	// Error holds the failure message for failed jobs.
	Error string `json:"error,omitempty"`
}

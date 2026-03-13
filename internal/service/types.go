package service

import (
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/citation"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// AnalyzeRequest represents a request to analyze content.
type AnalyzeRequest struct {
	Content   string `json:"content"`
	Type      string `json:"type"`
	Pipeline  string `json:"pipeline,omitempty"`
	Source    string `json:"source,omitempty"`
	KnownHash string `json:"known_hash,omitempty"` // pre-computed content hash; skips re-hashing
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

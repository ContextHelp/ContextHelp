package service

import "github.com/ideacrafterslabs/ctxt/internal/storage"

// AnalyzeRequest represents a request to analyze content.
type AnalyzeRequest struct {
	Content  string `json:"content"`
	Type     string `json:"type"`
	Pipeline string `json:"pipeline,omitempty"`
	Source   string `json:"source,omitempty"`
}

// CreatePipelineRequest represents a request to create a custom pipeline.
type CreatePipelineRequest struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Steps       string                 `json:"steps"`
	Sandbox     *storage.SandboxConfig `json:"sandbox,omitempty"`
}

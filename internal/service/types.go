package service

// AnalyzeRequest represents a request to analyze content.
type AnalyzeRequest struct {
	Content  string `json:"content"`
	Type     string `json:"type"`
	Pipeline string `json:"pipeline,omitempty"`
	Source   string `json:"source,omitempty"`
}

package retrieval

import (
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Method determines how retrieval is performed.
type Method string

const (
	MethodRAG Method = "rag" // Vector similarity search
	MethodLLM Method = "llm" // LLM-based ranking
)

// Config configures progressive retrieval behaviour.
type Config struct {
	Method                 Method
	EnableSufficiencyCheck bool
	LLMProfile             string // Which LLM profile to use for checking

	Categories TierConfig
	Items      TierConfig
	Resources  TierConfig
}

// TierConfig configures a single retrieval tier.
type TierConfig struct {
	Enabled bool
	TopK    int
}

// State tracks in-flight retrieval progress.
type State struct {
	OriginalQuery      string
	RewrittenQuery     string
	ActiveQuery        string
	NeedsRetrieval     bool
	ProceedToItems     bool
	ProceedToResources bool

	CategoryHits []Hit
	ItemHits     []Hit
	ResourceHits []Hit

	QueryVector   []float32
	NextStepQuery string
}

// Hit is a retrieved object with its relevance score.
type Hit struct {
	ID    string
	Score float64
	Data  *storage.KnowledgeObject
}

// Result is the final output of a retrieval workflow run.
type Result struct {
	NeedsRetrieval bool
	OriginalQuery  string
	RewrittenQuery string
	NextStepQuery  string
	Categories     []*storage.KnowledgeObject
	Items          []*storage.KnowledgeObject
	Resources      []*storage.KnowledgeObject
}

// DefaultConfig returns a sensible default retrieval configuration.
func DefaultConfig() Config {
	return Config{
		Method:                 MethodRAG,
		EnableSufficiencyCheck: true,
		LLMProfile:             "default",
		Categories:             TierConfig{Enabled: true, TopK: 10},
		Items:                  TierConfig{Enabled: true, TopK: 20},
		Resources:              TierConfig{Enabled: true, TopK: 5},
	}
}

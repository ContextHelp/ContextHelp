package steps

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"hop.top/c12n"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// C12nClassifier runs hop.top/c12n content classification and stores
// results under Metadata["enrichment.c12n_signals"].
type C12nClassifier struct {
	pipeline.BaseContract

	once    sync.Once
	pipe    *c12n.Pipeline
	initErr error
	cfg     c12n.PipelineConfig
}

// NewC12nClassifier creates a classifier with default config.
func NewC12nClassifier() *C12nClassifier {
	return &C12nClassifier{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"Metadata"},
		}),
		cfg: c12n.PipelineConfig{
			MaxConcurrency: 4,
			Timeout:        30 * time.Second,
		},
	}
}

func (s *C12nClassifier) Name() string { return "c12n_classify" }

func (s *C12nClassifier) initPipeline() {
	s.pipe, s.initErr = c12n.NewPipeline(s.cfg)
}

func (s *C12nClassifier) Run(
	ctx context.Context,
	draft *storage.KnowledgeObject,
) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	s.once.Do(s.initPipeline)
	if s.initErr != nil {
		// Classification is optional: when the c12n pipeline cannot be
		// built the object is passed through marked unavailable, the miss
		// is logged, and ingestion proceeds unclassified.
		log.Printf("c12n_classify: pipeline unavailable, skipping: %v", s.initErr)
		draft.Metadata["c12n_status"] = "unavailable"
		return draft, nil //nolint:nilerr // optional step; logged and marked c12n_status=unavailable
	}

	content := strings.TrimSpace(draft.RawContent)
	if content == "" {
		draft.Metadata["c12n_status"] = "skipped"
		return draft, nil
	}

	// Truncate to keep classification within reasonable bounds.
	if len(content) > 8000 {
		content = content[:8000]
	}

	cctx := c12n.ClassificationContext{Text: content}
	raw, err := s.pipe.Evaluate(cctx)
	if err != nil {
		log.Printf("c12n_classify: evaluate failed, skipping: %v", err)
		draft.Metadata["c12n_status"] = "error"
		return draft, nil
	}

	result, err := c12n.ParseResult(raw)
	if err != nil {
		log.Printf("c12n_classify: parse failed, skipping: %v", err)
		draft.Metadata["c12n_status"] = "error"
		return draft, nil
	}

	signals := make([]map[string]any, 0, len(result.Results))
	for _, sig := range result.Results {
		signals = append(signals, map[string]any{
			"name":        sig.Name,
			"signal_type": string(sig.Type),
			"confidence":  sig.Confidence,
			"labels":      sig.Labels,
			"metadata":    sig.Metadata,
		})
	}

	draft.Metadata["enrichment.c12n_signals"] = map[string]any{
		"signals":     signals,
		"duration_ns": result.DurationNs,
		"has_errors":  result.HasErrors(),
	}
	draft.Metadata["c12n_status"] = "complete"
	return draft, nil
}

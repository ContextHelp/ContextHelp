package steps

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type TimestampAligner struct {
	pipeline.BaseContract
}

func NewTimestampAligner() *TimestampAligner {
	return &TimestampAligner{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"Metadata"},
			Produces: []string{"Sections"},
		}),
	}
}

func (s *TimestampAligner) Name() string { return "timestamp_aligner" }

func (s *TimestampAligner) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		return draft, nil
	}

	segments, _ := draft.Metadata["_raw_segments"].([]providers.TranscriptSegment)
	if len(segments) == 0 {
		return draft, nil
	}

	for i, seg := range segments {
		section := storage.Section{
			Title:   fmt.Sprintf("Segment %d", i+1),
			Content: seg.Text,
			Order:   i,
			Metadata: map[string]any{
				"start_time": seg.StartTime.String(),
				"end_time":   seg.EndTime.String(),
			},
		}
		if seg.Speaker != "" {
			section.Metadata["speaker"] = seg.Speaker
		}
		draft.Sections = append(draft.Sections, section)
	}

	// Clean up internal metadata.
	delete(draft.Metadata, "_raw_segments")

	return draft, nil
}

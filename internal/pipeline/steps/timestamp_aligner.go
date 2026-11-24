package steps

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/representation"
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
		startMS, endMS := clampSpan(seg.StartTime.Milliseconds(), seg.EndTime.Milliseconds())
		section := storage.Section{
			Title:   fmt.Sprintf("Segment %d", i+1),
			Content: seg.Text,
			Order:   i,
			Metadata: map[string]any{
				"start_time":                     seg.StartTime.String(),
				"end_time":                       seg.EndTime.String(),
				representation.KeyStartMS:        startMS,
				representation.KeyEndMS:          endMS,
				representation.KeyRepresentation: string(representation.Transcript),
			},
		}
		if seg.Speaker != "" {
			section.Metadata["speaker"] = seg.Speaker
		}
		draft.Sections = append(draft.Sections, section)
	}

	// The transcriber normally emitted the transcript nodes already; a
	// diarizer that ran since then only changed the segments' speakers, so
	// copy those onto the nodes. When no node exists, emit them here: the
	// graph-first projection ignores flat sections, so segments only in
	// Sections would never be indexed.
	if draft.ID != "" {
		if representation.Has(draft.Graph, representation.Transcript) {
			applySpeakers(draft.Graph, segments)
		} else {
			provider, _ := draft.Metadata["transcription_provider"].(string)
			model, _ := draft.Metadata["transcription_model"].(string)
			conf, _ := draft.Metadata["transcription_confidence"].(float64)
			emitTranscript(draft, segments, provider, model, representation.ConfidenceOf(conf))
		}
	}

	// Clean up internal metadata.
	delete(draft.Metadata, "_raw_segments")

	return draft, nil
}

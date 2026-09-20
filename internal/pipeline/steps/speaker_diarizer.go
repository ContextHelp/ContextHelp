package steps

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type SpeakerDiarizer struct {
	pipeline.BaseContract
	provider providers.DiarizationProvider
	enabled  bool
}

func NewSpeakerDiarizer(enabled bool, opts ...func(*SpeakerDiarizer)) *SpeakerDiarizer {
	s := &SpeakerDiarizer{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires:     []string{"RawContent", "Metadata"},
			Produces:     []string{"Metadata", "Sections"},
			Capabilities: []string{"diarization"},
		}),
		provider: providers.NewStubDiarizationProvider(),
		enabled:  enabled,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func WithDiarizationProvider(p providers.DiarizationProvider) func(*SpeakerDiarizer) {
	return func(s *SpeakerDiarizer) { s.provider = p }
}

func (s *SpeakerDiarizer) Name() string { return "speaker_diarizer" }

func (s *SpeakerDiarizer) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if !s.enabled {
		return draft, nil
	}

	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	segments, _ := draft.Metadata["_raw_segments"].([]providers.TranscriptSegment)

	result, err := s.provider.Diarize(ctx, draft.Source, segments)
	if err != nil {
		// Optional enrichment: the transcript stays usable without speaker
		// labels. The error text is kept on the object so the failure stays
		// visible downstream.
		draft.Metadata["diarization_error"] = err.Error()
		return draft, nil //nolint:nilerr // optional provider; error preserved in diarization_error
	}

	draft.Metadata["speaker_count"] = result.SpeakerCount
	draft.Metadata["speakers"] = result.SpeakerLabels
	draft.Metadata["_raw_segments"] = result.Segments

	return draft, nil
}

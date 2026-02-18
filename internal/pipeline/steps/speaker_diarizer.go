package steps

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type SpeakerDiarizer struct {
	provider providers.DiarizationProvider
	enabled  bool
}

func NewSpeakerDiarizer(enabled bool, opts ...func(*SpeakerDiarizer)) *SpeakerDiarizer {
	s := &SpeakerDiarizer{
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
		// Diarization failure is non-fatal.
		draft.Metadata["diarization_error"] = err.Error()
		return draft, nil
	}

	draft.Metadata["speaker_count"] = result.SpeakerCount
	draft.Metadata["speakers"] = result.SpeakerLabels
	draft.Metadata["_raw_segments"] = result.Segments

	return draft, nil
}

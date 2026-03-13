package steps

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// AudioExtractor extracts audio from a video file using a VideoProvider.
type AudioExtractor struct {
	pipeline.BaseContract
	provider providers.VideoProvider
}

// AudioExtractorOption configures an AudioExtractor.
type AudioExtractorOption func(*AudioExtractor)

// WithVideoProvider sets the VideoProvider used for audio extraction.
func WithVideoProvider(p providers.VideoProvider) AudioExtractorOption {
	return func(a *AudioExtractor) { a.provider = p }
}

// NewAudioExtractor creates an AudioExtractor with optional configuration.
func NewAudioExtractor(opts ...AudioExtractorOption) *AudioExtractor {
	a := &AudioExtractor{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{},
			Produces: []string{},
		}),
		provider: providers.NewStubVideoProvider(),
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

func (s *AudioExtractor) Name() string { return "audio_extractor" }

func (s *AudioExtractor) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	result, err := s.provider.ExtractAudio(ctx, draft.Source)
	if err != nil {
		return nil, fmt.Errorf("audio_extractor: %w", err)
	}

	draft.Metadata["audio_path"] = result.Path
	draft.Metadata["audio_format"] = result.Format
	draft.Metadata["audio_duration"] = result.Duration.String()
	draft.Metadata["audio_sample_rate"] = result.SampleRate

	return draft, nil
}

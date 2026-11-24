package steps

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

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
			Binary:       true,
			Requires:     []string{},
			Produces:     []string{},
			Capabilities: []string{"video"},
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

	src, err := filepath.Abs(draft.Source)
	if err != nil {
		return nil, fmt.Errorf("audio_extractor: %w", err)
	}
	// The extracted audio goes to the run's scratch dir, never beside
	// the source; the run removes it, and audio_path with it.
	dir, err := pipeline.ScratchDir(ctx, "ctxt-audio-*", "audio_path")
	if err != nil {
		return nil, fmt.Errorf("audio_extractor: %w", err)
	}
	result, err := s.provider.ExtractAudio(ctx, src, dir)
	if err != nil {
		return nil, fmt.Errorf("audio_extractor: %w", permanentIfUnusable(err))
	}

	draft.Metadata["audio_path"] = result.Path
	draft.Metadata["audio_format"] = result.Format
	draft.Metadata["audio_duration"] = result.Duration.String()
	draft.Metadata["audio_sample_rate"] = result.SampleRate

	return draft, nil
}

// permanentIfUnusable marks errors a retry cannot fix.
func permanentIfUnusable(err error) error {
	if errors.Is(err, providers.ErrUnusableMedia) || errors.Is(err, providers.ErrInvalidFrameOptions) {
		return pipeline.Permanent(err)
	}
	return err
}

package steps

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type AudioTranscriber struct {
	pipeline.BaseContract
	provider providers.TranscriptionProvider
}

func NewAudioTranscriber(opts ...func(*AudioTranscriber)) *AudioTranscriber {
	a := &AudioTranscriber{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires:     []string{"Source", "ContentType"},
			Produces:     []string{"RawContent", "Metadata"},
			Capabilities: []string{"transcription"},
		}),
		provider: providers.NewStubTranscriptionProvider(),
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

func WithTranscriptionProvider(p providers.TranscriptionProvider) func(*AudioTranscriber) {
	return func(a *AudioTranscriber) { a.provider = p }
}

func (s *AudioTranscriber) Name() string { return "audio_transcriber" }

func (s *AudioTranscriber) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	lang, _ := draft.Metadata["language_hint"].(string)

	result, err := s.provider.Transcribe(ctx, draft.Source, providers.TranscribeOptions{
		Language: lang,
		Format:   draft.ContentType,
	})
	if err != nil {
		return nil, fmt.Errorf("audio_transcriber: %w", err)
	}

	draft.RawContent = result.FullText
	draft.Metadata["transcription_provider"] = s.provider.Name()
	draft.Metadata["transcription_language"] = result.DetectedLanguage
	draft.Metadata["transcription_confidence"] = result.Confidence
	draft.Metadata["_raw_segments"] = result.Segments

	return draft, nil
}

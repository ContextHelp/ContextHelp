package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/representation"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type AudioTranscriber struct {
	pipeline.BaseContract
	provider providers.TranscriptionProvider
}

func NewAudioTranscriber(opts ...func(*AudioTranscriber)) *AudioTranscriber {
	a := &AudioTranscriber{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Binary:       true,
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

	if m := representation.ModelOf(s.provider); m != "" {
		draft.Metadata["transcription_model"] = m
	}

	// Emit one transcript representation node per segment when ID is set.
	// There is no root summary holding the full text: the segments are the
	// text, and a copy of it would be indexed twice.
	if draft.ID == "" {
		return draft, nil
	}
	emitTranscript(draft, result.Segments, s.provider.Name(),
		representation.ModelOf(s.provider), representation.ConfidenceOf(result.Confidence))
	return draft, nil
}

// emitTranscript writes one transcript node per non-empty segment. A
// segment whose anchors are invalid is clamped; one Emit still refuses is
// skipped and recorded as transcript_error.
func emitTranscript(draft *storage.KnowledgeObject, segs []providers.TranscriptSegment, provider, model string, conf *float64) {
	for i, seg := range segs {
		if strings.TrimSpace(seg.Text) == "" {
			continue
		}
		start, end := clampSpan(seg.StartTime.Milliseconds(), seg.EndTime.Milliseconds())
		emitLenient(draft, representation.Spec{
			Kind:       representation.Transcript,
			Label:      fmt.Sprintf("Segment %d", i+1),
			Content:    seg.Text,
			Anchor:     representation.Anchor{}.WithSpan(start, end).WithSpeaker(seg.Speaker),
			Provider:   provider,
			Model:      model,
			Confidence: conf,
		})
	}
}

package steps

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
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

	// Emit canonical graph nodes when ID is set.
	if draft.ID == "" {
		return draft, nil
	}
	if draft.Graph == nil {
		draft.Graph = &pluginapi.ObjectGraph{}
	}
	rootID := pluginapi.NewNodeID(draft.ID, pluginapi.NodeTypeSummary, 0)
	if draft.Graph.FindNode(rootID) == nil {
		draft.Graph.Nodes = append(draft.Graph.Nodes, pluginapi.GraphNode{
			ID:       rootID,
			NodeType: pluginapi.NodeTypeSummary,
			Label:    "root",
			Content:  result.FullText,
			Order:    0,
		})
	}
	for i, seg := range result.Segments {
		secID := pluginapi.NewNodeID(draft.ID, pluginapi.NodeTypeSection, i)
		draft.Graph.Nodes = append(draft.Graph.Nodes, pluginapi.GraphNode{
			ID:       secID,
			NodeType: pluginapi.NodeTypeSection,
			Label:    fmt.Sprintf("Segment %d", i+1),
			Content:  seg.Text,
			Order:    i,
			Metadata: map[string]any{
				"start_ms": seg.StartTime.Milliseconds(),
				"end_ms":   seg.EndTime.Milliseconds(),
				"speaker":  seg.Speaker,
			},
		})
		draft.Graph.Edges = append(draft.Graph.Edges, pluginapi.GraphEdge{
			ID:       fmt.Sprintf("%s->%s", rootID, secID),
			FromID:   rootID,
			ToID:     secID,
			EdgeType: pluginapi.EdgeTypeContains,
		})
	}

	return draft, nil
}

package steps

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// FrameSampler samples keyframes from a video file using a VideoProvider.
type FrameSampler struct {
	pipeline.BaseContract
	provider   providers.VideoProvider
	frameCount int
}

// FrameSamplerOption configures a FrameSampler.
type FrameSamplerOption func(*FrameSampler)

// WithFrameSamplerVideoProvider sets the VideoProvider used for frame sampling.
func WithFrameSamplerVideoProvider(p providers.VideoProvider) FrameSamplerOption {
	return func(s *FrameSampler) { s.provider = p }
}

// WithFrameCount sets the number of frames to sample.
func WithFrameCount(n int) FrameSamplerOption {
	return func(s *FrameSampler) { s.frameCount = n }
}

// NewFrameSampler creates a FrameSampler with optional configuration.
func NewFrameSampler(opts ...FrameSamplerOption) *FrameSampler {
	s := &FrameSampler{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{},
			Produces: []string{},
		}),
		provider:   providers.NewStubVideoProvider(),
		frameCount: 10,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *FrameSampler) Name() string { return "frame_sampler" }

func (s *FrameSampler) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	frames, err := s.provider.SampleFrames(ctx, draft.Source, s.frameCount)
	if err != nil {
		return nil, fmt.Errorf("frame_sampler: %w", err)
	}

	paths := make([]string, len(frames))
	for i, f := range frames {
		paths[i] = f.Path
	}

	draft.Metadata["frame_count"] = len(frames)
	draft.Metadata["frame_paths"] = paths

	return draft, nil
}

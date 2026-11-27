package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// FrameSampler selects keyframes from a video file using a FrameProvider.
type FrameSampler struct {
	pipeline.BaseContract
	provider providers.FrameProvider
	opts     providers.FrameOptions
}

// FrameSamplerOption configures a FrameSampler.
type FrameSamplerOption func(*FrameSampler)

// WithFrameProvider sets the FrameProvider used for frame selection.
func WithFrameProvider(p providers.FrameProvider) FrameSamplerOption {
	return func(s *FrameSampler) { s.provider = p }
}

// WithFrameOptions sets the frame selection options.
func WithFrameOptions(o providers.FrameOptions) FrameSamplerOption {
	return func(s *FrameSampler) { s.opts = o }
}

// NewFrameSampler creates a FrameSampler with optional configuration.
func NewFrameSampler(opts ...FrameSamplerOption) *FrameSampler {
	s := &FrameSampler{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Binary:       true,
			Requires:     []string{},
			Produces:     []string{"Metadata"},
			Capabilities: []string{"frames"},
		}),
		provider: providers.NewStubFrameProvider(),
		opts:     providers.DefaultFrameOptions(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *FrameSampler) Name() string { return "frame_sampler" }

// Run selects frames from draft.Source and writes each, as it is
// produced, into a scratch directory under the user's cache directory,
// never beside the source. The pipeline run removes the directory, and frame_paths with it,
// when it ends. frame_ms and frame_count land in the draft's metadata.
func (s *FrameSampler) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	src, err := filepath.Abs(draft.Source)
	if err != nil {
		return nil, fmt.Errorf("frame_sampler: %w", err)
	}

	dir, err := pipeline.ScratchDir(ctx, "ctxt-frames-*", "frame_paths")
	if err != nil {
		return nil, fmt.Errorf("frame_sampler: %w", err)
	}

	var paths []string
	var ms []int64
	err = providers.StreamFrames(ctx, s.provider, providers.FileSource(src), s.opts, func(f providers.Frame) error {
		p := filepath.Join(dir, fmt.Sprintf("frame.%d.png", f.Index))
		if err := os.WriteFile(p, f.Data, 0o600); err != nil {
			return fmt.Errorf("write frame %d: %w", f.Index, err)
		}
		paths = append(paths, p)
		ms = append(ms, f.TimestampMS)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("frame_sampler: %w", permanentIfUnusable(err))
	}

	draft.Metadata["frame_count"] = len(paths)
	draft.Metadata["frame_paths"] = paths
	draft.Metadata["frame_ms"] = ms
	return draft, nil
}

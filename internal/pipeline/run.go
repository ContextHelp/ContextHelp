package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// RunSteps runs pipe's steps on draft in order. A step that reads text
// skips binary content, a step returning ErrDelegate is skipped, and any
// other error fails the run. Scratch directories steps create through
// ScratchDir are removed when RunSteps returns, on success and failure:
// cleanup is per run, so a hop to another pipeline cleans its own.
func RunSteps(ctx context.Context, pipe *Pipeline, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	ctx, scratch := WithScratch(ctx)
	cur := draft
	defer func() { scratch.Cleanup(cur) }()
	for _, step := range pipe.Steps {
		// A step that reads content as text never sees binary: it would
		// copy bytes into sections, nodes or metadata.
		if SkipsBinary(step, draft) {
			slog.Debug("pipeline: binary content, step skipped", "step", step.Name())
			continue
		}
		out, err := step.Run(ctx, draft)
		if err != nil {
			if errors.Is(err, ErrDelegate) {
				slog.Debug("pipeline: step delegated, skipping", "step", step.Name())
				continue
			}
			return nil, fmt.Errorf("step %s: %w", step.Name(), err)
		}
		draft, cur = out, out
	}
	return draft, nil
}

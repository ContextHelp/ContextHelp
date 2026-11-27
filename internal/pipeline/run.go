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
	// The graph is marked as built under stamping whether or not a step adds
	// to it: reanalysis reads an unstamped node of a marked graph as not
	// added by a step.
	draft.Graph = settleGraph(draft.Graph, markGraph(draft.Graph), "")
	defer func() { scratch.Cleanup(cur) }()
	for _, step := range pipe.Steps {
		// A step that reads content as text never sees binary: it would
		// copy bytes into sections, nodes or metadata.
		if SkipsBinary(step, draft) {
			slog.Debug("pipeline: binary content, step skipped", "step", step.Name())
			continue
		}
		before := markGraph(draft.Graph)
		out, err := step.Run(ctx, draft)
		if err == nil && out != nil {
			out.Graph = settleGraph(out.Graph, before, step.Name())
		}
		if err != nil {
			if errors.Is(err, ErrDelegate) {
				// The step may have changed the graph in place before it delegated.
				draft.Graph = settleGraph(draft.Graph, before, step.Name())
				slog.Debug("pipeline: step delegated, skipping", "step", step.Name())
				continue
			}
			return nil, fmt.Errorf("step %s: %w", step.Name(), err)
		}
		draft, cur = out, out
	}
	return draft, nil
}

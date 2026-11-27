package service

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// ReanalyzeOptions are the choices of one reanalysis. The zero value
// re-derives from the stored content only.
type ReanalyzeOptions struct {
	// AllowNetwork lets the steps that fetch from the network run, so a
	// reanalysis can refresh content from its source. Off by default: a
	// reanalysis must not replace a stored snapshot with whatever the
	// source serves now.
	AllowNetwork bool
}

// networkSteps are the steps that fetch their content from outside the
// store. Everything that names a step by role lives in this file.
var networkSteps = map[string]bool{
	"url_fetcher":         true,
	"ibr_fetcher":         true,
	"feed_fetcher":        true,
	"imap_fetcher":        true,
	"dropbox_fetcher":     true,
	"github_repo_fetcher": true,
}

// freshSourceSteps only make sense on content just acquired from its
// source (markup still to be cleaned, a response still to be routed by its
// type). They are skipped with the fetch they follow: run on the stored,
// already cleaned text they would strip or decode it a second time.
var freshSourceSteps = map[string]bool{
	"html_cleaner":        true,
	"content_type_router": true,
}

// containerSteps stage or enqueue the items of a container (a feed, a
// mailbox, an export, a folder). A pipeline holding one produces no
// content of its own: it cannot be re-derived from stored content, and
// running it again would enqueue the items again.
var containerSteps = map[string]bool{
	"item_enqueuer":       true,
	"batch_enqueuer":      true,
	"email_enqueuer":      true,
	"dropbox_enqueuer":    true,
	"item_deduplicator":   true,
	"dependency_enricher": true,
}

// sourceReadingSteps read the file at the object's source path (or the
// media there), not the stored bytes, so a reanalysis would read whatever
// is at the path now, or fail when it is gone, whether or not the object
// kept an original. A pipeline in which one would run is refused.
var sourceReadingSteps = map[string]bool{
	"pdf_extractor":     true,
	"office_extractor":  true,
	"audio_extractor":   true,
	"audio_transcriber": true,
	"frame_sampler":     true,
	"speaker_diarizer":  true,
}

// ingestDecisionSteps decide, once and from the state of the store at
// ingest, what to do with a new object (is it a near-duplicate, is it
// suppressed). They are never run in a reanalysis: dedup would mark the
// object a duplicate of another, and under the drop policy flag it
// suppressed, from a store that has moved on since ingest.
var ingestDecisionSteps = map[string]bool{
	"dedup": true,
}

// refuseSourceReading refuses obj when a step of pipe that reads the
// source would run on it. The two document extractors act only on their
// own subtype, which a format detector in the pipeline sets again from the
// source's extension.
func refuseSourceReading(pipe *pipeline.Pipeline, obj *storage.KnowledgeObject) error {
	hasDetector := false
	for _, st := range pipe.Steps {
		hasDetector = hasDetector || st.Name() == "format_detector"
	}
	for _, st := range pipe.Steps {
		if !sourceReadingSteps[st.Name()] {
			continue
		}
		switch st.Name() {
		case "pdf_extractor":
			if obj.Subtype != "pdf" && !hasDetector {
				continue
			}
		case "office_extractor":
			if obj.Subtype != "office" && !hasDetector {
				continue
			}
		}
		return fmt.Errorf("reanalyze: %w: object %q: step %s reads the source file, not the stored content",
			upgrade.ErrCannotReanalyze, obj.ID, st.Name())
	}
	return nil
}

// fetchDependentSteps need what only a fetch step provides (not the
// stored text), so they cannot run when the fetch is skipped.
var fetchDependentSteps = map[string]bool{
	"github_repo_mapper": true,
}

// fetchesFromNetwork reports whether step reads from the network.
func fetchesFromNetwork(step pipeline.PipelineStep) bool { return networkSteps[step.Name()] }

// storedContentPipeline returns pipe without its network and fresh-source
// steps, or
// ErrCannotReanalyze when a step left in needs what a skipped fetch
// provides. With allow the pipeline is returned as it is.
func storedContentPipeline(pipe *pipeline.Pipeline, subject string, allow bool) (*pipeline.Pipeline, error) {
	for _, st := range pipe.Steps {
		if containerSteps[st.Name()] {
			return nil, fmt.Errorf("reanalyze: %w: %s: step %s stages items, not content",
				upgrade.ErrCannotReanalyze, subject, st.Name())
		}
	}
	out := *pipe
	out.Steps = nil
	skipped := ""
	for _, st := range pipe.Steps {
		if ingestDecisionSteps[st.Name()] {
			continue
		}
		if allow {
			out.Steps = append(out.Steps, st)
			continue
		}
		if fetchesFromNetwork(st) {
			skipped = st.Name()
			continue
		}
		if freshSourceSteps[st.Name()] {
			continue
		}
		if skipped != "" && fetchDependentSteps[st.Name()] {
			return nil, fmt.Errorf("reanalyze: %w: %s: step %s needs the output of %s, which is not run on stored content",
				upgrade.ErrCannotReanalyze, subject, st.Name(), skipped)
		}
		out.Steps = append(out.Steps, st)
	}
	return &out, nil
}

// producedFields is the set of object fields the steps of pipe declare
// they write: a stored field outside it is left as stored.
func producedFields(pipe *pipeline.Pipeline) map[string]bool {
	out := map[string]bool{}
	for _, st := range pipe.Steps {
		for _, f := range st.Contract().Produces {
			out[f] = true
		}
	}
	return out
}

// refuseMissingGenerative refuses obj when a step of pipe needs an LLM or
// vision provider this build does not have: it was pruned for the missing
// capability, or it holds no usable provider, or only the stub. Run
// anyway, such a step produces heuristics or nothing, and the object
// would lose the LLM-made summaries, tags and mentions it has. A pipeline
// without a generative step is unaffected, and so is one whose user
// skip_steps removed the step. The embedding step is not judged here.
func refuseMissingGenerative(ctx context.Context, pipe *pipeline.Pipeline, id string) error {
	refuse := func(resource, step, why string) error {
		return fmt.Errorf("reanalyze: %w: object %q: %s provider %s for step %s",
			upgrade.ErrCannotReanalyze, id, resource, why, step)
	}
	for _, p := range pipe.Pruned {
		if res := p.Generative(); res != "" {
			return refuse(res, p.Name, "not available")
		}
	}
	for _, st := range pipe.Steps {
		ps, ok := pipeline.AsProviderStep(st)
		if !ok {
			if res := pipeline.GenerativeResourceOf(st.Contract()); res != "" {
				return refuse(res, st.Name(), "not available")
			}
			continue
		}
		for _, u := range ps.ProviderUses(ctx) {
			if u.Resource == pipeline.ResourceEmbedding {
				continue
			}
			switch {
			case u.Stub:
				return refuse(u.Resource, st.Name(), "is the stub backend")
			case !u.Usable:
				return refuse(u.Resource, st.Name(), "not available")
			}
		}
	}
	return nil
}

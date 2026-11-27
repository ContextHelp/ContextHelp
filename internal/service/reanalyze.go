// ReanalyzeObject re-runs the active pipeline against an
// existing object's RawContent and atomically replaces its projection.
// Used by the reingest_selective worker (ADR-070 §1) to upgrade
// objects whose stored projection was produced by an older pipeline version.
//
// Re-analysis is in-process and synchronous: unlike Analyze (which enqueues
// a worker job), ReanalyzeObject runs the pipeline steps inline so the
// caller can drive a deterministic for-loop over a selector's matching set
// without coordinating with the queue. The object's ID is preserved.

package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blobref"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// ReanalyzeObject re-runs the registry's currently-installed pipeline for
// the object's family against the stored RawContent, then atomically
// replaces the object's derived projection (Summaries, Sections, auto Tags and Mentions,
// Decisions, Tasks, Graph, step metadata) and bumps the `Pipeline` stamp to the
// new versioned name.
//
// Returns:
//   - newPipelineVersion: the versioned name that was stamped, e.g.
//     "text.short@v1". Always returned with a "@vN" suffix per ADR-070 §2.
//   - llmCostUSD: the LLM cost incurred during this re-analysis. Pipelines
//     that don't call LLMs return 0.0. Projection-only re-ingests always return 0.0; the
//     hook is in place so a future LLM-driven re-ingest can wire its
//     usage tracker through without a worker API change.
//   - err: any pipeline / storage failure.
//
// Errors:
//   - the object does not exist or has empty RawContent (cannot re-run);
//   - the registry has no pipeline that resolves to the object's family;
//   - the pipeline run itself fails;
//   - the object is encrypted or held (upgrade.ErrCannotReanalyze): nothing is written.
func (s *Service) ReanalyzeObject(ctx context.Context, id string) (newPipelineVersion string, llmCostUSD float64, err error) {
	return s.ReanalyzeObjectWith(ctx, id, ReanalyzeOptions{})
}

// ReanalyzeObjectWith is ReanalyzeObject with options. By default the
// steps that fetch from the network are not run: the object is re-derived
// from its stored content. A pipeline that cannot run without a fetch is
// refused with upgrade.ErrCannotReanalyze before anything is written.
func (s *Service) ReanalyzeObjectWith(ctx context.Context, id string, opts ReanalyzeOptions) (newPipelineVersion string, llmCostUSD float64, err error) {
	obj, err := s.Store(ctx).Objects().Get(ctx, id)
	if err != nil {
		return "", 0, fmt.Errorf("reanalyze: load %q: %w", id, err)
	}
	if obj == nil {
		return "", 0, fmt.Errorf("reanalyze: object %q not found", id)
	}
	if err := s.refuseReanalyze(ctx, obj); err != nil {
		return "", 0, err
	}
	src, err := s.reanalyzeSource(ctx, id, obj)
	if err != nil {
		return "", 0, err
	}

	pipe, versionedName, err := s.reanalyzePipeline(id, obj, opts.AllowNetwork)
	if err != nil {
		return "", 0, err
	}
	if err := refuseSourceReading(pipe, obj); err != nil {
		return "", 0, err
	}
	if err := refuseMissingGenerative(ctx, pipe, id); err != nil {
		return "", 0, err
	}

	// The draft is a full copy of the stored object; only the fields a
	// step of this pipeline produces are cleared.
	produced := producedFields(pipe)
	fetches := opts.AllowNetwork && slices.ContainsFunc(pipe.Steps, fetchesFromNetwork)
	draft, held := reanalyzeDraft(obj, versionedName, produced, stepNames(pipe), fetches)

	if err := keepOriginal(draft, pipe, src, id); err != nil {
		return "", 0, err
	}
	draft, err = runReanalysisSteps(ctx, id, pipe, draft)
	if err != nil {
		return "", 0, err
	}
	reattachHeldEdges(draft.Graph, held)

	if err := s.finishReanalysis(ctx, obj, draft); err != nil {
		return "", 0, fmt.Errorf("reanalyze: %q: %w", id, err)
	}
	draft.Pipeline = versionedName
	draft.UpdatedAt = time.Now().UTC()
	if err := s.persistReanalysis(ctx, draft); err != nil {
		return "", 0, fmt.Errorf("reanalyze: persist %q: %w", id, err)
	}
	// Replace the object's vectors for every model the embedding step
	// produced; other models' rows stay. Vectors are additive: a failed
	// write is logged, never fails the re-analysis.
	if len(draft.Vectors) > 0 {
		if err := s.Store(ctx).Embeddings().Put(ctx, id, draft.Vectors); err != nil {
			slog.Warn("reanalyze: embeddings not stored", "object", id, "err", err)
		}
	}

	// LLM cost: re-ingests are projection-only so always 0.0. The
	// non-zero return path is reserved for a follow-up that wires the
	// per-step cost meter through the pipeline.Run call.
	return versionedName, 0.0, nil
}

// reanalyzeSource is what a re-analysis reads before it runs: the object's
// recorded original, and its bytes when they could be read.
type reanalyzeSource struct {
	orig         blob.Original
	hasOrig      bool
	content      string // the original's bytes, when haveOriginal
	haveOriginal bool
}

// reanalyzeSource resolves obj's content and finds its original.
func (s *Service) reanalyzeSource(ctx context.Context, id string, obj *storage.KnowledgeObject) (reanalyzeSource, error) {
	// A captured medium is re-analysed from the bytes that were
	// captured, not from the text an earlier run left in RawContent.
	// When RawContent is the original's own ref, resolving it below
	// reads those bytes once; otherwise the original is read here.
	orig, hasOrig := blob.OriginalOf(obj)
	ownRef := false
	if key, ok := blobref.Key(obj.RawContent); ok && hasOrig && key == orig.Key {
		ownRef = true
	}
	// The pipeline runs on the content, not on its blob:// ref.
	if err := s.resolveContent(ctx, obj); err != nil {
		if errors.Is(err, storage.ErrBlobNotFound) {
			return reanalyzeSource{}, fmt.Errorf("reanalyze: %w: %w", upgrade.ErrCannotReanalyze, err)
		}
		return reanalyzeSource{}, fmt.Errorf("reanalyze: %w", err)
	}
	originalContent, haveOriginal := "", false
	if hasOrig {
		if ownRef {
			originalContent, haveOriginal = obj.RawContent, true
		} else {
			content, err := blob.Content(ctx, s.Store(ctx).Blobs(), blobref.For(orig.Key))
			switch {
			case err == nil:
				originalContent, haveOriginal = content, true
			case errors.Is(err, storage.ErrBlobNotFound):
				// Keep going on the stored text, with the original's
				// recorded keys: one lost blob must not end an upgrade run.
				slog.Warn("reanalyze: original blob missing, using stored content",
					"object", id, "blob", orig.Key)
			default:
				return reanalyzeSource{}, fmt.Errorf("reanalyze: read original of %q: %w", id, err)
			}
		}
	}
	if obj.RawContent == "" && !haveOriginal {
		// Re-running the pipeline against an empty RawContent would
		// destroy the projection without rebuilding it. Refuse loudly so
		// the worker can mark this object as "skip, can't re-ingest" rather
		// than silently corrupt it.
		return reanalyzeSource{}, fmt.Errorf("reanalyze: %w: object %q has empty RawContent (cannot re-run pipeline)", upgrade.ErrCannotReanalyze, id)
	}
	return reanalyzeSource{orig: orig, hasOrig: hasOrig, content: originalContent, haveOriginal: haveOriginal}, nil
}

// reanalyzePipeline picks the registry's current pipeline for obj's family
// and the versioned name to stamp on the re-analysed object.
func (s *Service) reanalyzePipeline(id string, obj *storage.KnowledgeObject, allowNetwork bool) (*pipeline.Pipeline, string, error) {
	// Resolve the family from the stored stamp (never empty: refuseReanalyze).
	family := obj.Pipeline
	if parsed, _, ok := pipeline.ParseVersionedName(obj.Pipeline); ok && parsed != "" {
		family = parsed
	}
	return s.runnablePipeline(fmt.Sprintf("object %q", id), family, allowNetwork)
}

// runnablePipeline is the pipeline a reanalysis of family runs: the
// registry's current one, less the steps skipped on stored content (or
// none, with allowNetwork). The classification of a reanalysis uses it
// too, so the two cannot drift.
func (s *Service) runnablePipeline(subject, family string, allowNetwork bool) (*pipeline.Pipeline, string, error) {

	// Pick the registry's currently-installed version for this family.
	// CurrentVersionForFamily returns the highest @vN registered. Fall
	// back to v0 when nothing matches (e.g. a custom pipeline with a bare
	// registration only) — pipeline.Registry.Get resolves that back to
	// the bare registration via the @v0 alias.
	curVer, known := CurrentVersionForFamily(s.Pipes, family)
	if !known {
		// CurrentVersionForFamily returned !known when no name in the
		// registry resolves to this family. Surface as a plain error
		// rather than re-detecting — the worker selector matched on
		// `pipeline=<family>@v0` so the caller is asserting this family
		// exists.
		return nil, "", fmt.Errorf("reanalyze: no pipeline registered for family %q (%s)", family, subject)
	}
	versionedName := pipeline.FormatVersionedName(family, curVer)

	pipe, err := s.Pipes.Get(versionedName)
	if err != nil {
		// Try bare; the registry resolves bare→@v0 via ParseVersionedName.
		pipe, err = s.Pipes.Get(family)
		if err != nil {
			return nil, "", fmt.Errorf("reanalyze: lookup pipeline %q: %w", versionedName, err)
		}
	}
	pipe, err = storedContentPipeline(pipe, subject, allowNetwork)
	if err != nil {
		return nil, "", err
	}
	return pipe, versionedName, nil
}

// keepOriginal records the object's original on the draft, whether or not
// its bytes could be read: without it the update would free the blob. When
// the original is gone and the stored text stands in for it, a step that
// reads the media would run on text: the object is skipped instead.
func keepOriginal(draft *storage.KnowledgeObject, pipe *pipeline.Pipeline, src reanalyzeSource, id string) error {
	if !src.hasOrig {
		return nil
	}
	if src.haveOriginal {
		draft.RawContent = src.content
	}
	if err := blob.AttachOriginal(draft, src.orig, src.content); err != nil {
		return fmt.Errorf("reanalyze: %w", err)
	}
	if src.haveOriginal {
		return nil
	}
	for _, step := range pipe.Steps {
		if pipeline.ReadsMediaBytes(step) {
			return fmt.Errorf("reanalyze: %w: original of %q is lost and step %s reads the media",
				upgrade.ErrCannotReanalyze, id, step.Name())
		}
	}
	return nil
}

// runReanalysisSteps runs the steps over draft. Steps swallow a provider
// outage or an unusable reply and produce nothing; the call recorder tells
// that apart from content with nothing to enrich, and the run then fails
// before anything is written. The text then follows the final content.
func runReanalysisSteps(ctx context.Context, id string, pipe *pipeline.Pipeline, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	runCtx, calls := providers.WithCallRecorder(ctx)
	preContent, preText := pipeline.ContentBytes(draft), draft.TextContent
	draft, err := pipeline.RunSteps(runCtx, pipe, draft)
	if err != nil {
		return nil, fmt.Errorf("reanalyze: %w", err)
	}
	if err := calls.Err(); err != nil {
		return nil, fmt.Errorf("reanalyze: %q: provider call failed, nothing written: %w", id, err)
	}
	// Text derived before a step changed the content is not the new
	// content's. Then the text follows the final content as storage defaults
	// it at ingest (projection.BodyText): the step's text, else the content.
	if pipeline.ContentBytes(draft) != preContent && draft.TextContent == preText {
		draft.TextContent = ""
	}
	if !blobref.NeedsBlob(pipeline.ContentBytes(draft)) {
		draft.TextContent = projection.BodyText(draft)
	}
	return draft, nil
}

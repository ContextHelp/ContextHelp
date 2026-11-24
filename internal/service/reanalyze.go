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
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blobref"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// ReanalyzeObject re-runs the registry's currently-installed pipeline for
// the object's family against the stored RawContent, then atomically
// replaces the object's projection (Summaries, Sections, Tags, Mentions,
// Decisions, Tasks, Metadata, Graph) and bumps the `Pipeline` stamp to the
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
//   - the pipeline run itself fails.
func (s *Service) ReanalyzeObject(ctx context.Context, id string) (newPipelineVersion string, llmCostUSD float64, err error) {
	obj, err := s.Store(ctx).Objects().Get(ctx, id)
	if err != nil {
		return "", 0, fmt.Errorf("reanalyze: load %q: %w", id, err)
	}
	if obj == nil {
		return "", 0, fmt.Errorf("reanalyze: object %q not found", id)
	}
	src, err := s.reanalyzeSource(ctx, id, obj)
	if err != nil {
		return "", 0, err
	}
	orig, hasOrig, originalContent, haveOriginal := src.orig, src.hasOrig, src.content, src.haveOriginal

	pipe, versionedName, err := s.reanalyzePipeline(id, obj)
	if err != nil {
		return "", 0, err
	}

	// Build a draft from the existing object: keep the ID + RawContent, drop
	// the projection so steps that merge will produce fresh output. Identity
	// fields (Source, SourceKey, ProfileID, ContentHash, CreatedAt) are
	// preserved so post-step persistence updates the SAME row, not a new one.
	draft := &storage.KnowledgeObject{
		ID:                 obj.ID,
		Type:               obj.Type,
		Subtype:            obj.Subtype,
		RawContent:         obj.RawContent,
		ContentType:        obj.ContentType,
		Source:             obj.Source,
		SourceKey:          obj.SourceKey,
		ContentHash:        obj.ContentHash,
		ProfileID:          obj.ProfileID,
		Status:             obj.Status,
		ReinforcementCount: obj.ReinforcementCount,
		LastReinforcedAt:   obj.LastReinforcedAt,
		CreatedAt:          obj.CreatedAt,
		Pipeline:           versionedName,
	}

	// The original stays recorded on the object, whether or not its
	// bytes could be read: without it the update would free the blob.
	if hasOrig {
		if haveOriginal {
			draft.RawContent = originalContent
		}
		if err := blob.AttachOriginal(draft, orig, originalContent); err != nil {
			return "", 0, fmt.Errorf("reanalyze: %w", err)
		}
	}

	// The original is gone and the stored text stands in for it: a step
	// that reads the media would run on text. Skip the object instead.
	if hasOrig && !haveOriginal {
		for _, step := range pipe.Steps {
			if pipeline.ReadsMediaBytes(step) {
				return "", 0, fmt.Errorf("reanalyze: %w: original of %q is lost and step %s reads the media",
					upgrade.ErrCannotReanalyze, id, step.Name())
			}
		}
	}

	draft, err = pipeline.RunSteps(ctx, pipe, draft)
	if err != nil {
		return "", 0, fmt.Errorf("reanalyze: %w", err)
	}

	// The ambient envelope (session id, ambient_source, fingerprint,
	// metadata.client) is capture-side data no step regenerates: carry it.
	storageutil.CarryAmbient(draft, obj)

	// Stamp the version + updated time and persist atomically. Update writes
	// every projection column so the new pipeline output overwrites the old.
	draft.Pipeline = versionedName
	draft.UpdatedAt = time.Now().UTC()
	// Content above the threshold is stored externalized, whether or
	// not the pipeline has an externalize_content step.
	row, err := s.storable(ctx, draft)
	if err != nil {
		return "", 0, fmt.Errorf("reanalyze: persist %q: %w", id, err)
	}
	// The blob of content the pipeline replaced goes with it.
	orphans, err := blob.UpdateObject(ctx, s.Store(ctx), row)
	if err != nil {
		return "", 0, fmt.Errorf("reanalyze: persist %q: %w", id, err)
	}
	logOrphans("reanalyze", id, orphans)
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
func (s *Service) reanalyzePipeline(id string, obj *storage.KnowledgeObject) (*pipeline.Pipeline, string, error) {
	// Resolve the family from the stored stamp. Fallback path: if the stamp
	// is malformed or empty, lean on the detector chain — the worker should
	// still be able to upgrade an object whose previous stamp got corrupted.
	family := obj.Pipeline
	if parsed, _, ok := pipeline.ParseVersionedName(obj.Pipeline); ok && parsed != "" {
		family = parsed
	}
	if family == "" {
		family = s.detectPipeline(obj.Source, obj.ContentType, obj.RawContent)
	}

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
		return nil, "", fmt.Errorf("reanalyze: no pipeline registered for family %q (object %q)", family, id)
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
	return pipe, versionedName, nil
}

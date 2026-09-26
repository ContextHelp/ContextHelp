// Package service: ReanalyzeObject re-runs the active pipeline against an
// existing object's RawContent and atomically replaces its projection.
// Used by the reingest_selective worker (ADR-070 §1, T-0581) to upgrade
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
//     that don't call LLMs return 0.0. T-0581 always returns 0.0; the
//     hook is in place so a future LLM-driven re-ingest can wire its
//     usage tracker through without a worker API change.
//   - err: any pipeline / storage failure.
//
// Errors:
//   - the object does not exist or has empty RawContent (cannot re-run);
//   - the registry has no pipeline that resolves to the object's family;
//   - the pipeline run itself fails.
func (s *Service) ReanalyzeObject(ctx context.Context, id string) (newPipelineVersion string, llmCostUSD float64, err error) {
	obj, err := s.Store.Objects().Get(ctx, id)
	if err != nil {
		return "", 0, fmt.Errorf("reanalyze: load %q: %w", id, err)
	}
	if obj == nil {
		return "", 0, fmt.Errorf("reanalyze: object %q not found", id)
	}
	if obj.RawContent == "" {
		// Re-running the pipeline against an empty RawContent would
		// destroy the projection without rebuilding it. Refuse loudly so
		// the worker can mark this object as "skip, can't re-ingest" rather
		// than silently corrupt it.
		return "", 0, fmt.Errorf("reanalyze: object %q has empty RawContent (cannot re-run pipeline)", id)
	}

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
	// back to v0 when nothing matches (e.g. a custom plugin with a bare
	// registration only) — pipeline.Registry.Get resolves that back to
	// the bare registration via the @v0 alias.
	curVer, known := CurrentVersionForFamily(s.Pipes, family)
	if !known {
		// CurrentVersionForFamily returned !known when no name in the
		// registry resolves to this family. Surface as a plain error
		// rather than re-detecting — the worker selector matched on
		// `pipeline=<family>@v0` so the caller is asserting this family
		// exists.
		return "", 0, fmt.Errorf("reanalyze: no pipeline registered for family %q (object %q)", family, id)
	}
	versionedName := pipeline.FormatVersionedName(family, curVer)

	pipe, err := s.Pipes.Get(versionedName)
	if err != nil {
		// Try bare; the registry resolves bare→@v0 via ParseVersionedName.
		pipe, err = s.Pipes.Get(family)
		if err != nil {
			return "", 0, fmt.Errorf("reanalyze: lookup pipeline %q: %w", versionedName, err)
		}
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

	for _, step := range pipe.Steps {
		out, runErr := step.Run(ctx, draft)
		if runErr != nil {
			if errors.Is(runErr, pipeline.ErrDelegate) {
				continue
			}
			return "", 0, fmt.Errorf("reanalyze step %s: %w", step.Name(), runErr)
		}
		draft = out
	}

	// Stamp the version + updated time and persist atomically. Update writes
	// every projection column so the new pipeline output overwrites the old.
	draft.Pipeline = versionedName
	draft.UpdatedAt = time.Now().UTC()
	if err := s.Store.Objects().Update(ctx, draft); err != nil {
		return "", 0, fmt.Errorf("reanalyze: persist %q: %w", id, err)
	}
	// Replace the object's vectors for every model the embedding step
	// produced; other models' rows stay. Vectors are additive: a failed
	// write is logged, never fails the re-analysis.
	if len(draft.Vectors) > 0 {
		if err := s.Store.Embeddings().Put(ctx, id, draft.Vectors); err != nil {
			slog.Warn("reanalyze: embeddings not stored", "object", id, "err", err)
		}
	}

	// LLM cost: T-0581 ships projection-only re-ingests so always 0.0. The
	// non-zero return path is reserved for a follow-up that wires the
	// per-step cost meter through the pipeline.Run call.
	return versionedName, 0.0, nil
}

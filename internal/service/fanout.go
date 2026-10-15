package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// FanOutResult summarises what the fan-out enrichment produced.
type FanOutResult struct {
	ObjectID     string   `json:"object_id"`
	EdgesCreated int      `json:"edges_created"`
	EdgesSkipped int      `json:"edges_skipped"`
	AuditEntries int      `json:"audit_entries"`
	EntitySlugs  []string `json:"entity_slugs,omitempty"`
}

// FanOut runs post-ingest enrichment for a knowledge object.
//
// It reads the object's Mentions, creates bidirectional cross-reference
// edges (object->entity, entity->object) when they don't already exist,
// and appends audit log entries recording each mutation. The operation is
// idempotent: re-running against the same object skips duplicate edges.
func (s *Service) FanOut(ctx context.Context, objectID string) (*FanOutResult, error) {
	cfg := s.Cfg.FanOut

	obj, err := s.Store(ctx).Objects().Get(ctx, objectID)
	if err != nil {
		return nil, fmt.Errorf("fanout: get object: %w", err)
	}

	result := &FanOutResult{ObjectID: objectID}

	if !cfg.Entities || len(obj.Mentions) == 0 {
		return result, nil
	}

	now := time.Now()

	// Both directions name the entity by storage.MentionTarget, as every
	// mention-edge writer does, and carry the entity_mention node of
	// obj.Graph that holds the mention and the field it came from. The
	// store keeps an edge already there for the pair (the worker wrote
	// the forward ones), so a rerun creates nothing.
	edges := storage.MentionEdges(obj, obj.Mentions, true, now)
	created, err := s.Store(ctx).Edges().CreateMissing(ctx, edges)
	if err != nil {
		return nil, fmt.Errorf("fanout: create edges: %w", err)
	}
	result.EdgesCreated = created
	result.EdgesSkipped = len(edges) - created
	for _, e := range edges {
		if e.EdgeType == storage.EdgeTypeMentions {
			result.EntitySlugs = append(result.EntitySlugs, e.ToID)
		}
	}

	// Entity page upsert: update persistent pages for each mentioned entity.
	if cfg.Entities && len(result.EntitySlugs) > 0 {
		for _, slug := range result.EntitySlugs {
			if _, err := s.PageUpsert(ctx, slug, objectID); err != nil {
				return nil, fmt.Errorf("fanout: page upsert %s: %w", slug, err)
			}
		}
	}

	// Audit log entries.
	if cfg.AuditLog && result.EdgesCreated > 0 {
		entry := &storage.AuditEntry{
			ID:        uuid.New().String(),
			EventType: "fanout.completed",
			ObjectID:  objectID,
			Actor:     "system",
			Payload: map[string]any{
				"edges_created": result.EdgesCreated,
				"edges_skipped": result.EdgesSkipped,
				"entity_slugs":  result.EntitySlugs,
			},
			CreatedAt: now,
		}
		if err := s.Store(ctx).AuditLog().Append(ctx, entry); err != nil {
			return nil, fmt.Errorf("fanout: audit log: %w", err)
		}
		result.AuditEntries++
	}

	return result, nil
}

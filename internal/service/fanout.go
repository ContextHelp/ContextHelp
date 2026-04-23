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

	obj, err := s.Store.Objects().Get(ctx, objectID)
	if err != nil {
		return nil, fmt.Errorf("fanout: get object: %w", err)
	}

	result := &FanOutResult{ObjectID: objectID}

	if !cfg.Entities || len(obj.Mentions) == 0 {
		return result, nil
	}

	// Collect existing outbound edges to avoid duplicates.
	existing, err := s.Store.Edges().ListFrom(ctx, "object", objectID)
	if err != nil {
		return nil, fmt.Errorf("fanout: list edges: %w", err)
	}
	edgeIndex := make(map[string]bool, len(existing))
	for _, e := range existing {
		edgeIndex[edgeKey(e.FromType, e.FromID, e.ToType, e.ToID, e.EdgeType)] = true
	}

	// Collect existing inbound edges for reverse dedup.
	existingInbound, err := s.Store.Edges().ListTo(ctx, "object", objectID)
	if err != nil {
		return nil, fmt.Errorf("fanout: list inbound edges: %w", err)
	}
	for _, e := range existingInbound {
		edgeIndex[edgeKey(e.FromType, e.FromID, e.ToType, e.ToID, e.EdgeType)] = true
	}

	now := time.Now()

	for _, mention := range obj.Mentions {
		slug := mention.String()
		result.EntitySlugs = append(result.EntitySlugs, slug)

		// Forward edge: object -> entity.
		fwdKey := edgeKey("object", objectID, "entity", slug, "mentions")
		if !edgeIndex[fwdKey] {
			edge := &storage.Edge{
				ID:        uuid.New().String(),
				FromType:  "object",
				FromID:    objectID,
				ToType:    "entity",
				ToID:      slug,
				EdgeType:  "mentions",
				Weight:    1.0,
				CreatedAt: now,
			}
			if err := s.Store.Edges().Create(ctx, edge); err != nil {
				return nil, fmt.Errorf("fanout: create fwd edge: %w", err)
			}
			edgeIndex[fwdKey] = true
			result.EdgesCreated++
		} else {
			result.EdgesSkipped++
		}

		// Reverse edge: entity -> object.
		revKey := edgeKey("entity", slug, "object", objectID, "mentioned_in")
		if !edgeIndex[revKey] {
			edge := &storage.Edge{
				ID:        uuid.New().String(),
				FromType:  "entity",
				FromID:    slug,
				ToType:    "object",
				ToID:      objectID,
				EdgeType:  "mentioned_in",
				Weight:    1.0,
				CreatedAt: now,
			}
			if err := s.Store.Edges().Create(ctx, edge); err != nil {
				return nil, fmt.Errorf("fanout: create rev edge: %w", err)
			}
			edgeIndex[revKey] = true
			result.EdgesCreated++
		} else {
			result.EdgesSkipped++
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
		if err := s.Store.AuditLog().Append(ctx, entry); err != nil {
			return nil, fmt.Errorf("fanout: audit log: %w", err)
		}
		result.AuditEntries++
	}

	return result, nil
}

func edgeKey(fromType, fromID, toType, toID, edgeType string) string {
	return fromType + ":" + fromID + "->" + toType + ":" + toID + ":" + edgeType
}

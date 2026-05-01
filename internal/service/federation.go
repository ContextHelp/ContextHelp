package service

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// FederationAccept stores incoming pushed objects, edges, and entities,
// deduplicating by ContentHash on objects and slug-based UpsertThin on
// entities. Mirrors LocalPusher dedup logic: GetByContentHash → skip if
// exists, Create if not.
//
// Returns count of newly created objects (skipped dupes not counted).
//
// T-0175: entities are upserted thin so a 'full' record at the receiver
// is preserved (ADR-049 + EntityStore.UpsertThin), while mention edges
// (object→entity) gain valid targets.
func (s *Service) FederationAccept(
	ctx context.Context,
	objects []storage.KnowledgeObject,
	edges []storage.Edge,
	entities []storage.Entity,
) (int, error) {
	objs := s.Store.Objects()
	edgeStore := s.Store.Edges()
	entityStore := s.Store.Entities()

	for i := range entities {
		ent := &entities[i]
		if ent.Slug == "" {
			continue
		}
		if err := entityStore.UpsertThin(ctx, ent); err != nil {
			return 0, fmt.Errorf("federation accept: upsert entity %q: %w",
				ent.Slug, err)
		}
	}

	created := 0
	insertedEdges := make(map[string]bool)

	for i := range objects {
		obj := &objects[i]
		if obj.ContentHash != "" {
			existing, _ := objs.GetByContentHash(ctx, obj.ContentHash)
			if existing != nil {
				continue // dedup — already present
			}
		}
		if err := objs.Create(ctx, obj); err != nil {
			return created, fmt.Errorf("federation accept: create object %q: %w", obj.ID, err)
		}
		created++

		for j := range edges {
			e := &edges[j]
			if e.FromID != obj.ID && e.ToID != obj.ID {
				continue
			}
			if insertedEdges[e.ID] {
				continue
			}
			if err := edgeStore.Create(ctx, e); err != nil {
				return created, fmt.Errorf("federation accept: create edge %q: %w", e.ID, err)
			}
			insertedEdges[e.ID] = true
		}
	}
	return created, nil
}

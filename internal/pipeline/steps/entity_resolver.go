package steps

import (
	"context"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	uri "hop.top/cite/scheme"
)

// EntityResolver is a pipeline step that resolves @mention URIs against the
// entity registry, creates placeholder entities for unresolved mentions, and
// writes backlink edges from the knowledge object to each entity.
//
// If either store is nil the step acts as a no-op passthrough (safe for tests
// or environments without a live database).
type EntityResolver struct {
	pipeline.BaseContract
	entities storage.EntityStore
	edges    storage.EdgeStore
}

// NewEntityResolver creates an EntityResolver with nil stores (no-op mode).
func NewEntityResolver() *EntityResolver {
	return &EntityResolver{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"Mentions"},
			Produces: []string{"Metadata"},
		}),
	}
}

// NewEntityResolverWithStores creates an EntityResolver that performs live
// entity lookup/creation and edge writing.
func NewEntityResolverWithStores(entities storage.EntityStore, edges storage.EdgeStore) *EntityResolver {
	r := NewEntityResolver()
	r.entities = entities
	r.edges = edges
	return r
}

func (r *EntityResolver) Name() string { return "entity_resolver" }

func (r *EntityResolver) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	// No-op when stores are absent or no mentions to resolve.
	if r.entities == nil || r.edges == nil || len(draft.Mentions) == 0 {
		return draft, nil
	}

	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	var resolved, placeholder []string
	// targets are the mentions that resolved to an entity or got a placeholder.
	targets := make([]uri.URI, 0, len(draft.Mentions))

	for _, u := range draft.Mentions {
		slug := storage.MentionSlug(u)
		if slug == "" {
			continue
		}

		entity, created, err := storage.EnsureMentionEntity(ctx, r.entities, u, time.Now().UTC())
		if err != nil {
			// Non-fatal: log and continue.
			fmt.Printf("entity_resolver: ensure entity %q: %v\n", slug, err)
			continue
		}
		if created {
			placeholder = append(placeholder, slug)
		} else {
			resolved = append(resolved, entity.Slug)
		}
		targets = append(targets, u)
	}

	// Inter-object layer only: the backlink edges go to the edges table
	// (ADR-049/ADR-063), one per entity, naming it by storage.MentionTarget
	// as every mention-edge writer does; the store keeps an edge already
	// there for the pair. Intra-object NodeTypeEntityMention nodes live in
	// ko.Graph and are written by entity_extractor; each edge names the one
	// holding its mention as its provenance.
	if draft.ID != "" {
		edges := storage.MentionEdges(draft, targets, false, time.Now().UTC())
		if _, err := r.edges.CreateMissing(ctx, edges); err != nil {
			fmt.Printf("entity_resolver: create mention edges of %q: %v\n", draft.ID, err)
		}
	}

	if len(resolved) > 0 {
		draft.Metadata["resolved_entities"] = resolved
	}
	if len(placeholder) > 0 {
		draft.Metadata["placeholder_entities"] = placeholder
	}

	return draft, nil
}

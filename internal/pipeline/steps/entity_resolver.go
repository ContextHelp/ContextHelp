package steps

import (
	"context"
	"fmt"
	"strings"
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

		entity, err := r.entities.Resolve(ctx, slug)
		if err != nil {
			// Unresolved: create a local placeholder entity.
			// Derive namespace from the first path segment of the slug.
			ns := strings.SplitN(slug, "/", 2)[0]
			if _, perr := r.createPlaceholder(ctx, ns, slug); perr != nil {
				// Non-fatal: log and continue.
				fmt.Printf("entity_resolver: create placeholder %q: %v\n", slug, perr)
				continue
			}
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

// createPlaceholder inserts a thin local placeholder entity and returns it.
func (r *EntityResolver) createPlaceholder(ctx context.Context, namespace, slug string) (*storage.Entity, error) {
	now := time.Now().UTC()

	// Derive a human-readable title from the slug (last segment, dashes→spaces).
	segments := strings.Split(slug, "/")
	last := segments[len(segments)-1]
	title := strings.ReplaceAll(last, "-", " ")
	title = strings.ReplaceAll(title, "_", " ")

	entity := &storage.Entity{
		Slug:          slug,
		Title:         title,
		Namespace:     namespace,
		ContentStatus: storage.ContentStatusThin,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := r.entities.UpsertThin(ctx, entity); err != nil {
		return nil, err
	}
	return entity, nil
}

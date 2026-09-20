package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// EntityPageType is the KnowledgeObject.Type for entity pages.
const EntityPageType = "entity_page"

// EntityPageMeta is stored in KnowledgeObject.Metadata["entity_page"].
type EntityPageMeta struct {
	EntitySlug    string          `json:"entity_slug"`
	SourceIDs     []string        `json:"source_ids"`
	RevisionCount int             `json:"revision_count"`
	RevisionLog   []RevisionEntry `json:"revision_log"`
}

// RevisionEntry records one incremental update to an entity page.
type RevisionEntry struct {
	Date     time.Time `json:"date"`
	SourceID string    `json:"source_id"`
	Delta    string    `json:"delta"`
}

// PageUpsertResult summarises the page upsert outcome.
type PageUpsertResult struct {
	PageID   string `json:"page_id"`
	Created  bool   `json:"created"`
	Revision int    `json:"revision"`
}

// PageUpsert finds or creates an entity_page KnowledgeObject for the
// given entity slug, appending sourceObjectID as a contributing source.
// Content is compiled as a summary from all source objects.
func (s *Service) PageUpsert(
	ctx context.Context, entitySlug, sourceObjectID string,
) (*PageUpsertResult, error) {
	existing, err := s.findEntityPage(ctx, entitySlug)
	if err != nil {
		return nil, fmt.Errorf("page upsert: find: %w", err)
	}

	if existing != nil {
		return s.updateEntityPage(ctx, existing, entitySlug, sourceObjectID)
	}
	return s.createEntityPage(ctx, entitySlug, sourceObjectID)
}

// findEntityPage locates an existing entity_page by scanning objects
// with Type == "entity_page" and matching entity_slug in Metadata.
func (s *Service) findEntityPage(
	ctx context.Context, entitySlug string,
) (*storage.KnowledgeObject, error) {
	objs, _, err := s.Store.Objects().List(ctx, storage.ObjectFilter{
		Type:   EntityPageType,
		Limit:  500,
		Status: "all",
	})
	if err != nil {
		return nil, err
	}
	for _, obj := range objs {
		meta, ok := ExtractPageMeta(obj)
		if ok && meta.EntitySlug == entitySlug {
			return obj, nil
		}
	}
	return nil, nil
}

// createEntityPage builds a new entity_page KnowledgeObject from the
// first source object.
func (s *Service) createEntityPage(
	ctx context.Context, entitySlug, sourceObjectID string,
) (*PageUpsertResult, error) {
	sourceObj, err := s.Store.Objects().Get(ctx, sourceObjectID)
	if err != nil {
		return nil, fmt.Errorf("page create: get source: %w", err)
	}

	now := time.Now().Truncate(time.Second)
	snippet := objectSnippet(sourceObj)

	meta := EntityPageMeta{
		EntitySlug:    entitySlug,
		SourceIDs:     []string{sourceObjectID},
		RevisionCount: 1,
		RevisionLog: []RevisionEntry{{
			Date:     now,
			SourceID: sourceObjectID,
			Delta:    "initial page creation",
		}},
	}

	content := compilePageContent(entitySlug, []string{snippet})

	obj := &storage.KnowledgeObject{
		ID:         uuid.New().String(),
		Type:       EntityPageType,
		RawContent: content,
		Source:     "entity_page:" + entitySlug,
		Status:     "active",
		Metadata:   map[string]any{"entity_page": toMap(meta)},
		Tags:       []storage.Tag{{Label: "entity_page"}, {Label: entitySlug}},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.Store.Objects().Create(ctx, obj); err != nil {
		return nil, fmt.Errorf("page create: store: %w", err)
	}

	return &PageUpsertResult{
		PageID:   obj.ID,
		Created:  true,
		Revision: 1,
	}, nil
}

// updateEntityPage appends a source and increments the revision.
func (s *Service) updateEntityPage(
	ctx context.Context,
	page *storage.KnowledgeObject,
	entitySlug, sourceObjectID string,
) (*PageUpsertResult, error) {
	meta, _ := ExtractPageMeta(page)

	// Skip if source already recorded.
	for _, sid := range meta.SourceIDs {
		if sid == sourceObjectID {
			return &PageUpsertResult{
				PageID:   page.ID,
				Created:  false,
				Revision: meta.RevisionCount,
			}, nil
		}
	}

	sourceObj, err := s.Store.Objects().Get(ctx, sourceObjectID)
	if err != nil {
		return nil, fmt.Errorf("page update: get source: %w", err)
	}

	now := time.Now().Truncate(time.Second)
	snippet := objectSnippet(sourceObj)
	delta := truncate(snippet, 200)

	meta.SourceIDs = append(meta.SourceIDs, sourceObjectID)
	meta.RevisionCount++
	meta.RevisionLog = append(meta.RevisionLog, RevisionEntry{
		Date:     now,
		SourceID: sourceObjectID,
		Delta:    delta,
	})

	// Collect all source snippets for recompilation.
	snippets := s.collectSourceSnippets(ctx, meta.SourceIDs)
	page.RawContent = compilePageContent(entitySlug, snippets)
	page.Metadata["entity_page"] = toMap(meta)
	page.UpdatedAt = now

	if err := s.Store.Objects().Update(ctx, page); err != nil {
		return nil, fmt.Errorf("page update: store: %w", err)
	}

	return &PageUpsertResult{
		PageID:   page.ID,
		Created:  false,
		Revision: meta.RevisionCount,
	}, nil
}

// GetEntityPage returns the entity page for the given slug, or nil.
func (s *Service) GetEntityPage(
	ctx context.Context, entitySlug string,
) (*storage.KnowledgeObject, error) {
	return s.findEntityPage(ctx, entitySlug)
}

// ListEntityPages returns all entity_page objects.
func (s *Service) ListEntityPages(
	ctx context.Context, limit int,
) ([]*storage.KnowledgeObject, error) {
	if limit <= 0 {
		limit = 50
	}
	objs, _, err := s.Store.Objects().List(ctx, storage.ObjectFilter{
		Type:   EntityPageType,
		Limit:  limit,
		Status: "all",
	})
	return objs, err
}

// RefreshEntityPage regenerates page content from all source objects.
func (s *Service) RefreshEntityPage(
	ctx context.Context, entitySlug string,
) (*storage.KnowledgeObject, error) {
	page, err := s.findEntityPage(ctx, entitySlug)
	if err != nil {
		return nil, err
	}
	if page == nil {
		return nil, fmt.Errorf("entity page not found: %s", entitySlug)
	}

	meta, _ := ExtractPageMeta(page)
	snippets := s.collectSourceSnippets(ctx, meta.SourceIDs)
	page.RawContent = compilePageContent(entitySlug, snippets)
	page.UpdatedAt = time.Now().Truncate(time.Second)

	if err := s.Store.Objects().Update(ctx, page); err != nil {
		return nil, fmt.Errorf("refresh page: %w", err)
	}
	return page, nil
}

// collectSourceSnippets gathers text snippets from source object IDs.
func (s *Service) collectSourceSnippets(
	ctx context.Context, sourceIDs []string,
) []string {
	snippets := make([]string, 0, len(sourceIDs))
	for _, id := range sourceIDs {
		obj, err := s.Store.Objects().Get(ctx, id)
		if err != nil {
			continue
		}
		if snip := objectSnippet(obj); snip != "" {
			snippets = append(snippets, snip)
		}
	}
	return snippets
}

// compilePageContent builds markdown from entity slug + source snippets.
func compilePageContent(slug string, snippets []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Entity: %s\n\n", slug)
	fmt.Fprintf(&b, "Compiled from %d sources.\n\n", len(snippets))
	for i, s := range snippets {
		fmt.Fprintf(&b, "## Source %d\n\n%s\n\n", i+1, s)
	}
	return b.String()
}

// ExtractPageMeta reads EntityPageMeta from obj.Metadata["entity_page"].
func ExtractPageMeta(obj *storage.KnowledgeObject) (EntityPageMeta, bool) {
	var meta EntityPageMeta
	raw, ok := obj.Metadata["entity_page"]
	if !ok {
		return meta, false
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return meta, false
	}
	if err := json.Unmarshal(b, &meta); err != nil {
		return meta, false
	}
	return meta, true
}

// toMap converts a struct to map[string]any via JSON round-trip.
func toMap(v any) map[string]any {
	b, _ := json.Marshal(v)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

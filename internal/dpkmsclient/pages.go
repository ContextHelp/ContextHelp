package dpkmsclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// EntityPageMeta is the entity_page block of an entity page's metadata.
// It mirrors dpkms's page metadata on the wire; a test holds the two in
// step.
type EntityPageMeta struct {
	EntitySlug    string          `json:"entity_slug"`
	SourceIDs     []string        `json:"source_ids"`
	RevisionCount int             `json:"revision_count"`
	RevisionLog   []RevisionEntry `json:"revision_log"`
}

// RevisionEntry records one update to an entity page.
type RevisionEntry struct {
	Date     time.Time `json:"date"`
	SourceID string    `json:"source_id"`
	Delta    string    `json:"delta"`
}

// PageMeta reads the entity page metadata of obj; ok is false when obj
// carries none.
func PageMeta(obj *storage.KnowledgeObject) (meta EntityPageMeta, ok bool) {
	raw, found := obj.Metadata["entity_page"]
	if !found {
		return meta, false
	}
	b, err := json.Marshal(raw)
	if err != nil || json.Unmarshal(b, &meta) != nil {
		return EntityPageMeta{}, false
	}
	return meta, true
}

// pagePath is /api/v1/pages/{slug}, with slug escaped.
func pagePath(slug string) string { return "/api/v1/pages/" + url.PathEscape(slug) }

// ListPages returns up to limit entity pages (GET /api/v1/pages), newest
// first. limit 0 takes dpkms's default of 50.
func (c *Client) ListPages(ctx context.Context, limit int) ([]*storage.KnowledgeObject, error) {
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	var out struct {
		Data []*storage.KnowledgeObject `json:"data"`
	}
	if err := c.Get(ctx, "/api/v1/pages", q, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// GetPage returns the entity page of slug. A slug without a page is a
// NOT_FOUND error (exit 3).
func (c *Client) GetPage(ctx context.Context, slug string) (*storage.KnowledgeObject, error) {
	var page storage.KnowledgeObject
	if err := c.Get(ctx, pagePath(slug), nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// RefreshPage recompiles the entity page of slug from its sources on
// dpkms and returns it (POST /api/v1/pages/{slug}/refresh, write:objects).
// A slug without a page is a NOT_FOUND error.
func (c *Client) RefreshPage(ctx context.Context, slug string) (*storage.KnowledgeObject, error) {
	var page storage.KnowledgeObject
	if err := c.Do(ctx, Request{Method: http.MethodPost, Path: pagePath(slug) + "/refresh"}, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

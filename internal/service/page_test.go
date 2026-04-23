package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func seedSourceObject(t *testing.T, svc *Service, id, content string) {
	t.Helper()
	ctx := context.Background()
	obj := &storage.KnowledgeObject{
		ID:         id,
		Type:       "text",
		RawContent: content,
		Status:     "active",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	require.NoError(t, svc.Store.Objects().Create(ctx, obj))
}

func TestPageUpsert_CreatesNewPage(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	seedSourceObject(t, svc, "src-1", "Alice is a key engineer")

	result, err := svc.PageUpsert(ctx, "person.alice", "src-1")
	require.NoError(t, err)
	assert.True(t, result.Created)
	assert.Equal(t, 1, result.Revision)
	assert.NotEmpty(t, result.PageID)

	// Verify page stored.
	page, err := svc.GetEntityPage(ctx, "person.alice")
	require.NoError(t, err)
	require.NotNil(t, page)
	assert.Equal(t, EntityPageType, page.Type)
	assert.Contains(t, page.RawContent, "person.alice")
	assert.Contains(t, page.RawContent, "Alice is a key engineer")

	meta, ok := ExtractPageMeta(page)
	require.True(t, ok)
	assert.Equal(t, "person.alice", meta.EntitySlug)
	assert.Equal(t, []string{"src-1"}, meta.SourceIDs)
	assert.Equal(t, 1, meta.RevisionCount)
}

func TestPageUpsert_IncrementsRevision(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	seedSourceObject(t, svc, "src-a", "First mention of Go")
	seedSourceObject(t, svc, "src-b", "Second mention of Go in prod")

	r1, err := svc.PageUpsert(ctx, "lang.go", "src-a")
	require.NoError(t, err)
	assert.True(t, r1.Created)

	r2, err := svc.PageUpsert(ctx, "lang.go", "src-b")
	require.NoError(t, err)
	assert.False(t, r2.Created)
	assert.Equal(t, 2, r2.Revision)
	assert.Equal(t, r1.PageID, r2.PageID)

	page, err := svc.GetEntityPage(ctx, "lang.go")
	require.NoError(t, err)

	meta, _ := ExtractPageMeta(page)
	assert.Equal(t, 2, meta.RevisionCount)
	assert.Len(t, meta.SourceIDs, 2)
	assert.Len(t, meta.RevisionLog, 2)
	assert.Contains(t, page.RawContent, "First mention")
	assert.Contains(t, page.RawContent, "Second mention")
}

func TestPageUpsert_Idempotent(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	seedSourceObject(t, svc, "src-idem", "Idempotent source")

	r1, err := svc.PageUpsert(ctx, "concept.idem", "src-idem")
	require.NoError(t, err)
	assert.Equal(t, 1, r1.Revision)

	r2, err := svc.PageUpsert(ctx, "concept.idem", "src-idem")
	require.NoError(t, err)
	assert.Equal(t, 1, r2.Revision)
	assert.False(t, r2.Created)
}

func TestListEntityPages(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	seedSourceObject(t, svc, "src-list-1", "content 1")
	seedSourceObject(t, svc, "src-list-2", "content 2")

	_, err := svc.PageUpsert(ctx, "org.acme", "src-list-1")
	require.NoError(t, err)
	_, err = svc.PageUpsert(ctx, "org.globex", "src-list-2")
	require.NoError(t, err)

	pages, err := svc.ListEntityPages(ctx, 50)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(pages), 2)
}

func TestRefreshEntityPage(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	seedSourceObject(t, svc, "src-ref-1", "Original content")

	_, err := svc.PageUpsert(ctx, "project.refresh", "src-ref-1")
	require.NoError(t, err)

	// Modify source content.
	src, _ := svc.Store.Objects().Get(ctx, "src-ref-1")
	src.RawContent = "Updated content after edit"
	require.NoError(t, svc.Store.Objects().Update(ctx, src))

	page, err := svc.RefreshEntityPage(ctx, "project.refresh")
	require.NoError(t, err)
	assert.Contains(t, page.RawContent, "Updated content after edit")
}

func TestRefreshEntityPage_NotFound(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	_, err := svc.RefreshEntityPage(ctx, "nonexistent.page")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestGetEntityPage_NotFound(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	page, err := svc.GetEntityPage(ctx, "no.such.page")
	require.NoError(t, err)
	assert.Nil(t, page)
}

func TestPageUpsert_SourceNotFound(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	_, err := svc.PageUpsert(ctx, "broken.ref", "nonexistent-src")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get source")
}

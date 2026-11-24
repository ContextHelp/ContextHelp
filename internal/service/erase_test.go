package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/ingest"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/local"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

func newEraseTestService(t *testing.T) (*Service, *sqlite.Driver, storage.BlobStore) {
	t.Helper()
	ctx := context.Background()
	bs, err := local.New(t.TempDir())
	require.NoError(t, err)
	drv, err := sqlite.New(":memory:", bs)
	require.NoError(t, err)
	require.NoError(t, drv.Init(ctx))
	t.Cleanup(func() { _ = drv.Close(ctx) })
	return New(drv, nil, nil, nil, "", nil), drv, bs
}

func TestEraseBySourceKey_DerivedAndBlobImmediate(t *testing.T) {
	ctx := sharing.WithViewer(context.Background(), sharing.Unrestricted)
	svc, drv, bs := newEraseTestService(t)
	now := time.Now().UTC().Truncate(time.Second)

	const (
		srcKey   = "bos://acct/records/42"
		sentinel = "SECRET-PII-SENTINEL-9942"
	)

	blobKey := strings.Repeat("a", 64)
	require.NoError(t, bs.Put(ctx, blobKey, strings.NewReader(sentinel), storage.BlobMeta{ContentType: "text/plain"}))
	require.NoError(t, drv.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "obj-root", Type: "note", RawContent: sentinel, TextContent: sentinel,
		Source: "bos", SourceKey: srcKey, Status: storage.ObjectStatusActive,
		Metadata: map[string]any{"blob_key": blobKey}, CreatedAt: now, UpdatedAt: now,
	}))
	for _, id := range []string{"obj-derived-1", "obj-derived-2"} {
		require.NoError(t, drv.Objects().Create(ctx, &storage.KnowledgeObject{
			ID: id, Type: "summary", RawContent: "derived " + id, TextContent: "derived " + id,
			Status: storage.ObjectStatusActive, CreatedAt: now, UpdatedAt: now,
		}))
	}
	require.NoError(t, drv.Edges().CreateAll(ctx, []*storage.Edge{
		{ID: "e1", FromType: "object", FromID: "obj-derived-1", ToType: "object", ToID: "obj-root", EdgeType: "derives_from", CreatedAt: now},
		{ID: "e2", FromType: "object", FromID: "obj-derived-2", ToType: "object", ToID: "obj-derived-1", EdgeType: "discovered_by", CreatedAt: now},
		{ID: "e3", FromType: "object", FromID: "obj-derived-1", ToType: "object", ToID: "obj-derived-2", EdgeType: "derives_from", CreatedAt: now},
	}))
	require.NoError(t, drv.Aliases().Create(ctx, &storage.Alias{Alias: "alias-root", ObjectID: "obj-root", Scope: "global", CreatedAt: now}))
	require.NoError(t, drv.Jobs().Create(ctx, &storage.Job{
		ID: "job-root-queued", Type: "analyze", Status: "pending", Payload: sentinel,
		UserSourceKey: srcKey, CreatedAt: now, UpdatedAt: now,
	}))

	res, err := svc.EraseBySourceKey(ctx, EraseBySourceOptions{
		SourceKey: srcKey, Tombstone: true, Source: "bos", ObligationID: "ob-123", ErasedBy: "bos:compliance",
	})
	require.NoError(t, err)
	require.Len(t, res.DeletedIDs, 3)
	require.True(t, res.TombstoneWritten)

	for _, id := range []string{"obj-root", "obj-derived-1", "obj-derived-2"} {
		_, gerr := drv.Objects().Get(ctx, id)
		require.ErrorIs(t, gerr, storage.ErrNotFound)
	}
	_, _, berr := bs.Get(ctx, blobKey)
	require.ErrorIs(t, berr, storage.ErrBlobNotFound)

	// Tombstone blocks direct Create and quiet-skips ingest.Runner.
	err = drv.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "obj-resurrect", Type: "note", RawContent: "try again", SourceKey: srcKey,
		Status: storage.ObjectStatusActive, CreatedAt: now, UpdatedAt: now,
	})
	require.ErrorIs(t, err, storage.ErrSourceErased)

	runner := ingest.NewRunner(drv.Objects(), ingest.WithTombstones(drv.Tombstones()))
	resIng, err := runner.Run(ctx, staticAdapter{objs: []ingest.Object{{ID: "42", SourceKey: srcKey, Type: "note", Content: "resurrect"}}})
	require.NoError(t, err)
	require.Zero(t, resIng.Errors)
	got, err := drv.Objects().GetBySourceKey(ctx, srcKey, "")
	require.NoError(t, err)
	require.Nil(t, got)

	_, jerr := drv.Jobs().Get(ctx, "job-root-queued")
	require.ErrorIs(t, jerr, storage.ErrNotFound)

	entries, _, err := drv.AuditLog().List(ctx, storage.AuditFilter{EventType: "object.erased"})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.NotContains(t, entries[0].ObjectID, sentinel)
}

func TestEraseBySourceKey_ScrubsPagesAndTopicIndex(t *testing.T) {
	ctx := sharing.WithViewer(context.Background(), sharing.Unrestricted)
	svc, drv, _ := newEraseTestService(t)
	now := time.Now().UTC().Truncate(time.Second)

	const (
		srcKey   = "bos://acct/records/99"
		sentinel = "SECRET-PAGE-SENTINEL-7788"
	)
	require.NoError(t, drv.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "obj-root", Type: "note", RawContent: sentinel, TextContent: sentinel,
		Source: "bos", SourceKey: srcKey, Status: storage.ObjectStatusActive,
		Metadata:  map[string]any{"enrichment.structured_metadata": map[string]any{"topics": []string{"finance"}}},
		Summaries: []string{"summary of " + sentinel},
		CreatedAt: now, UpdatedAt: now,
	}))
	require.NoError(t, drv.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "obj-survivor", Type: "note", RawContent: "keep me safe", TextContent: "keep me safe",
		Source: "manual", Status: storage.ObjectStatusActive,
		Metadata:  map[string]any{"enrichment.structured_metadata": map[string]any{"topics": []string{"finance"}}},
		Summaries: []string{"surviving summary"},
		CreatedAt: now, UpdatedAt: now,
	}))
	_, err := svc.PageUpsert(ctx, "shared-entity", "obj-survivor")
	require.NoError(t, err)
	_, err = svc.PageUpsert(ctx, "shared-entity", "obj-root")
	require.NoError(t, err)
	_, err = svc.PageUpsert(ctx, "exclusive-entity", "obj-root")
	require.NoError(t, err)
	_, err = svc.BuildTopicIndex(ctx)
	require.NoError(t, err)

	res, err := svc.EraseBySourceKey(ctx, EraseBySourceOptions{SourceKey: srcKey, Tombstone: true})
	require.NoError(t, err)
	require.Equal(t, 1, res.ScrubbedPages)
	require.Equal(t, 1, res.DeletedPages)
	require.True(t, res.ScrubbedTopicIndex)

	sharedPage, err := svc.GetEntityPage(ctx, "shared-entity")
	require.NoError(t, err)
	require.NotNil(t, sharedPage)
	require.NotContains(t, sharedPage.RawContent, sentinel)
	meta, _ := ExtractPageMeta(sharedPage)
	for _, rev := range meta.RevisionLog {
		require.NotEqual(t, "obj-root", rev.SourceID)
		require.NotContains(t, rev.Delta, sentinel)
	}

	excl, err := svc.GetEntityPage(ctx, "exclusive-entity")
	require.NoError(t, err)
	require.Nil(t, excl)

	idx, err := svc.GetTopicIndex(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"obj-survivor"}, idx.ObjectIDs)
}

func TestEraseBySourceKey_HoldRefusesAllOrNothing(t *testing.T) {
	ctx := sharing.WithViewer(context.Background(), sharing.Unrestricted)
	svc, drv, _ := newEraseTestService(t)
	now := time.Now().UTC().Truncate(time.Second)

	const srcKey = "bos://acct/records/held-1"
	require.NoError(t, drv.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "obj-h-root", Type: "note", RawContent: "root", SourceKey: srcKey,
		Status: storage.ObjectStatusActive, CreatedAt: now, UpdatedAt: now,
	}))
	require.NoError(t, drv.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "obj-h-derived", Type: "summary", RawContent: "derived",
		Status: storage.ObjectStatusActive, CreatedAt: now, UpdatedAt: now,
	}))
	require.NoError(t, drv.Edges().Create(ctx, &storage.Edge{
		ID: "eh1", FromType: "object", FromID: "obj-h-derived", ToType: "object", ToID: "obj-h-root", EdgeType: "derives_from", CreatedAt: now,
	}))

	require.NoError(t, drv.Holds().Place(ctx, &storage.ObjectHold{
		ObjectID: "obj-h-derived", HoldID: "hold-legal-1", Source: "bos", Reason: "litigation", PlacedBy: "bos", PlacedAt: now,
	}))
	_, err := svc.EraseBySourceKey(ctx, EraseBySourceOptions{SourceKey: srcKey, Tombstone: true})
	require.ErrorIs(t, err, storage.ErrObjectHeld)
	_, err = drv.Objects().Get(ctx, "obj-h-root")
	require.NoError(t, err)
	_, err = drv.Objects().Get(ctx, "obj-h-derived")
	require.NoError(t, err)
	exists, _ := drv.Tombstones().Exists(ctx, srcKey)
	require.False(t, exists)

	_, err = drv.Holds().Release(ctx, "obj-h-derived", "hold-legal-1", "bos", "released", now)
	require.NoError(t, err)
	_, err = svc.EraseBySourceKey(ctx, EraseBySourceOptions{SourceKey: srcKey, Tombstone: true})
	require.NoError(t, err)

	// Direct blob.DeleteObject refuses held object without deleting edges.
	require.NoError(t, drv.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "obj-h-direct", Type: "note", RawContent: "direct",
		Status: storage.ObjectStatusActive, CreatedAt: now, UpdatedAt: now,
	}))
	require.NoError(t, drv.Edges().Create(ctx, &storage.Edge{
		ID: "eh2", FromType: "object", FromID: "obj-h-direct", ToType: "entity", ToID: "ent-1", EdgeType: "mentions", CreatedAt: now,
	}))
	require.NoError(t, drv.Holds().Place(ctx, &storage.ObjectHold{
		ObjectID: "obj-h-direct", HoldID: "hold-direct", Source: "operator", Reason: "audit", PlacedBy: "op", PlacedAt: now,
	}))
	_, err = blob.DeleteObject(ctx, drv, "obj-h-direct")
	require.ErrorIs(t, err, storage.ErrObjectHeld)
	edgesAfter, err := drv.Edges().ListFrom(ctx, "object", "obj-h-direct")
	require.NoError(t, err)
	require.Len(t, edgesAfter, 1)
}

type staticAdapter struct{ objs []ingest.Object }

func (staticAdapter) Name() string                                     { return "test" }
func (a staticAdapter) Fetch(context.Context) ([]ingest.Object, error) { return a.objs, nil }

package service

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/stretchr/testify/require"
)

// blobLostOnWriteDriver deletes the blobs an object refers to as its
// row lands (Create or Update): a delete or sweep of the same key that
// counted no references before the row was written, deleting the blob
// the write put.
type blobLostOnWriteDriver struct {
	storage.StorageDriver
}

func (d blobLostOnWriteDriver) Objects() storage.ObjectStore {
	return blobLostOnWriteObjects{ObjectStore: d.StorageDriver.Objects(), blobs: d.Blobs()}
}

type blobLostOnWriteObjects struct {
	storage.ObjectStore
	blobs storage.BlobStore
}

func (o blobLostOnWriteObjects) lose(ctx context.Context, obj *storage.KnowledgeObject) error {
	for _, key := range blob.Keys(obj) {
		if err := o.blobs.Delete(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

func (o blobLostOnWriteObjects) Create(ctx context.Context, obj *storage.KnowledgeObject) error {
	if err := o.ObjectStore.Create(ctx, obj); err != nil {
		return err
	}
	return o.lose(ctx, obj)
}

func (o blobLostOnWriteObjects) Update(ctx context.Context, obj *storage.KnowledgeObject) error {
	if err := o.ObjectStore.Update(ctx, obj); err != nil {
		return err
	}
	return o.lose(ctx, obj)
}

// requireContent fails unless object id's content resolves to want.
func requireContent(t *testing.T, svc *Service, id, want string) {
	t.Helper()
	ctx := ownerCtx()
	obj, err := svc.store.Objects().Get(ctx, id)
	require.NoError(t, err)
	require.True(t, blob.IsBlobRef(obj.RawContent), "content not externalized")
	got, err := blob.Content(ctx, svc.store.Blobs(), obj.RawContent)
	require.NoError(t, err, "the written row names a deleted blob")
	require.Equal(t, want, got)
}

// A write whose blob a concurrent delete removes before its row lands
// puts the blob back once the row is written: federation receive and
// object updates (re-analysis goes through the same blob.UpdateObject).
func TestBlobDeletedBeforeRowLandsIsPutBack(t *testing.T) {
	ctx := ownerCtx()

	t.Run("federation accept", func(t *testing.T) {
		svc := blobTestService(t)
		svc.store = blobLostOnWriteDriver{StorageDriver: svc.store}
		body := bigBody(4 * testBlobThreshold)
		now := time.Now().UTC().Truncate(time.Second)
		n, err := svc.FederationAccept(ctx, []storage.KnowledgeObject{{
			ID: "fed-race", Type: "text", RawContent: body, ContentHash: "hash-fed-race",
			Source: "test://fed-race", CreatedAt: now, UpdatedAt: now,
		}}, nil, nil)
		require.NoError(t, err)
		require.Equal(t, 1, n)
		requireContent(t, svc, "fed-race", body)
	})

	t.Run("update", func(t *testing.T) {
		svc := blobTestService(t)
		seedBlobObject(t, svc, "upd-race", bigBody(2*testBlobThreshold))
		svc.store = blobLostOnWriteDriver{StorageDriver: svc.store}
		obj, err := svc.store.Objects().Get(ctx, "upd-race")
		require.NoError(t, err)
		body := "replaced " + bigBody(3*testBlobThreshold)
		obj.RawContent, obj.TextContent = body, ""
		require.NoError(t, svc.UpdateObject(ctx, obj))
		requireContent(t, svc, "upd-race", body)
	})
}

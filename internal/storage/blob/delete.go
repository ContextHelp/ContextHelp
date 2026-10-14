package blob

import (
	"context"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Orphan is a blob DeleteObject left behind: its object is gone and no
// other object refers to it, but it could not be deleted.
type Orphan struct {
	Key string
	Err error
}

func (o Orphan) String() string { return fmt.Sprintf("%s: %v", o.Key, o.Err) }

// DeleteObject hard-deletes object id: its edges, its row, then each
// blob its content is externalized to that no other object still refers
// to (keys are content hashes, so objects can share one) and that is
// older than DefaultSweepGrace. It is the one hard-delete path: DELETE
// /objects/{id} and prune both go through it.
//
// Edges go first: no foreign key cascades them, and an edge left naming
// a deleted object is never cleaned up. The row goes before its blobs. A
// failure after it leaves at worst an orphan blob, returned in orphans
// with the object deleted and err nil, never a row whose ref names a
// deleted blob. err reports that the object could not be read, or its
// edges or row deleted; its row and blobs are intact then.
//
// A younger blob is left for Sweep: it may be one an ingest of the same
// content has just put and whose row is not written yet (see
// deleteUnreferenced).
func DeleteObject(ctx context.Context, drv storage.StorageDriver, id string) (orphans []Orphan, err error) {
	obj, err := drv.Objects().Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("delete object %s: %w", id, err)
	}
	keys := Keys(obj)
	if err := drv.Edges().DeleteByObject(ctx, id); err != nil {
		return nil, fmt.Errorf("delete object %s: %w", id, err)
	}
	if err := drv.Objects().Delete(ctx, id); err != nil {
		return nil, err
	}
	for _, key := range keys {
		if err := deleteUnreferenced(ctx, drv, key); err != nil {
			orphans = append(orphans, Orphan{Key: key, Err: err})
		}
	}
	return orphans, nil
}

// deleteUnreferenced deletes blob key when no object refers to it and
// it was last written more than DefaultSweepGrace ago.
//
// Ingest puts a blob before it writes the row that refers to it, and an
// ingest of content another object holds puts that blob again. An
// ingest landing between the reference count and the delete would
// otherwise lose its blob: its row would name a deleted key. The age is
// read after the count, so such an ingest has refreshed it by then, and
// the blob is left to Sweep, which counts references again. A blob of
// unknown age is left too. Only an ingest putting the blob between the
// age read and the delete, two consecutive store calls, still races.
func deleteUnreferenced(ctx context.Context, drv storage.StorageDriver, key string) error {
	n, err := drv.Objects().CountBlobRefs(ctx, key)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	store := drv.Blobs()
	if store == nil {
		return ErrNoStore
	}
	old, err := olderThanGrace(ctx, store, key)
	if err != nil || !old {
		return err
	}
	return store.Delete(ctx, key)
}

// olderThanGrace reports whether blob key exists and was last written
// more than DefaultSweepGrace ago, read from the store's listing of key
// (the age Sweep reads).
func olderThanGrace(ctx context.Context, store storage.BlobStore, key string) (bool, error) {
	items, err := store.List(ctx, key)
	if err != nil {
		return false, fmt.Errorf("blob %s: read age: %w", key, err)
	}
	for _, item := range items {
		if item.Key == key {
			return !item.UpdatedAt.IsZero() && time.Since(item.UpdatedAt) >= DefaultSweepGrace, nil
		}
	}
	return false, nil
}

// Keys returns the blob keys obj refers to, each once: its content refs
// and metadata.blob_key.
func Keys(obj *storage.KnowledgeObject) []string {
	var keys []string
	add := func(key string) {
		if key == "" {
			return
		}
		for _, k := range keys {
			if k == key {
				return
			}
		}
		keys = append(keys, key)
	}
	for _, content := range []string{obj.RawContent, obj.TextContent} {
		if key, ok := BlobKey(content); ok {
			add(key)
		}
	}
	if key, ok := obj.Metadata["blob_key"].(string); ok {
		add(key)
	}
	return keys
}

// PlanDelete returns the blob keys DeleteObject would delete were every
// object in objs deleted: each key they refer to that no object outside
// objs refers to and that is older than DefaultSweepGrace, in the order
// objs first name them. It reads only.
func PlanDelete(ctx context.Context, drv storage.StorageDriver, objs []*storage.KnowledgeObject) ([]string, error) {
	var order []string
	refs := map[string]int{} // key -> objects in objs referring to it
	for _, obj := range objs {
		for _, key := range Keys(obj) {
			if refs[key] == 0 {
				order = append(order, key)
			}
			refs[key]++
		}
	}
	store := drv.Blobs()
	if store == nil {
		return nil, nil // nothing to delete; DeleteObject reports ErrNoStore
	}
	var keys []string
	for _, key := range order {
		n, err := drv.Objects().CountBlobRefs(ctx, key)
		if err != nil {
			return nil, err
		}
		if n > refs[key] {
			continue
		}
		old, err := olderThanGrace(ctx, store, key)
		if err != nil {
			return nil, err
		}
		if old {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

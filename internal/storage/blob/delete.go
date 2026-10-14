package blob

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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
// a deleted object is never cleaned up. Aliases and embeddings need no
// step here: their foreign keys cascade from the row's delete on both
// drivers. The row goes before its blobs. A
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
// ingest of content another object holds puts that blob again. The age
// is read after the reference count, so an ingest landing between the
// two has made the blob young by then, and it is left to Sweep, which
// counts references again. A blob of unknown age is left too. An ingest
// putting the blob between the age read and the delete is caught by
// remove.
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
	_, err = remove(ctx, drv.Objects(), store, key)
	return err
}

// remove deletes blob key, which no object referred to when its
// references were last counted, and puts it back when one refers to it
// once it is gone. removed reports that the blob is deleted.
//
// An ingest of the same content can land at any point of a delete: it
// puts the blob, writes its row, then checks the blob is still there and
// puts it back if not (Ensure). Whichever of the two acts last sees the
// other: an ingest checking after the delete puts the blob back itself;
// a delete landing after the ingest's check finds the ingest's row on
// its second count, and puts back the content it read before deleting.
// A blob that is already gone is not deleted.
//
// A conditional delete can't replace the second count: keys are content
// hashes, so a re-put of the same key carries the same S3 ETag
// (If-Match), and a delete conditional on the modification time is
// directory-bucket only.
func remove(ctx context.Context, objects storage.ObjectStore, store storage.BlobStore, key string) (removed bool, err error) {
	rc, meta, err := store.Get(ctx, key)
	if errors.Is(err, storage.ErrBlobNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("blob %s: read before delete: %w", key, err)
	}
	data, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		return false, fmt.Errorf("blob %s: read before delete: %w", key, err)
	}
	if err := store.Delete(ctx, key); err != nil {
		return false, err
	}
	n, err := objects.CountBlobRefs(ctx, key)
	if err == nil && n == 0 {
		return true, nil
	}
	// Referred to again, or unknown: put it back.
	if perr := store.Put(ctx, key, bytes.NewReader(data), meta); perr != nil {
		return false, errors.Join(fmt.Errorf("blob %s: deleted while an object refers to it; put back: %w", key, perr), err)
	}
	if err != nil {
		return false, fmt.Errorf("blob %s: count references after delete (blob put back): %w", key, err)
	}
	return false, nil
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

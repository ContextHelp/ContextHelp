package blob

import (
	"context"
	"fmt"

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
// to (keys are content hashes, so objects can share one). It is the one
// hard-delete path: DELETE /objects/{id} and prune both go through it.
//
// Edges go first: no foreign key cascades them, and an edge left naming
// a deleted object is never cleaned up. The row goes before its blobs. A
// failure after it leaves at worst an orphan blob, returned in orphans
// with the object deleted and err nil, never a row whose ref names a
// deleted blob. err reports that the object could not be read, or its
// edges or row deleted; its row and blobs are intact then.
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

// deleteUnreferenced deletes blob key unless an object refers to it.
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
	return store.Delete(ctx, key)
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

package blob

import (
	"context"
	"fmt"
	"slices"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// UpdateObject writes obj over its stored row, then deletes each blob
// the row referred to that obj does not and no other object does: the
// content an update replaced (keys are content hashes, so new content
// gets a new key).
//
// Same order as DeleteObject: the row goes first, so a failure after it
// leaves at worst an orphan blob, returned in orphans with the row
// updated and err nil. err reports that the object could not be read or
// updated; nothing changed then.
func UpdateObject(ctx context.Context, drv storage.StorageDriver, obj *storage.KnowledgeObject) (orphans []Orphan, err error) {
	old, err := drv.Objects().Get(ctx, obj.ID)
	if err != nil {
		return nil, fmt.Errorf("update object %s: %w", obj.ID, err)
	}
	if err := drv.Objects().Update(ctx, obj); err != nil {
		return nil, err
	}
	kept := Keys(obj)
	for _, key := range Keys(old) {
		if slices.Contains(kept, key) {
			continue
		}
		if err := deleteUnreferenced(ctx, drv, key); err != nil {
			orphans = append(orphans, Orphan{Key: key, Err: err})
		}
	}
	return orphans, nil
}

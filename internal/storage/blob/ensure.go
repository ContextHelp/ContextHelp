package blob

import (
	"context"
	"fmt"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Ensure puts back each blob obj refers to that store no longer holds,
// from the content obj.BlobContent keeps for it (the externalize step
// sets it). Ingest calls it once obj's row is written.
//
// Ingest puts a blob before it writes the row naming it, so until the
// row lands no object refers to the blob, and a delete or sweep freeing
// the same key (keys are content hashes) may delete it. Once the row is
// written, a delete that has not deleted the blob yet finds the row and
// keeps or puts back the blob (remove); one that has is caught here.
// Keys obj.BlobContent has no content for are not checked.
func Ensure(ctx context.Context, store storage.BlobStore, obj *storage.KnowledgeObject) error {
	if len(obj.BlobContent) == 0 {
		return nil
	}
	if store == nil {
		return ErrNoStore
	}
	for _, key := range Keys(obj) {
		content, ok := obj.BlobContent[key]
		if !ok {
			continue
		}
		have, err := store.Exists(ctx, key)
		if err != nil {
			return fmt.Errorf("blob %s: check after write: %w", key, err)
		}
		if have {
			continue
		}
		contentType, _ := obj.Metadata["blob_content_type"].(string)
		if err := store.Put(ctx, key, strings.NewReader(content), storage.BlobMeta{
			ContentType: contentType,
			Size:        int64(len(content)),
			ContentHash: key,
		}); err != nil {
			return fmt.Errorf("blob %s: put back after write: %w", key, err)
		}
	}
	return nil
}

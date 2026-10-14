package sqlite

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage/blobref"
)

// CountBlobRefs counts the objects that refer to blob key: raw_content
// or text_content is its ref, or metadata.blob_key names it.
func (s *ObjectStore) CountBlobRefs(ctx context.Context, key string) (int, error) {
	ref := blobref.For(key)
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM objects
		WHERE raw_content = ? OR text_content = ? OR json_extract(metadata, '$.blob_key') = ?`,
		ref, ref, key).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count blob refs %s: %w", key, err)
	}
	return n, nil
}

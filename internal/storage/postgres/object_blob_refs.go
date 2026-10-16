package postgres

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage/blobref"
)

// CountBlobRefs counts the objects that refer to blob key: raw_content
// or text_content is its ref, or metadata.blob_key names it. A scoped
// store counts the visible ones, which is how a restricted viewer's
// blob reads learn whether a key is theirs to read.
func (s *ObjectStore) CountBlobRefs(ctx context.Context, key string) (int, error) {
	ref := blobref.For(key)
	var n int
	idx := 3
	clause, sargs := s.andScope(&idx)
	// #nosec G202 -- clause is compiled SQL whose values are bound in sargs.
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM objects
		WHERE (raw_content = $1 OR text_content = $1 OR metadata->>'blob_key' = $2)`+clause,
		append([]any{ref, key}, sargs...)...).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count blob refs %s: %w", key, err)
	}
	return n, nil
}

package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blobref"
	"github.com/ideacrafterslabs/ctxt/internal/storage/rowscan"
)

var _ storage.RefTextStore = (*ObjectStore)(nil)

// ListRefText implements storage.RefTextStore.
func (s *ObjectStore) ListRefText(ctx context.Context, after string, limit int) ([]*storage.KnowledgeObject, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, raw_content, text_content, metadata FROM objects
		  WHERE id > $1 AND text_content LIKE $2::text || '_%' ORDER BY id LIMIT $3`,
		after, blobref.Prefix, limit)
	if err != nil {
		return nil, fmt.Errorf("list ref text: %w", err)
	}
	defer rows.Close()
	return rowscan.All(rows, func(rows *sql.Rows) (*storage.KnowledgeObject, error) {
		var obj storage.KnowledgeObject
		var meta []byte
		if err := rows.Scan(&obj.ID, &obj.RawContent, &obj.TextContent, &meta); err != nil {
			return nil, fmt.Errorf("list ref text: %w", err)
		}
		if err := decodeJSONColumn(meta, "metadata", &obj.Metadata); err != nil {
			return nil, fmt.Errorf("list ref text %s: %w", obj.ID, err)
		}
		return &obj, nil
	})
}

// ReplaceRefText implements storage.RefTextStore. An empty
// projection_version is stale under every projection version.
func (s *ObjectStore) ReplaceRefText(ctx context.Context, id, ref, text string, truncated bool) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE objects SET
		text_content = $1,
		metadata = CASE WHEN $2::boolean THEN COALESCE(metadata, '{}'::jsonb) || jsonb_build_object($3::text, true)
		                ELSE metadata END,
		projection_version = ''
	  WHERE id = $4 AND text_content = $5`,
		text, truncated, blobref.MetaTextTruncated, id, ref)
	if err != nil {
		return false, fmt.Errorf("replace ref text %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("replace ref text %s: %w", id, err)
	}
	return n > 0, nil
}

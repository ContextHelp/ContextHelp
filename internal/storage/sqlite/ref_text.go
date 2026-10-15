package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blobref"
	"github.com/ideacrafterslabs/ctxt/internal/storage/rowscan"
)

var _ storage.RefTextStore = (*ObjectStore)(nil)

// ListRefText implements storage.RefTextStore. GLOB, unlike LIKE, is
// case-sensitive, as blobref.Is is.
func (s *ObjectStore) ListRefText(ctx context.Context, after string, limit int) ([]*storage.KnowledgeObject, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, raw_content, text_content, metadata FROM objects
		  WHERE id > ? AND text_content GLOB ? || '?*' ORDER BY id LIMIT ?`,
		after, blobref.Prefix, limit)
	if err != nil {
		return nil, fmt.Errorf("list ref text: %w", err)
	}
	defer rows.Close()
	return rowscan.All(rows, func(rows *sql.Rows) (*storage.KnowledgeObject, error) {
		var obj storage.KnowledgeObject
		var meta sql.NullString
		if err := rows.Scan(&obj.ID, &obj.RawContent, &obj.TextContent, &meta); err != nil {
			return nil, fmt.Errorf("list ref text: %w", err)
		}
		if err := decodeJSONColumn(meta.String, "metadata", &obj.Metadata); err != nil {
			return nil, fmt.Errorf("list ref text %s: %w", obj.ID, err)
		}
		return &obj, nil
	})
}

// ReplaceRefText implements storage.RefTextStore. An empty
// projection_version is stale under every projection version.
func (s *ObjectStore) ReplaceRefText(ctx context.Context, id, ref, text string, truncated bool) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE objects SET
		text_content = ?,
		metadata = CASE WHEN ? THEN json_set(COALESCE(NULLIF(metadata, ''), '{}'), '$.' || ?, json('true'))
		                ELSE metadata END,
		projection_version = ''
	  WHERE id = ? AND text_content = ?`,
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

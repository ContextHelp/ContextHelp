package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// AttachmentStore implements storage.AttachmentStore for SQLite.
type AttachmentStore struct{ db *sql.DB }

// SaveAttachment stores binary data linked to objectID and returns the new attachment ID.
func (s *AttachmentStore) SaveAttachment(
	ctx context.Context,
	objectID, filename, mimeType string,
	data []byte,
) (string, error) {
	id := "att_" + uuid.New().String()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO attachments (id, object_id, filename, mime_type, size_bytes, data, created_at)
         VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, objectID, filename, mimeType, int64(len(data)), data, now,
	)
	if err != nil {
		return "", fmt.Errorf("attachment save: %w", err)
	}
	return id, nil
}

// GetAttachment retrieves an attachment by ID, including binary data.
func (s *AttachmentStore) GetAttachment(ctx context.Context, attachmentID string) (*storage.Attachment, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, object_id, filename, mime_type, size_bytes, data, created_at
         FROM attachments WHERE id = ?`,
		attachmentID,
	)
	a, err := scanAttachment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("attachment %q not found", attachmentID)
	}
	return a, err
}

// ListAttachments returns metadata (no data blob) for all attachments of objectID.
func (s *AttachmentStore) ListAttachments(ctx context.Context, objectID string) ([]storage.Attachment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, object_id, filename, mime_type, size_bytes, created_at
         FROM attachments WHERE object_id = ? ORDER BY created_at ASC`,
		objectID,
	)
	if err != nil {
		return nil, fmt.Errorf("attachment list: %w", err)
	}
	defer rows.Close()

	var out []storage.Attachment
	for rows.Next() {
		var a storage.Attachment
		var createdStr string
		if err := rows.Scan(&a.ID, &a.ObjectID, &a.Filename, &a.MimeType, &a.SizeBytes, &createdStr); err != nil {
			return nil, err
		}
		a.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		out = append(out, a)
	}
	return out, rows.Err()
}

// DeleteAttachment removes an attachment by ID.
func (s *AttachmentStore) DeleteAttachment(ctx context.Context, attachmentID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM attachments WHERE id = ?`, attachmentID)
	if err != nil {
		return fmt.Errorf("attachment delete: %w", err)
	}
	return nil
}

// scanAttachment reads a full attachment row (including data blob).
func scanAttachment(row *sql.Row) (*storage.Attachment, error) {
	var a storage.Attachment
	var createdStr string
	if err := row.Scan(&a.ID, &a.ObjectID, &a.Filename, &a.MimeType, &a.SizeBytes, &a.Data, &createdStr); err != nil {
		return nil, err
	}
	a.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	return &a, nil
}

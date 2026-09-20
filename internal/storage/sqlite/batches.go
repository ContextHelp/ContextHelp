package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type BatchStore struct {
	db *sql.DB
}

func (s *BatchStore) Create(ctx context.Context, batch *storage.Batch) error {
	now := time.Now().UTC()
	batch.CreatedAt = now
	batch.UpdatedAt = now
	if batch.Status == "" {
		batch.Status = "processing"
	}
	errorsJSON, _ := json.Marshal(batch.Errors)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO batches (id, format, total_records, completed, failed, status, errors, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		batch.ID, batch.Format, batch.TotalRecords, batch.Completed, batch.Failed,
		batch.Status, string(errorsJSON),
		now.Format(time.RFC3339), now.Format(time.RFC3339),
	)
	return err
}

func (s *BatchStore) Get(ctx context.Context, id string) (*storage.Batch, error) {
	var b storage.Batch
	var errorsJSON string
	var createdAt, updatedAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, format, total_records, completed, failed, status, errors, created_at, updated_at
		 FROM batches WHERE id = ?`, id,
	).Scan(&b.ID, &b.Format, &b.TotalRecords, &b.Completed, &b.Failed,
		&b.Status, &errorsJSON, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	if err := decodeJSONColumn(errorsJSON, "errors", &b.Errors); err != nil {
		return nil, fmt.Errorf("get batch: %w", err)
	}
	b.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	b.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &b, nil
}

func (s *BatchStore) Update(ctx context.Context, batch *storage.Batch) error {
	batch.UpdatedAt = time.Now().UTC()
	errorsJSON, _ := json.Marshal(batch.Errors)
	_, err := s.db.ExecContext(ctx,
		`UPDATE batches SET total_records=?, completed=?, failed=?, status=?, errors=?, updated_at=?
		 WHERE id=?`,
		batch.TotalRecords, batch.Completed, batch.Failed, batch.Status,
		string(errorsJSON), batch.UpdatedAt.Format(time.RFC3339), batch.ID,
	)
	return err
}

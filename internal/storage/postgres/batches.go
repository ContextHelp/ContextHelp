package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func (s *BatchStore) Create(ctx context.Context, batch *storage.Batch) error {
	now := time.Now().UTC()
	batch.CreatedAt = now
	batch.UpdatedAt = now
	if batch.Status == "" {
		batch.Status = "processing"
	}
	errorsJSON, _ := json.Marshal(batch.Errors)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO batches (id, type, status, total, processed, failed, config, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		batch.ID, batch.Format, batch.Status,
		batch.TotalRecords, batch.Completed, batch.Failed,
		errorsJSON, now, now,
	)
	return err
}

func (s *BatchStore) Get(ctx context.Context, id string) (*storage.Batch, error) {
	var b storage.Batch
	var errorsJSON []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT id, type, status, total, processed, failed, errors, created_at, updated_at
		 FROM batches WHERE id = $1`, id,
	).Scan(&b.ID, &b.Format, &b.Status, &b.TotalRecords, &b.Completed, &b.Failed,
		&errorsJSON, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get batch: %w", err)
	}
	json.Unmarshal(errorsJSON, &b.Errors)
	return &b, nil
}

func (s *BatchStore) Update(ctx context.Context, batch *storage.Batch) error {
	batch.UpdatedAt = time.Now().UTC()
	errorsJSON, _ := json.Marshal(batch.Errors)
	_, err := s.db.ExecContext(ctx,
		`UPDATE batches SET total=$1, processed=$2, failed=$3, status=$4, errors=$5, updated_at=$6
		 WHERE id=$7`,
		batch.TotalRecords, batch.Completed, batch.Failed, batch.Status,
		errorsJSON, batch.UpdatedAt, batch.ID,
	)
	return err
}

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// watermarkStore persists per-federation change positions in the
// federation_watermarks table (migrations 025 and 042): last_synced_at
// holds the position's updated_at, last_object_id its id. One row per
// federation_name.
type watermarkStore struct{ db *sql.DB }

// GetWatermark returns the position stored for federationName, or the
// zero position when there is none. An empty last_synced_at (the column
// default) reads as the zero time.
func (s *watermarkStore) GetWatermark(ctx context.Context, federationName string) (storage.ChangeCursor, error) {
	var stored, id string
	err := s.db.QueryRowContext(ctx,
		`SELECT last_synced_at, last_object_id FROM federation_watermarks WHERE federation_name = ?`,
		federationName,
	).Scan(&stored, &id)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.ChangeCursor{}, nil
	}
	if err != nil {
		return storage.ChangeCursor{}, fmt.Errorf("watermark get %q: %w", federationName, err)
	}
	pos := storage.ChangeCursor{ID: id}
	if stored == "" {
		return pos, nil
	}
	ts, err := time.Parse(time.RFC3339Nano, stored)
	if err != nil {
		return storage.ChangeCursor{}, fmt.Errorf("watermark get %q: parse %q: %w", federationName, stored, err)
	}
	pos.UpdatedAt = ts.UTC()
	return pos, nil
}

// SetWatermark upserts pos for federationName; its time is stored as
// RFC3339Nano UTC.
func (s *watermarkStore) SetWatermark(ctx context.Context, federationName string, pos storage.ChangeCursor) error {
	encoded := pos.UpdatedAt.UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO federation_watermarks (federation_name, last_synced_at, last_object_id)
         VALUES (?, ?, ?)
         ON CONFLICT(federation_name) DO UPDATE SET
             last_synced_at = excluded.last_synced_at,
             last_object_id = excluded.last_object_id`,
		federationName, encoded, pos.ID,
	)
	if err != nil {
		return fmt.Errorf("watermark set %q: %w", federationName, err)
	}
	return nil
}

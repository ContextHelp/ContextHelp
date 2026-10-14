package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// watermarkStore persists per-federation change positions in the
// federation_watermarks table: last_synced_at holds the position's
// updated_at, last_object_id its id. One row per federation_name.
type watermarkStore struct{ db *sql.DB }

// GetWatermark returns the position stored for federationName, or the
// zero position when there is none.
func (s *watermarkStore) GetWatermark(ctx context.Context, federationName string) (storage.ChangeCursor, error) {
	var pos storage.ChangeCursor
	err := s.db.QueryRowContext(ctx,
		`SELECT last_synced_at, last_object_id FROM federation_watermarks WHERE federation_name = $1`,
		federationName,
	).Scan(&pos.UpdatedAt, &pos.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.ChangeCursor{}, nil
	}
	if err != nil {
		return storage.ChangeCursor{}, fmt.Errorf("watermark get %q: %w", federationName, err)
	}
	pos.UpdatedAt = pos.UpdatedAt.UTC()
	return pos, nil
}

// SetWatermark upserts pos for federationName.
func (s *watermarkStore) SetWatermark(ctx context.Context, federationName string, pos storage.ChangeCursor) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO federation_watermarks (federation_name, last_synced_at, last_object_id)
         VALUES ($1, $2, $3)
         ON CONFLICT (federation_name)
         DO UPDATE SET last_synced_at = EXCLUDED.last_synced_at,
                       last_object_id = EXCLUDED.last_object_id`,
		federationName, pos.UpdatedAt.UTC(), pos.ID,
	)
	if err != nil {
		return fmt.Errorf("watermark set %q: %w", federationName, err)
	}
	return nil
}

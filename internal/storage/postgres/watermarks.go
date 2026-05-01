package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// watermarkStore persists per-federation last-synced timestamps in the
// federation_watermarks table. One row per federation_name; missing rows
// are treated as the Unix epoch.
type watermarkStore struct{ db *sql.DB }

// GetWatermark returns last_synced_at for federationName or epoch when missing.
func (s *watermarkStore) GetWatermark(ctx context.Context, federationName string) (time.Time, error) {
	var ts time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT last_synced_at FROM federation_watermarks WHERE federation_name = $1`,
		federationName,
	).Scan(&ts)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Unix(0, 0).UTC(), nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("watermark get %q: %w", federationName, err)
	}
	return ts.UTC(), nil
}

// SetWatermark upserts last_synced_at for federationName.
func (s *watermarkStore) SetWatermark(ctx context.Context, federationName string, ts time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO federation_watermarks (federation_name, last_synced_at)
         VALUES ($1, $2)
         ON CONFLICT (federation_name)
         DO UPDATE SET last_synced_at = EXCLUDED.last_synced_at`,
		federationName, ts.UTC(),
	)
	if err != nil {
		return fmt.Errorf("watermark set %q: %w", federationName, err)
	}
	return nil
}

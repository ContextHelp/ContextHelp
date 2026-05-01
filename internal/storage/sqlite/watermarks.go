package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// watermarkStore persists per-federation last-synced timestamps in the
// federation_watermarks table (created by migration 025). One row per
// federation_name; missing rows are treated as the Unix epoch.
type watermarkStore struct{ db *sql.DB }

// GetWatermark returns last_synced_at for federationName or epoch when missing.
// Empty stored values (legacy default '') are also normalised to epoch.
func (s *watermarkStore) GetWatermark(ctx context.Context, federationName string) (time.Time, error) {
	var stored string
	err := s.db.QueryRowContext(ctx,
		`SELECT last_synced_at FROM federation_watermarks WHERE federation_name = ?`,
		federationName,
	).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Unix(0, 0).UTC(), nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("watermark get %q: %w", federationName, err)
	}
	if stored == "" {
		return time.Unix(0, 0).UTC(), nil
	}
	ts, err := time.Parse(time.RFC3339Nano, stored)
	if err != nil {
		return time.Time{}, fmt.Errorf("watermark get %q: parse %q: %w", federationName, stored, err)
	}
	return ts.UTC(), nil
}

// SetWatermark upserts last_synced_at for federationName as RFC3339Nano UTC.
func (s *watermarkStore) SetWatermark(ctx context.Context, federationName string, ts time.Time) error {
	encoded := ts.UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO federation_watermarks (federation_name, last_synced_at)
         VALUES (?, ?)
         ON CONFLICT(federation_name) DO UPDATE SET last_synced_at = excluded.last_synced_at`,
		federationName, encoded,
	)
	if err != nil {
		return fmt.Errorf("watermark set %q: %w", federationName, err)
	}
	return nil
}

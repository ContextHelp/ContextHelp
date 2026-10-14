package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// watermarkStore persists per-federation watermarks in the
// federation_watermarks table (migrations 025 and 043): last_change_seq
// is the ChangeSeq of the last object the target acknowledged. One row
// per federation_name.
type watermarkStore struct{ db *sql.DB }

// GetWatermark returns the ChangeSeq stored for federationName, or 0
// when there is none.
func (s *watermarkStore) GetWatermark(ctx context.Context, federationName string) (int64, error) {
	var seq int64
	err := s.db.QueryRowContext(ctx,
		`SELECT last_change_seq FROM federation_watermarks WHERE federation_name = ?`,
		federationName,
	).Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("watermark get %q: %w", federationName, err)
	}
	return seq, nil
}

// SetWatermark upserts seq for federationName.
func (s *watermarkStore) SetWatermark(ctx context.Context, federationName string, seq int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO federation_watermarks (federation_name, last_change_seq)
         VALUES (?, ?)
         ON CONFLICT(federation_name) DO UPDATE SET last_change_seq = excluded.last_change_seq`,
		federationName, seq,
	)
	if err != nil {
		return fmt.Errorf("watermark set %q: %w", federationName, err)
	}
	return nil
}

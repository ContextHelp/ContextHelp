package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func (s *FeedItemStore) Create(ctx context.Context, item *storage.FeedItem) error {
	if item.IngestedAt.IsZero() {
		item.IngestedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO feed_items (id, feed_id, guid, link, object_id, ingested_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		item.ID, item.FeedID, item.GUID, item.Link, item.ObjectID, item.IngestedAt.UTC(),
	)
	return err
}

func (s *FeedItemStore) ExistsByGUID(ctx context.Context, feedID, guid string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM feed_items WHERE feed_id = $1 AND guid = $2`,
		feedID, guid,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("exists by guid: %w", err)
	}
	return count > 0, nil
}

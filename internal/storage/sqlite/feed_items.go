package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type FeedItemStore struct {
	db *sql.DB
}

func (s *FeedItemStore) Create(ctx context.Context, item *storage.FeedItem) error {
	if item.IngestedAt.IsZero() {
		item.IngestedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO feed_items (id, feed_id, guid, link, object_id, ingested_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		item.ID, item.FeedID, item.GUID, item.Link, item.ObjectID,
		item.IngestedAt.Format(time.RFC3339),
	)
	return err
}

func (s *FeedItemStore) ExistsByGUID(ctx context.Context, feedID, guid string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM feed_items WHERE feed_id = ? AND guid = ?`,
		feedID, guid,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

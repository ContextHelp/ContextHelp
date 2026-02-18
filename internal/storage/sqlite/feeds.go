package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type FeedStore struct {
	db *sql.DB
}

func (s *FeedStore) Create(ctx context.Context, feed *storage.Feed) error {
	now := time.Now().UTC()
	feed.CreatedAt = now
	feed.UpdatedAt = now
	if feed.Status == "" {
		feed.Status = "active"
	}
	if feed.SyncInterval == "" {
		feed.SyncInterval = "1h"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO feeds (id, url, title, description, site_url, format, status, sync_interval, etag, last_modified, last_sync, error_count, last_error, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		feed.ID, feed.URL, feed.Title, feed.Description, feed.SiteURL, feed.Format,
		feed.Status, feed.SyncInterval, feed.ETag, feed.LastModified,
		timePtr(feed.LastSync), feed.ErrorCount, feed.LastError,
		now.Format(time.RFC3339), now.Format(time.RFC3339),
	)
	return err
}

func (s *FeedStore) Get(ctx context.Context, id string) (*storage.Feed, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, url, title, description, site_url, format, status, sync_interval, etag, last_modified, last_sync, error_count, last_error, created_at, updated_at
		 FROM feeds WHERE id = ?`, id)
	return scanFeed(row)
}

func (s *FeedStore) GetByURL(ctx context.Context, url string) (*storage.Feed, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, url, title, description, site_url, format, status, sync_interval, etag, last_modified, last_sync, error_count, last_error, created_at, updated_at
		 FROM feeds WHERE url = ?`, url)
	return scanFeed(row)
}

func (s *FeedStore) List(ctx context.Context, filter storage.FeedFilter) ([]*storage.Feed, error) {
	query := `SELECT id, url, title, description, site_url, format, status, sync_interval, etag, last_modified, last_sync, error_count, last_error, created_at, updated_at FROM feeds`
	var args []any
	if filter.Status != "" {
		query += ` WHERE status = ?`
		args = append(args, filter.Status)
	}
	query += ` ORDER BY created_at DESC`
	if filter.Limit > 0 {
		query += fmt.Sprintf(` LIMIT %d OFFSET %d`, filter.Limit, filter.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var feeds []*storage.Feed
	for rows.Next() {
		f, err := scanFeedRow(rows)
		if err != nil {
			return nil, err
		}
		feeds = append(feeds, f)
	}
	return feeds, rows.Err()
}

func (s *FeedStore) Update(ctx context.Context, feed *storage.Feed) error {
	feed.UpdatedAt = time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`UPDATE feeds SET title=?, description=?, site_url=?, format=?, status=?, sync_interval=?, etag=?, last_modified=?, last_sync=?, error_count=?, last_error=?, updated_at=?
		 WHERE id=?`,
		feed.Title, feed.Description, feed.SiteURL, feed.Format, feed.Status,
		feed.SyncInterval, feed.ETag, feed.LastModified,
		timePtr(feed.LastSync), feed.ErrorCount, feed.LastError,
		feed.UpdatedAt.Format(time.RFC3339), feed.ID,
	)
	return err
}

func (s *FeedStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM feeds WHERE id = ?`, id)
	return err
}

func timePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format(time.RFC3339)
}

func scanFeed(row *sql.Row) (*storage.Feed, error) {
	var f storage.Feed
	var lastSync, createdAt, updatedAt sql.NullString
	err := row.Scan(&f.ID, &f.URL, &f.Title, &f.Description, &f.SiteURL,
		&f.Format, &f.Status, &f.SyncInterval, &f.ETag, &f.LastModified,
		&lastSync, &f.ErrorCount, &f.LastError, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	if lastSync.Valid {
		t, _ := time.Parse(time.RFC3339, lastSync.String)
		f.LastSync = &t
	}
	if createdAt.Valid {
		f.CreatedAt, _ = time.Parse(time.RFC3339, createdAt.String)
	}
	if updatedAt.Valid {
		f.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt.String)
	}
	return &f, nil
}

type feedScanner interface {
	Scan(dest ...any) error
}

func scanFeedRow(row feedScanner) (*storage.Feed, error) {
	var f storage.Feed
	var lastSync, createdAt, updatedAt sql.NullString
	err := row.Scan(&f.ID, &f.URL, &f.Title, &f.Description, &f.SiteURL,
		&f.Format, &f.Status, &f.SyncInterval, &f.ETag, &f.LastModified,
		&lastSync, &f.ErrorCount, &f.LastError, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	if lastSync.Valid {
		t, _ := time.Parse(time.RFC3339, lastSync.String)
		f.LastSync = &t
	}
	if createdAt.Valid {
		f.CreatedAt, _ = time.Parse(time.RFC3339, createdAt.String)
	}
	if updatedAt.Valid {
		f.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt.String)
	}
	return &f, nil
}

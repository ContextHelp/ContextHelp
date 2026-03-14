package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

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
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		feed.ID, feed.URL, feed.Title, feed.Description, feed.SiteURL, feed.Format,
		feed.Status, feed.SyncInterval, feed.ETag, feed.LastModified,
		pgTimePtr(feed.LastSync), feed.ErrorCount, feed.LastError,
		now, now,
	)
	return err
}

func (s *FeedStore) Get(ctx context.Context, id string) (*storage.Feed, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, url, title, description, site_url, format, status, sync_interval, etag, last_modified, last_sync, error_count, last_error, created_at, updated_at
		 FROM feeds WHERE id = $1`, id)
	return scanFeed(row)
}

func (s *FeedStore) GetByURL(ctx context.Context, url string) (*storage.Feed, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, url, title, description, site_url, format, status, sync_interval, etag, last_modified, last_sync, error_count, last_error, created_at, updated_at
		 FROM feeds WHERE url = $1`, url)
	return scanFeed(row)
}

func (s *FeedStore) List(ctx context.Context, filter storage.FeedFilter) ([]*storage.Feed, error) {
	query := `SELECT id, url, title, description, site_url, format, status, sync_interval, etag, last_modified, last_sync, error_count, last_error, created_at, updated_at FROM feeds`
	var args []any
	if filter.Status != "" {
		query += ` WHERE status = $1`
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
		`UPDATE feeds SET title=$1, description=$2, site_url=$3, format=$4, status=$5, sync_interval=$6, etag=$7, last_modified=$8, last_sync=$9, error_count=$10, last_error=$11, updated_at=$12
		 WHERE id=$13`,
		feed.Title, feed.Description, feed.SiteURL, feed.Format, feed.Status,
		feed.SyncInterval, feed.ETag, feed.LastModified,
		pgTimePtr(feed.LastSync), feed.ErrorCount, feed.LastError,
		feed.UpdatedAt, feed.ID,
	)
	return err
}

func (s *FeedStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM feeds WHERE id = $1`, id)
	return err
}

func pgTimePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}

func scanFeed(row *sql.Row) (*storage.Feed, error) {
	var f storage.Feed
	var lastSync sql.NullTime
	err := row.Scan(&f.ID, &f.URL, &f.Title, &f.Description, &f.SiteURL,
		&f.Format, &f.Status, &f.SyncInterval, &f.ETag, &f.LastModified,
		&lastSync, &f.ErrorCount, &f.LastError, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if lastSync.Valid {
		t := lastSync.Time
		f.LastSync = &t
	}
	return &f, nil
}

func scanFeedRow(rows *sql.Rows) (*storage.Feed, error) {
	var f storage.Feed
	var lastSync sql.NullTime
	err := rows.Scan(&f.ID, &f.URL, &f.Title, &f.Description, &f.SiteURL,
		&f.Format, &f.Status, &f.SyncInterval, &f.ETag, &f.LastModified,
		&lastSync, &f.ErrorCount, &f.LastError, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if lastSync.Valid {
		t := lastSync.Time
		f.LastSync = &t
	}
	return &f, nil
}

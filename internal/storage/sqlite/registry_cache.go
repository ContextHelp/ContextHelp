package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type RegistryStore struct {
	db *sql.DB
}

func (s *RegistryStore) CacheManifest(ctx context.Context, cache *storage.RegistryCache) error {
	manifestJSON := []byte("{}")
	if cache.Manifest != nil {
		var err error
		manifestJSON, err = json.Marshal(cache.Manifest)
		if err != nil {
			return fmt.Errorf("marshal manifest: %w", err)
		}
	}

	var autoUpdate int
	if cache.AutoUpdate {
		autoUpdate = 1
	}

	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO registry_cache (
		registry_url, manifest, last_fetched, etag, auto_update
	) VALUES (?, ?, ?, ?, ?)`,
		cache.RegistryURL, manifestJSON,
		cache.LastFetched.Format(time.RFC3339),
		cache.ETag, autoUpdate,
	)
	if err != nil {
		return fmt.Errorf("cache manifest: %w", err)
	}
	return nil
}

func (s *RegistryStore) GetCachedManifest(ctx context.Context, url string) (*storage.RegistryCache, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
		registry_url, manifest, last_fetched, etag, auto_update
	FROM registry_cache WHERE registry_url = ?`, url)
	return scanRegistryCache(row)
}

func (s *RegistryStore) UpdateETag(ctx context.Context, url, etag string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE registry_cache SET
		etag = ?, last_fetched = ?
	WHERE registry_url = ?`,
		etag, time.Now().Format(time.RFC3339), url)
	if err != nil {
		return fmt.Errorf("update etag: %w", err)
	}
	return nil
}

func (s *RegistryStore) List(ctx context.Context) ([]*storage.RegistryCache, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM registry_cache").Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count registries: %w", err)
	}

	query := `SELECT
		registry_url, manifest, last_fetched, etag, auto_update
	FROM registry_cache ORDER BY last_fetched DESC`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, 0, fmt.Errorf("query registries: %w", err)
	}
	defer rows.Close()

	var registries []*storage.RegistryCache
	for rows.Next() {
		r, err := scanRegistryCache(rows)
		if err != nil {
			return nil, 0, err
		}
		registries = append(registries, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate registries: %w", err)
	}

	return registries, total, nil
}

func (s *RegistryStore) Delete(ctx context.Context, url string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM registry_cache WHERE registry_url = ?`, url)
	if err != nil {
		return fmt.Errorf("delete registry: %w", err)
	}
	return nil
}

func scanRegistryCache(row interface{ Scan(...any) error }) (*storage.RegistryCache, error) {
	var r storage.RegistryCache
	var manifestJSON string
	var lastFetched string
	var autoUpdate int

	err := row.Scan(
		&r.RegistryURL, &manifestJSON, &lastFetched,
		&r.ETag, &autoUpdate,
	)
	if err != nil {
		return nil, fmt.Errorf("scan registry cache: %w", err)
	}

	if manifestJSON != "{}" && manifestJSON != "" {
		if err := json.Unmarshal([]byte(manifestJSON), &r.Manifest); err != nil {
			return nil, fmt.Errorf("unmarshal manifest: %w", err)
		}
	}

	r.LastFetched, err = time.Parse(time.RFC3339, lastFetched)
	if err != nil {
		return nil, fmt.Errorf("parse last_fetched: %w", err)
	}

	r.AutoUpdate = autoUpdate == 1

	return &r, nil
}

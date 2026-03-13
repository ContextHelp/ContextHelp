package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func (s *RegistryStore) CacheManifest(ctx context.Context, cache *storage.RegistryCache) error {
	manifestJSON := []byte("{}")
	if cache.Manifest != nil {
		var err error
		manifestJSON, err = json.Marshal(cache.Manifest)
		if err != nil {
			return fmt.Errorf("marshal manifest: %w", err)
		}
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO registry_cache (
		registry_url, manifest, last_fetched, etag, auto_update
	) VALUES ($1, $2, $3, $4, $5)
	ON CONFLICT (registry_url) DO UPDATE SET
		manifest = EXCLUDED.manifest,
		last_fetched = EXCLUDED.last_fetched,
		etag = EXCLUDED.etag,
		auto_update = EXCLUDED.auto_update`,
		cache.RegistryURL, manifestJSON, cache.LastFetched.UTC(), cache.ETag, cache.AutoUpdate,
	)
	if err != nil {
		return fmt.Errorf("cache manifest: %w", err)
	}
	return nil
}

func (s *RegistryStore) GetCachedManifest(ctx context.Context, url string) (*storage.RegistryCache, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
		registry_url, manifest, last_fetched, etag, auto_update
	FROM registry_cache WHERE registry_url = $1`, url)
	return scanRegistryCache(row)
}

func (s *RegistryStore) UpdateETag(ctx context.Context, url, etag string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE registry_cache SET
		etag = $1, last_fetched = $2
	WHERE registry_url = $3`, etag, time.Now().UTC(), url)
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

	rows, err := s.db.QueryContext(ctx, `SELECT
		registry_url, manifest, last_fetched, etag, auto_update
	FROM registry_cache ORDER BY last_fetched DESC`)
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
	return registries, total, rows.Err()
}

func scanRegistryCache(row interface{ Scan(...any) error }) (*storage.RegistryCache, error) {
	var r storage.RegistryCache
	var manifestJSON []byte

	err := row.Scan(
		&r.RegistryURL, &manifestJSON, &r.LastFetched, &r.ETag, &r.AutoUpdate,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("registry cache not found")
		}
		return nil, fmt.Errorf("scan registry cache: %w", err)
	}
	if len(manifestJSON) > 0 && string(manifestJSON) != "{}" {
		if err := json.Unmarshal(manifestJSON, &r.Manifest); err != nil {
			return nil, fmt.Errorf("unmarshal manifest: %w", err)
		}
	}
	return &r, nil
}

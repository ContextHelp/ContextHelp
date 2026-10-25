// Package local is the filesystem storage.BlobStore: blobs live as files
// under a configured root directory.
package local

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Store implements storage.BlobStore using the local filesystem.
type Store struct {
	root string
}

// Blobs are instance data, closed to other users like the database:
// directories 0700, files 0600 (a protected instance's file-mode check
// expects them so).
const (
	dirPerm  = 0o700
	filePerm = 0o600
)

func New(root string) (*Store, error) {
	if err := os.MkdirAll(root, dirPerm); err != nil {
		return nil, fmt.Errorf("blob local: create root %q: %w", root, err)
	}
	return &Store{root: root}, nil
}

func (s *Store) blobPath(key string) string {
	if len(key) < 4 {
		return filepath.Join(s.root, key)
	}
	return filepath.Join(s.root, key[:2], key[2:4], key)
}

func (s *Store) metaPath(key string) string {
	return s.blobPath(key) + ".meta.json"
}

func (s *Store) Put(_ context.Context, key string, data io.Reader, meta storage.BlobMeta) error {
	path := s.blobPath(key)
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return fmt.Errorf("blob local put: mkdir: %w", err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, filePerm) // #nosec G304 -- key-derived path under root
	if err != nil {
		return fmt.Errorf("blob local put: create: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, data); err != nil {
		return fmt.Errorf("blob local put: write: %w", err)
	}

	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("blob local put: marshal meta: %w", err)
	}
	if err := os.WriteFile(s.metaPath(key), metaBytes, filePerm); err != nil {
		return fmt.Errorf("blob local put: write meta: %w", err)
	}

	return nil
}

func (s *Store) Get(_ context.Context, key string) (io.ReadCloser, storage.BlobMeta, error) {
	path := s.blobPath(key)
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, storage.BlobMeta{}, fmt.Errorf("blob %q: %w: %w", key, storage.ErrBlobNotFound, err)
	}
	if err != nil {
		return nil, storage.BlobMeta{}, fmt.Errorf("blob local get %q: %w", key, err)
	}

	var meta storage.BlobMeta
	metaBytes, err := os.ReadFile(s.metaPath(key))
	if err == nil {
		json.Unmarshal(metaBytes, &meta) //nolint:errcheck
	}

	return f, meta, nil
}

func (s *Store) Delete(_ context.Context, key string) error {
	os.Remove(s.metaPath(key)) //nolint:errcheck
	if err := os.Remove(s.blobPath(key)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("blob local delete: %w", err)
	}
	return nil
}

func (s *Store) Exists(_ context.Context, key string) (bool, error) {
	_, err := os.Stat(s.blobPath(key))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// List returns the blobs whose key starts with prefix, every blob for
// an empty one. A prefix of four or more characters names one shard
// directory (blobPath), so only that directory is walked.
func (s *Store) List(_ context.Context, prefix string) ([]storage.BlobInfo, error) {
	dir := s.root
	if len(prefix) >= 4 {
		dir = filepath.Dir(s.blobPath(prefix))
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return nil, nil
		}
	}
	var items []storage.BlobInfo
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || strings.HasSuffix(path, ".meta.json") {
			return nil
		}
		key := info.Name()
		if prefix != "" && !strings.HasPrefix(key, prefix) {
			return nil
		}
		items = append(items, storage.BlobInfo{
			Key:       key,
			Size:      info.Size(),
			UpdatedAt: info.ModTime(),
		})
		return nil
	})
	return items, err
}

func (s *Store) URL(_ context.Context, key string) (string, error) {
	path := s.blobPath(key)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("blob %q not found: %w", key, err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return "file://" + abs, nil
}

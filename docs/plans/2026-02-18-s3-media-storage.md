# S3-Compatible Blob Storage — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a pluggable blob storage layer with local filesystem default and generic S3-compatible backend, externalizing content above a configurable threshold via a pipeline step.

**Architecture:** BlobStore interface on StorageDriver with factory pattern (local/s3/stub backends). An `externalize-content` pipeline step handles threshold-based content externalization. TextContent field added to KnowledgeObject for FTS of binary content.

**Tech Stack:** Go, AWS SDK v2 (github.com/aws/aws-sdk-go-v2), SQLite (modernc.org/sqlite), spf13/viper config

**Design doc:** `docs/plans/2026-02-18-s3-media-storage-design.md`

---

### Task 1: Add BlobStore interface and types to storage package

**Files:**
- Modify: `internal/storage/storage.go:6-21` (add Blobs() to StorageDriver, add BlobStore interface)
- Modify: `internal/storage/types.go` (add BlobMeta, BlobInfo types)
- Modify: `internal/storage/storage_test.go` (update mockDriver, add mockBlobStore)

**Step 1: Write the failing test**

Add to `internal/storage/storage_test.go`:

```go
type mockBlobStore struct{}

func (m *mockBlobStore) Put(ctx context.Context, key string, data io.Reader, meta BlobMeta) error {
	return nil
}
func (m *mockBlobStore) Get(ctx context.Context, key string) (io.ReadCloser, BlobMeta, error) {
	return nil, BlobMeta{}, nil
}
func (m *mockBlobStore) Delete(ctx context.Context, key string) error          { return nil }
func (m *mockBlobStore) Exists(ctx context.Context, key string) (bool, error)  { return false, nil }
func (m *mockBlobStore) List(ctx context.Context, prefix string) ([]BlobInfo, error) { return nil, nil }
func (m *mockBlobStore) URL(ctx context.Context, key string) (string, error)   { return "", nil }

func TestBlobStoreInterfaceSatisfaction(t *testing.T) {
	var s BlobStore = &mockBlobStore{}
	if s == nil {
		t.Fatal("mockBlobStore should satisfy BlobStore")
	}
}
```

Update `mockDriver` to add:
```go
func (m *mockDriver) Blobs() BlobStore { return &mockBlobStore{} }
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/storage/ -run TestBlobStore -v`
Expected: FAIL — `BlobStore`, `BlobMeta`, `BlobInfo` types undefined, `Blobs()` method missing from interface

**Step 3: Add BlobStore interface to storage.go**

Add to `internal/storage/storage.go` — add `"io"` to imports and append after StorageDriver:

```go
import (
	"context"
	"io"
)

// StorageDriver — add this method to the interface:
	Blobs() BlobStore

// BlobStore manages external binary content.
type BlobStore interface {
	Put(ctx context.Context, key string, data io.Reader, meta BlobMeta) error
	Get(ctx context.Context, key string) (io.ReadCloser, BlobMeta, error)
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	List(ctx context.Context, prefix string) ([]BlobInfo, error)
	URL(ctx context.Context, key string) (string, error)
}
```

Add to `internal/storage/types.go`:

```go
// BlobMeta describes metadata for a stored blob.
type BlobMeta struct {
	ContentType string            `json:"content_type"`
	Size        int64             `json:"size"`
	ContentHash string            `json:"content_hash"`
	Filename    string            `json:"filename,omitempty"`
	Properties  map[string]string `json:"properties,omitempty"`
}

// BlobInfo describes a blob in a listing.
type BlobInfo struct {
	Key       string    `json:"key"`
	Size      int64     `json:"size"`
	UpdatedAt time.Time `json:"updated_at"`
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/storage/ -run TestBlobStore -v`
Expected: PASS

Note: `go test ./internal/storage/sqlite/...` will FAIL because `sqlite.Driver` no longer satisfies `StorageDriver` (missing `Blobs()`). That's expected — Task 6 fixes it.

**Step 5: Commit**

```
feat(storage): add BlobStore interface and types
```

---

### Task 2: Add TextContent field to KnowledgeObject

**Files:**
- Modify: `internal/storage/types.go:6-31` (add TextContent field)

**Step 1: Add the field**

Add to `KnowledgeObject` struct in `internal/storage/types.go`, after `RawContent`:

```go
TextContent string `json:"text_content,omitempty"`
```

**Step 2: Run existing tests to verify nothing breaks**

Run: `go test ./internal/storage/ -v`
Expected: PASS (adding a zero-value field to a struct is backward-compatible)

**Step 3: Commit**

```
feat(storage): add TextContent field for extracted text from blobs
```

---

### Task 3: Add BlobConfig to configuration

**Files:**
- Modify: `internal/config/config.go:53-56` (add Blob field to StorageConfig)
- Create: `internal/config/config_test.go` (test blob config defaults)

**Step 1: Write the failing test**

Create `internal/config/config_test.go`:

```go
package config

import (
	"testing"
	"time"
)

func TestBlobConfigDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Storage.Blob.Backend != "local" {
		t.Errorf("blob backend: got %q, want %q", cfg.Storage.Blob.Backend, "local")
	}
	if cfg.Storage.Blob.Threshold != 65536 {
		t.Errorf("blob threshold: got %d, want %d", cfg.Storage.Blob.Threshold, 65536)
	}
	if cfg.Storage.Blob.S3.Region != "us-east-1" {
		t.Errorf("s3 region: got %q, want %q", cfg.Storage.Blob.S3.Region, "us-east-1")
	}
	if cfg.Storage.Blob.S3.MaxRetries != 3 {
		t.Errorf("s3 max_retries: got %d, want %d", cfg.Storage.Blob.S3.MaxRetries, 3)
	}
	if cfg.Storage.Blob.S3.PresignExpiry != time.Hour {
		t.Errorf("s3 presign_expiry: got %v, want %v", cfg.Storage.Blob.S3.PresignExpiry, time.Hour)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestBlobConfig -v`
Expected: FAIL — `Blob` field undefined on `StorageConfig`

**Step 3: Add BlobConfig types and defaults**

Add to `internal/config/config.go`:

```go
import "time"

// StorageConfig — add Blob field:
type StorageConfig struct {
	Type string     `mapstructure:"type"`
	Path string     `mapstructure:"path"`
	Blob BlobConfig `mapstructure:"blob"`
}

type BlobConfig struct {
	Backend   string          `mapstructure:"backend"`
	Threshold int64           `mapstructure:"threshold"`
	Local     BlobLocalConfig `mapstructure:"local"`
	S3        BlobS3Config    `mapstructure:"s3"`
}

type BlobLocalConfig struct {
	Path string `mapstructure:"path"`
}

type BlobS3Config struct {
	Endpoint      string        `mapstructure:"endpoint"`
	Region        string        `mapstructure:"region"`
	Bucket        string        `mapstructure:"bucket"`
	Prefix        string        `mapstructure:"prefix"`
	AccessKey     string        `mapstructure:"access_key"`
	SecretKey     string        `mapstructure:"secret_key"`
	UsePathStyle  bool          `mapstructure:"use_path_style"`
	PresignExpiry time.Duration `mapstructure:"presign_expiry"`
	MaxRetries    int           `mapstructure:"max_retries"`
}
```

Add to `setDefaults()`:

```go
v.SetDefault("storage.blob.backend", "local")
v.SetDefault("storage.blob.threshold", 65536)
v.SetDefault("storage.blob.local.path", filepath.Join(dataDir, "blobs"))
v.SetDefault("storage.blob.s3.region", "us-east-1")
v.SetDefault("storage.blob.s3.use_path_style", false)
v.SetDefault("storage.blob.s3.presign_expiry", time.Hour)
v.SetDefault("storage.blob.s3.max_retries", 3)
```

Add to `bindEnvVars()`:

```go
v.BindEnv("storage.blob.backend", "CTXT_BLOB_BACKEND")
v.BindEnv("storage.blob.threshold", "CTXT_BLOB_THRESHOLD")
v.BindEnv("storage.blob.s3.endpoint", "CTXT_BLOB_S3_ENDPOINT")
v.BindEnv("storage.blob.s3.region", "CTXT_BLOB_S3_REGION")
v.BindEnv("storage.blob.s3.bucket", "CTXT_BLOB_S3_BUCKET")
v.BindEnv("storage.blob.s3.prefix", "CTXT_BLOB_S3_PREFIX")
v.BindEnv("storage.blob.s3.access_key", "CTXT_BLOB_S3_ACCESS_KEY")
v.BindEnv("storage.blob.s3.secret_key", "CTXT_BLOB_S3_SECRET_KEY")
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestBlobConfig -v`
Expected: PASS

**Step 5: Commit**

```
feat(config): add blob storage configuration with local/s3 backends
```

---

### Task 4: Implement stub blob backend

**Files:**
- Create: `internal/storage/blob/stub/store.go`
- Create: `internal/storage/blob/stub/store_test.go`

**Step 1: Write the failing test**

Create `internal/storage/blob/stub/store_test.go`:

```go
package stub

import (
	"context"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestStubPut(t *testing.T) {
	s := New()
	err := s.Put(context.Background(), "abc123", strings.NewReader("data"), storage.BlobMeta{})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
}

func TestStubGetNotFound(t *testing.T) {
	s := New()
	_, _, err := s.Get(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestStubExists(t *testing.T) {
	s := New()
	ok, err := s.Exists(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if ok {
		t.Error("stub should always return false for exists")
	}
}

func TestStubList(t *testing.T) {
	s := New()
	items, err := s.List(context.Background(), "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}

func TestStubURL(t *testing.T) {
	s := New()
	_, err := s.URL(context.Background(), "abc123")
	if err == nil {
		t.Fatal("expected error for URL on stub")
	}
}

func TestStubDelete(t *testing.T) {
	s := New()
	err := s.Delete(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
}

// Verify interface compliance.
var _ storage.BlobStore = (*Store)(nil)
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/storage/blob/stub/ -v`
Expected: FAIL — package doesn't exist

**Step 3: Implement the stub**

Create `internal/storage/blob/stub/store.go`:

```go
package stub

import (
	"context"
	"fmt"
	"io"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Store is a no-op BlobStore for testing.
type Store struct{}

func New() *Store { return &Store{} }

func (s *Store) Put(_ context.Context, _ string, _ io.Reader, _ storage.BlobMeta) error {
	return nil
}

func (s *Store) Get(_ context.Context, key string) (io.ReadCloser, storage.BlobMeta, error) {
	return nil, storage.BlobMeta{}, fmt.Errorf("blob %q not found (stub)", key)
}

func (s *Store) Delete(_ context.Context, _ string) error { return nil }

func (s *Store) Exists(_ context.Context, _ string) (bool, error) { return false, nil }

func (s *Store) List(_ context.Context, _ string) ([]storage.BlobInfo, error) { return nil, nil }

func (s *Store) URL(_ context.Context, key string) (string, error) {
	return "", fmt.Errorf("blob %q: URL not available (stub)", key)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/storage/blob/stub/ -v`
Expected: PASS

**Step 5: Commit**

```
feat(blob): add stub backend for testing
```

---

### Task 5: Implement local filesystem blob backend

**Files:**
- Create: `internal/storage/blob/local/store.go`
- Create: `internal/storage/blob/local/store_test.go`

**Step 1: Write the failing tests**

Create `internal/storage/blob/local/store_test.go`:

```go
package local

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return s
}

func TestPutAndGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	data := []byte("hello blob world")
	meta := storage.BlobMeta{
		ContentType: "text/plain",
		Size:        int64(len(data)),
		ContentHash: "abc123def456",
		Filename:    "test.txt",
	}

	err := s.Put(ctx, "abc123def456", bytes.NewReader(data), meta)
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	rc, gotMeta, err := s.Get(ctx, "abc123def456")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("data: got %q, want %q", got, data)
	}
	if gotMeta.ContentType != "text/plain" {
		t.Errorf("content_type: got %q, want %q", gotMeta.ContentType, "text/plain")
	}
	if gotMeta.Filename != "test.txt" {
		t.Errorf("filename: got %q, want %q", gotMeta.Filename, "test.txt")
	}
}

func TestExists(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	ok, err := s.Exists(ctx, "missing")
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if ok {
		t.Error("expected false for missing key")
	}

	data := []byte("test")
	_ = s.Put(ctx, "present", bytes.NewReader(data), storage.BlobMeta{Size: int64(len(data))})

	ok, err = s.Exists(ctx, "present")
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if !ok {
		t.Error("expected true for existing key")
	}
}

func TestDelete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	data := []byte("delete me")
	_ = s.Put(ctx, "todelete", bytes.NewReader(data), storage.BlobMeta{Size: int64(len(data))})

	err := s.Delete(ctx, "todelete")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	ok, _ := s.Exists(ctx, "todelete")
	if ok {
		t.Error("expected false after delete")
	}
}

func TestList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	keys := []string{"aabb11", "aabb22", "ccdd33"}
	for _, k := range keys {
		_ = s.Put(ctx, k, bytes.NewReader([]byte("x")), storage.BlobMeta{Size: 1})
	}

	items, err := s.List(ctx, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 3 {
		t.Errorf("expected 3 items, got %d", len(items))
	}
}

func TestURL(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	data := []byte("url test")
	_ = s.Put(ctx, "urlkey1", bytes.NewReader(data), storage.BlobMeta{Size: int64(len(data))})

	u, err := s.URL(ctx, "urlkey1")
	if err != nil {
		t.Fatalf("url: %v", err)
	}
	if u == "" {
		t.Error("expected non-empty URL")
	}
	// Should be a file:// URL.
	if len(u) < 7 || u[:7] != "file://" {
		t.Errorf("expected file:// URL, got %q", u)
	}
}

func TestPutIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	data := []byte("idempotent")
	meta := storage.BlobMeta{Size: int64(len(data)), ContentType: "text/plain"}

	_ = s.Put(ctx, "idem", bytes.NewReader(data), meta)
	err := s.Put(ctx, "idem", bytes.NewReader(data), meta)
	if err != nil {
		t.Fatalf("second put: %v", err)
	}
}

func TestKeySharding(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	key := "abcdef1234567890"

	_ = s.Put(ctx, key, bytes.NewReader([]byte("x")), storage.BlobMeta{Size: 1})

	// Verify 2-level sharding: root/ab/cd/abcdef1234567890
	expected := s.blobPath(key)
	if _, err := os.Stat(expected); err != nil {
		t.Errorf("expected sharded path %q to exist: %v", expected, err)
	}
}

var _ storage.BlobStore = (*Store)(nil)
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/storage/blob/local/ -v`
Expected: FAIL — package doesn't exist

**Step 3: Implement the local store**

Create `internal/storage/blob/local/store.go`:

```go
package local

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Store implements storage.BlobStore using the local filesystem.
type Store struct {
	root string
}

func New(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0755); err != nil {
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
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("blob local put: mkdir: %w", err)
	}

	f, err := os.Create(path)
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
	if err := os.WriteFile(s.metaPath(key), metaBytes, 0644); err != nil {
		return fmt.Errorf("blob local put: write meta: %w", err)
	}

	return nil
}

func (s *Store) Get(_ context.Context, key string) (io.ReadCloser, storage.BlobMeta, error) {
	path := s.blobPath(key)
	f, err := os.Open(path)
	if err != nil {
		return nil, storage.BlobMeta{}, fmt.Errorf("blob %q not found: %w", key, err)
	}

	var meta storage.BlobMeta
	metaBytes, err := os.ReadFile(s.metaPath(key))
	if err == nil {
		json.Unmarshal(metaBytes, &meta)
	}

	return f, meta, nil
}

func (s *Store) Delete(_ context.Context, key string) error {
	os.Remove(s.metaPath(key))
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

func (s *Store) List(_ context.Context, prefix string) ([]storage.BlobInfo, error) {
	var items []storage.BlobInfo
	err := filepath.Walk(s.root, func(path string, info os.FileInfo, err error) error {
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
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/storage/blob/local/ -v`
Expected: PASS

**Step 5: Commit**

```
feat(blob): add local filesystem backend with 2-level hash sharding
```

---

### Task 6: Implement blob factory and wire into SQLite driver

**Files:**
- Create: `internal/storage/blob/factory.go`
- Create: `internal/storage/blob/factory_test.go`
- Modify: `internal/storage/sqlite/driver.go:14-28,30-62,73-83` (add blobs field, init in New, add Blobs() method)

**Step 1: Write the failing test for factory**

Create `internal/storage/blob/factory_test.go`:

```go
package blob

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestFactoryLocal(t *testing.T) {
	dir := t.TempDir()
	cfg := config.BlobConfig{
		Backend: "local",
		Local:   config.BlobLocalConfig{Path: dir},
	}
	store, err := New(cfg)
	if err != nil {
		t.Fatalf("new local: %v", err)
	}
	var _ storage.BlobStore = store
}

func TestFactoryStub(t *testing.T) {
	cfg := config.BlobConfig{Backend: "stub"}
	store, err := New(cfg)
	if err != nil {
		t.Fatalf("new stub: %v", err)
	}
	var _ storage.BlobStore = store
}

func TestFactoryUnknown(t *testing.T) {
	cfg := config.BlobConfig{Backend: "unknown"}
	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected error for unknown backend")
	}
}

func TestFactoryDefault(t *testing.T) {
	dir := t.TempDir()
	cfg := config.BlobConfig{
		Backend: "",
		Local:   config.BlobLocalConfig{Path: dir},
	}
	store, err := New(cfg)
	if err != nil {
		t.Fatalf("new default: %v", err)
	}
	var _ storage.BlobStore = store
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/storage/blob/ -v`
Expected: FAIL — package doesn't exist

**Step 3: Implement factory**

Create `internal/storage/blob/factory.go`:

```go
package blob

import (
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/local"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/stub"
)

// New creates a BlobStore based on the configuration.
func New(cfg config.BlobConfig) (storage.BlobStore, error) {
	backend := cfg.Backend
	if backend == "" {
		backend = "local"
	}

	switch backend {
	case "local":
		return local.New(cfg.Local.Path)
	case "s3":
		return nil, fmt.Errorf("blob: s3 backend not yet implemented")
	case "stub":
		return stub.New(), nil
	default:
		return nil, fmt.Errorf("blob: unknown backend %q (valid: local, s3, stub)", backend)
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/storage/blob/ -v`
Expected: PASS

**Step 5: Wire Blobs() into SQLite driver**

Modify `internal/storage/sqlite/driver.go`:

Add `blobs` field to `Driver` struct and `Blobs()` accessor. The SQLite driver doesn't own the blob store itself — it receives it from the caller or returns a stub. Add a `SetBlobs` method:

```go
// Add field to Driver struct:
blobs storage.BlobStore

// Add method:
func (d *Driver) Blobs() storage.BlobStore { return d.blobs }

// Add setter for injection:
func (d *Driver) SetBlobs(bs storage.BlobStore) { d.blobs = bs }
```

Initialize with stub in `New()`:

```go
import "github.com/ideacrafterslabs/ctxt/internal/storage/blob/stub"

// At end of New(), before return:
d.blobs = stub.New()
```

**Step 6: Run full test suite to verify**

Run: `go test ./internal/storage/... -v`
Expected: PASS (all existing tests + new ones)

**Step 7: Commit**

```
feat(blob): add factory and wire BlobStore into SQLite driver
```

---

### Task 7: Add SQLite migration for text_content and FTS update

**Files:**
- Create: `internal/storage/sqlite/migrations/004_text_content.sql`
- Modify: `internal/storage/sqlite/migrations.go:10-28` (add migration004 embed)
- Modify: `internal/storage/sqlite/objects.go` (add text_content to all queries)
- Modify: `internal/storage/sqlite/objects_test.go` (if exists, verify text_content round-trip)

**Step 1: Write the migration SQL**

Create `internal/storage/sqlite/migrations/004_text_content.sql`:

```sql
-- Add text_content column for searchable text extracted from binary blobs.
ALTER TABLE objects ADD COLUMN text_content TEXT DEFAULT '';

-- Rebuild FTS to include text_content.
DROP TABLE IF EXISTS objects_fts;
CREATE VIRTUAL TABLE objects_fts USING fts5(
    id UNINDEXED,
    summaries,
    raw_content,
    text_content,
    content='objects',
    content_rowid='rowid'
);
```

**Step 2: Register migration**

Add to `internal/storage/sqlite/migrations.go`:

```go
//go:embed migrations/004_text_content.sql
var migration004 string

// Add to migrations slice:
{Version: 4, SQL: migration004},
```

**Step 3: Update objects.go — add text_content to all SQL queries**

In `internal/storage/sqlite/objects.go`, update every query that reads/writes objects to include `text_content`:

**Create** — add `text_content` to INSERT column list and values:
- Column list: add after `content_type`
- Values: add `obj.TextContent` after `obj.ContentType`

**Get/GetByContentHash/List/ListBySQL** — add `text_content` to SELECT column list:
- Add after `content_type`

**Update** — add `text_content=?` to SET clause:
- Add `obj.TextContent` to values after `obj.ContentType`

**scanObject/scanObjectFromRows** — add `&obj.TextContent` to Scan:
- Add after `&obj.ContentType`

**Note:** The `unmarshalObjectJSON` function doesn't handle TextContent (it's scanned directly as a string, not JSON). No changes needed there.

**Step 4: Run migration and object tests**

Run: `go test ./internal/storage/sqlite/ -v`
Expected: PASS

**Step 5: Commit**

```
feat(sqlite): add text_content column and rebuild FTS with extracted text support
```

---

### Task 8: Implement blob resolve helper

**Files:**
- Create: `internal/storage/blob/resolve.go`
- Create: `internal/storage/blob/resolve_test.go`

**Step 1: Write the failing test**

Create `internal/storage/blob/resolve_test.go`:

```go
package blob

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/local"
)

func TestResolveInlineContent(t *testing.T) {
	s, _ := local.New(t.TempDir())
	obj := &storage.KnowledgeObject{RawContent: "just plain text"}

	rc, err := Resolve(context.Background(), s, obj)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	defer rc.Close()

	got, _ := io.ReadAll(rc)
	if string(got) != "just plain text" {
		t.Errorf("got %q, want %q", got, "just plain text")
	}
}

func TestResolveBlobReference(t *testing.T) {
	s, _ := local.New(t.TempDir())
	ctx := context.Background()

	original := []byte("blob content here")
	_ = s.Put(ctx, "abc123", bytes.NewReader(original), storage.BlobMeta{
		ContentType: "text/plain",
		Size:        int64(len(original)),
	})

	obj := &storage.KnowledgeObject{RawContent: "blob://abc123"}

	rc, err := Resolve(ctx, s, obj)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	defer rc.Close()

	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, original) {
		t.Errorf("got %q, want %q", got, original)
	}
}

func TestIsBlobRef(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"blob://abc123", true},
		{"blob://", false},
		{"regular text", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsBlobRef(tt.input); got != tt.want {
			t.Errorf("IsBlobRef(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestBlobKey(t *testing.T) {
	key, ok := BlobKey("blob://abc123")
	if !ok || key != "abc123" {
		t.Errorf("BlobKey: got %q/%v, want %q/%v", key, ok, "abc123", true)
	}

	_, ok = BlobKey("regular text")
	if ok {
		t.Error("BlobKey should return false for non-blob ref")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/storage/blob/ -run TestResolve -v`
Expected: FAIL — `Resolve`, `IsBlobRef`, `BlobKey` not defined

**Step 3: Implement resolve.go**

Create `internal/storage/blob/resolve.go`:

```go
package blob

import (
	"context"
	"io"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

const blobPrefix = "blob://"

// IsBlobRef returns true if the content is a blob reference.
func IsBlobRef(content string) bool {
	return strings.HasPrefix(content, blobPrefix) && len(content) > len(blobPrefix)
}

// BlobKey extracts the blob key from a blob:// reference.
func BlobKey(content string) (string, bool) {
	if !IsBlobRef(content) {
		return "", false
	}
	return content[len(blobPrefix):], true
}

// Resolve returns a reader for the object's content.
// If RawContent is a blob:// reference, it fetches from the blob store.
// Otherwise, it wraps the inline content in a reader.
func Resolve(ctx context.Context, store storage.BlobStore, obj *storage.KnowledgeObject) (io.ReadCloser, error) {
	key, ok := BlobKey(obj.RawContent)
	if !ok {
		return io.NopCloser(strings.NewReader(obj.RawContent)), nil
	}
	rc, _, err := store.Get(ctx, key)
	return rc, err
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/storage/blob/ -v`
Expected: PASS

**Step 5: Commit**

```
feat(blob): add Resolve helper and blob:// reference utilities
```

---

### Task 9: Implement externalize-content pipeline step

**Files:**
- Create: `internal/pipeline/steps/externalize.go`
- Create: `internal/pipeline/steps/externalize_test.go`

**Step 1: Write the failing tests**

Create `internal/pipeline/steps/externalize_test.go`:

```go
package steps

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/local"
)

func TestExternalizeUnderThreshold(t *testing.T) {
	bs, _ := local.New(t.TempDir())
	step := NewExternalizer(bs, 1024)

	draft := &storage.KnowledgeObject{
		RawContent: "short content",
		Source:     "test",
	}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.RawContent != "short content" {
		t.Errorf("content should be unchanged, got %q", got.RawContent)
	}
}

func TestExternalizeOverThreshold(t *testing.T) {
	bs, _ := local.New(t.TempDir())
	step := NewExternalizer(bs, 10) // low threshold for testing

	bigContent := strings.Repeat("x", 100)
	draft := &storage.KnowledgeObject{
		RawContent:  bigContent,
		ContentType: "text/plain",
		Source:      "test",
	}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if !blob.IsBlobRef(got.RawContent) {
		t.Fatalf("expected blob:// reference, got %q", got.RawContent)
	}

	// Verify metadata was set.
	if got.Metadata["blob_key"] == nil {
		t.Error("expected blob_key in metadata")
	}
	if got.Metadata["blob_original_size"] == nil {
		t.Error("expected blob_original_size in metadata")
	}

	// Verify content is retrievable from blob store.
	key, _ := blob.BlobKey(got.RawContent)
	rc, _, err := bs.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get blob: %v", err)
	}
	defer rc.Close()
	retrieved, _ := io.ReadAll(rc)
	if !bytes.Equal(retrieved, []byte(bigContent)) {
		t.Errorf("retrieved content mismatch: got %d bytes, want %d", len(retrieved), len(bigContent))
	}
}

func TestExternalizeExactThreshold(t *testing.T) {
	bs, _ := local.New(t.TempDir())
	step := NewExternalizer(bs, 10)

	// Exactly at threshold — should stay inline.
	draft := &storage.KnowledgeObject{
		RawContent: strings.Repeat("x", 10),
		Source:     "test",
	}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if blob.IsBlobRef(got.RawContent) {
		t.Error("content at threshold should stay inline")
	}
}

func TestExternalizeZeroThreshold(t *testing.T) {
	bs, _ := local.New(t.TempDir())
	step := NewExternalizer(bs, 0) // 0 = never externalize

	draft := &storage.KnowledgeObject{
		RawContent: strings.Repeat("x", 1000),
		Source:     "test",
	}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if blob.IsBlobRef(got.RawContent) {
		t.Error("threshold 0 should never externalize")
	}
}

func TestExternalizeContract(t *testing.T) {
	bs, _ := local.New(t.TempDir())
	step := NewExternalizer(bs, 1024)

	c := step.Contract()
	if len(c.Requires) == 0 || c.Requires[0] != "RawContent" {
		t.Errorf("requires: got %v, want [RawContent]", c.Requires)
	}
	found := false
	for _, cap := range c.Capabilities {
		if cap == "blob-externalize" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected blob-externalize capability, got %v", c.Capabilities)
	}
}

func TestExternalizeName(t *testing.T) {
	bs, _ := local.New(t.TempDir())
	step := NewExternalizer(bs, 1024)
	if step.Name() != "externalize_content" {
		t.Errorf("name: got %q, want %q", step.Name(), "externalize_content")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/steps/ -run TestExternalize -v`
Expected: FAIL — `NewExternalizer` not defined

**Step 3: Implement the step**

Create `internal/pipeline/steps/externalize.go`:

```go
package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// Externalizer moves content above a size threshold to a blob store.
type Externalizer struct {
	pipeline.BaseContract
	store     storage.BlobStore
	threshold int64
}

func NewExternalizer(store storage.BlobStore, threshold int64) *Externalizer {
	return &Externalizer{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires:     []string{"RawContent"},
			Produces:     []string{"RawContent", "Metadata"},
			Capabilities: []string{"blob-externalize"},
		}),
		store:     store,
		threshold: threshold,
	}
}

func (e *Externalizer) Name() string { return "externalize_content" }

func (e *Externalizer) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if e.threshold <= 0 {
		return draft, nil
	}

	if int64(len(draft.RawContent)) <= e.threshold {
		return draft, nil
	}

	hash := storageutil.ContentHash(draft.RawContent, draft.Source)

	err := e.store.Put(ctx, hash, strings.NewReader(draft.RawContent), storage.BlobMeta{
		ContentType: draft.ContentType,
		Size:        int64(len(draft.RawContent)),
		ContentHash: hash,
	})
	if err != nil {
		return nil, fmt.Errorf("externalize: blob put: %w", err)
	}

	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["blob_key"] = hash
	draft.Metadata["blob_original_size"] = int64(len(draft.RawContent))
	draft.Metadata["blob_content_type"] = draft.ContentType

	draft.RawContent = "blob://" + hash
	return draft, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/pipeline/steps/ -run TestExternalize -v`
Expected: PASS

**Step 5: Commit**

```
feat(pipeline): add externalize-content step for blob storage
```

---

### Task 10: Register externalize step in builtins

**Files:**
- Modify: `internal/pipeline/builtins/builtins.go:35-45` (add externalize_content to stepConstructors)
- Modify: `internal/pipeline/builtins/capabilities.go` (add blob-externalize capability check)

**Step 1: Wire the step constructor**

The externalize step needs a `BlobStore` and threshold, so it goes in a new category — blob-aware constructors. Add a new constructor type and registration mechanism.

Add to `internal/pipeline/builtins/builtins.go`:

```go
// blobStepConstructors maps step names to blob-store-aware constructors.
var blobStepConstructors = map[string]func(storage.BlobStore, int64) pipeline.PipelineStep{
	"externalize_content": func(bs storage.BlobStore, threshold int64) pipeline.PipelineStep {
		return steps.NewExternalizer(bs, threshold)
	},
}
```

Update `resolveStep` to check blobStepConstructors (needs BlobStore + threshold passed through). This requires a small refactor: pass a `BuildContext` struct that carries Factory + BlobStore + threshold.

Alternatively, keep it simpler: add `BlobStore` and `BlobThreshold` fields to a new `BuildOpts` struct passed to `buildRegistry`:

```go
type BuildOpts struct {
	Factory       *providers.Factory
	BlobStore     storage.BlobStore
	BlobThreshold int64
}
```

Update `ConfiguredRegistry` and `buildRegistry` to accept `BuildOpts`:

```go
func ConfiguredRegistry(opts BuildOpts) pipeline.Registry {
	return buildRegistry(opts, false)
}
```

Update `resolveStep` to accept `BuildOpts`:

```go
func resolveStep(name string, opts BuildOpts) (pipeline.PipelineStep, error) {
	if opts.Factory != nil {
		if ctor, ok := providerStepConstructors[name]; ok {
			return ctor(opts.Factory), nil
		}
	}
	if opts.BlobStore != nil {
		if ctor, ok := blobStepConstructors[name]; ok {
			return ctor(opts.BlobStore, opts.BlobThreshold), nil
		}
	}
	if ctor, ok := stepConstructors[name]; ok {
		return ctor(), nil
	}
	// provider fallbacks...
}
```

**Step 2: Add blob-externalize capability**

Modify `internal/pipeline/builtins/capabilities.go`, add to `CapabilitiesFromFactory`:

```go
// In CapabilitiesFromFactory, add a BlobStore parameter check.
// Better: rename to CapabilitiesFromOpts(opts BuildOpts) and check opts.BlobStore != nil.
```

Create `CapabilitiesFromOpts`:

```go
func CapabilitiesFromOpts(opts BuildOpts) pipeline.CapabilitySet {
	caps := CapabilitiesFromFactory(opts.Factory)
	if opts.BlobStore != nil {
		caps["blob-externalize"] = true
	}
	return caps
}
```

Update `buildPipeline` to use `CapabilitiesFromOpts`.

**Step 3: Run all builtin tests**

Run: `go test ./internal/pipeline/builtins/ -v`
Expected: PASS

**Step 4: Commit**

```
feat(builtins): register externalize_content step with blob capability
```

---

### Task 11: Implement S3-compatible blob backend

**Files:**
- Create: `internal/storage/blob/s3/store.go`
- Create: `internal/storage/blob/s3/store_test.go`

**Step 1: Add AWS SDK v2 dependency**

Run: `go get github.com/aws/aws-sdk-go-v2 github.com/aws/aws-sdk-go-v2/config github.com/aws/aws-sdk-go-v2/service/s3 github.com/aws/aws-sdk-go-v2/credentials`

**Step 2: Write tests (unit tests with mocked S3)**

Create `internal/storage/blob/s3/store_test.go`:

```go
package s3

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestNewMissingBucket(t *testing.T) {
	cfg := config.BlobS3Config{
		Region: "us-east-1",
	}
	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected error for missing bucket")
	}
}

func TestKeyPath(t *testing.T) {
	s := &Store{prefix: "data"}
	got := s.keyPath("abcdef1234")
	want := "data/ab/cd/abcdef1234"
	if got != want {
		t.Errorf("keyPath: got %q, want %q", got, want)
	}
}

func TestKeyPathNoPrefix(t *testing.T) {
	s := &Store{}
	got := s.keyPath("abcdef1234")
	want := "ab/cd/abcdef1234"
	if got != want {
		t.Errorf("keyPath: got %q, want %q", got, want)
	}
}

var _ storage.BlobStore = (*Store)(nil)
```

**Step 3: Implement S3 store**

Create `internal/storage/blob/s3/store.go`:

```go
package s3

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	cfgpkg "github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type Store struct {
	client        *s3.Client
	bucket        string
	prefix        string
	presignExpiry time.Duration
}

func New(cfg cfgpkg.BlobS3Config) (*Store, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("blob s3: bucket is required")
	}

	ctx := context.Background()
	var opts []func(*awsconfig.LoadOptions) error

	if cfg.Region != "" {
		opts = append(opts, awsconfig.WithRegion(cfg.Region))
	}
	if cfg.AccessKey != "" && cfg.SecretKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("blob s3: load config: %w", err)
	}

	var clientOpts []func(*s3.Options)
	if cfg.Endpoint != "" {
		clientOpts = append(clientOpts, func(o *s3.Options) {
			o.BaseEndpoint = &cfg.Endpoint
			o.UsePathStyle = cfg.UsePathStyle
		})
	}

	presign := cfg.PresignExpiry
	if presign == 0 {
		presign = time.Hour
	}

	return &Store{
		client:        s3.NewFromConfig(awsCfg, clientOpts...),
		bucket:        cfg.Bucket,
		prefix:        cfg.Prefix,
		presignExpiry: presign,
	}, nil
}

func (s *Store) keyPath(key string) string {
	var sharded string
	if len(key) >= 4 {
		sharded = path.Join(key[:2], key[2:4], key)
	} else {
		sharded = key
	}
	if s.prefix != "" {
		return path.Join(s.prefix, sharded)
	}
	return sharded
}

func (s *Store) metaKey(key string) string {
	return s.keyPath(key) + ".meta.json"
}

func (s *Store) Put(ctx context.Context, key string, data io.Reader, meta storage.BlobMeta) error {
	objKey := s.keyPath(key)
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &s.bucket,
		Key:         &objKey,
		Body:        data,
		ContentType: &meta.ContentType,
	})
	if err != nil {
		return fmt.Errorf("blob s3 put: %w", err)
	}

	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("blob s3 put meta: marshal: %w", err)
	}
	metaKey := s.metaKey(key)
	ct := "application/json"
	metaReader := io.NopCloser(io.NewSectionReader(
		readerAtFromBytes(metaBytes), 0, int64(len(metaBytes)),
	))
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &s.bucket,
		Key:         &metaKey,
		Body:        metaReader,
		ContentType: &ct,
	})
	if err != nil {
		return fmt.Errorf("blob s3 put meta: %w", err)
	}

	return nil
}

func (s *Store) Get(ctx context.Context, key string) (io.ReadCloser, storage.BlobMeta, error) {
	objKey := s.keyPath(key)
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.bucket,
		Key:    &objKey,
	})
	if err != nil {
		return nil, storage.BlobMeta{}, fmt.Errorf("blob %q not found: %w", key, err)
	}

	var meta storage.BlobMeta
	metaKey := s.metaKey(key)
	metaResult, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.bucket,
		Key:    &metaKey,
	})
	if err == nil {
		defer metaResult.Body.Close()
		json.NewDecoder(metaResult.Body).Decode(&meta)
	}

	return result.Body, meta, nil
}

func (s *Store) Delete(ctx context.Context, key string) error {
	objKey := s.keyPath(key)
	metaKey := s.metaKey(key)

	_, _ = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &s.bucket,
		Key:    &metaKey,
	})
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &s.bucket,
		Key:    &objKey,
	})
	if err != nil {
		return fmt.Errorf("blob s3 delete: %w", err)
	}
	return nil
}

func (s *Store) Exists(ctx context.Context, key string) (bool, error) {
	objKey := s.keyPath(key)
	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: &s.bucket,
		Key:    &objKey,
	})
	if err != nil {
		return false, nil
	}
	return true, nil
}

func (s *Store) List(ctx context.Context, prefix string) ([]storage.BlobInfo, error) {
	listPrefix := s.prefix
	if prefix != "" {
		listPrefix = path.Join(s.prefix, prefix)
	}

	result, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: &s.bucket,
		Prefix: &listPrefix,
	})
	if err != nil {
		return nil, fmt.Errorf("blob s3 list: %w", err)
	}

	var items []storage.BlobInfo
	for _, obj := range result.Contents {
		key := path.Base(*obj.Key)
		// Skip meta files.
		if len(key) > 10 && key[len(key)-10:] == ".meta.json" {
			continue
		}
		items = append(items, storage.BlobInfo{
			Key:       key,
			Size:      *obj.Size,
			UpdatedAt: *obj.LastModified,
		})
	}
	return items, nil
}

func (s *Store) URL(ctx context.Context, key string) (string, error) {
	objKey := s.keyPath(key)
	presigner := s3.NewPresignClient(s.client)
	result, err := presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.bucket,
		Key:    &objKey,
	}, s3.WithPresignExpires(s.presignExpiry))
	if err != nil {
		return "", fmt.Errorf("blob s3 url: %w", err)
	}
	return result.URL, nil
}

// readerAtFromBytes wraps a byte slice as an io.ReaderAt.
type bytesReaderAt struct {
	data []byte
}

func readerAtFromBytes(data []byte) *bytesReaderAt {
	return &bytesReaderAt{data: data}
}

func (r *bytesReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(r.data)) {
		return 0, io.EOF
	}
	n := copy(p, r.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}
```

**Step 4: Update factory to support S3**

In `internal/storage/blob/factory.go`, replace the S3 stub:

```go
case "s3":
	return s3store.New(cfg.S3)
```

Add import: `s3store "github.com/ideacrafterslabs/ctxt/internal/storage/blob/s3"`

**Step 5: Run tests**

Run: `go test ./internal/storage/blob/s3/ -v`
Expected: PASS (unit tests only — no live S3 needed)

**Step 6: Commit**

```
feat(blob): add S3-compatible backend with presigned URLs
```

---

### Task 12: Wire blob store initialization into the application

**Files:**
- Modify: application entry point / housekeeping that creates the storage driver (look at `cmd/dpkms/` or wherever `sqlite.New()` is called)
- This wires `blob.New(cfg.Storage.Blob)` and calls `driver.SetBlobs(blobStore)` after creating the SQLite driver

**Step 1: Find the initialization site**

Search for `sqlite.New(` to find where the driver is created. Wire blob factory there:

```go
blobStore, err := blob.New(cfg.Storage.Blob)
if err != nil {
	return fmt.Errorf("init blob store: %w", err)
}
driver.SetBlobs(blobStore)
```

**Step 2: Run the full test suite**

Run: `go test ./... 2>&1 | tail -20`
Expected: PASS

**Step 3: Commit**

```
feat: wire blob store initialization into application startup
```

---

### Task 13: End-to-end round-trip test

**Files:**
- Create: `internal/storage/blob/roundtrip_test.go`

**Step 1: Write the test**

```go
package blob

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/local"
)

func TestRoundTrip(t *testing.T) {
	bs, _ := local.New(t.TempDir())
	ctx := context.Background()

	original := strings.Repeat("large content ", 1000)
	draft := &storage.KnowledgeObject{
		RawContent:  original,
		ContentType: "text/plain",
		Source:      "test",
	}

	// Externalize.
	ext := steps.NewExternalizer(bs, 100)
	got, err := ext.Run(ctx, draft)
	if err != nil {
		t.Fatalf("externalize: %v", err)
	}

	if !IsBlobRef(got.RawContent) {
		t.Fatalf("expected blob ref, got %q", got.RawContent[:50])
	}

	// Resolve.
	rc, err := Resolve(ctx, bs, got)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	defer rc.Close()

	resolved, _ := io.ReadAll(rc)
	if !bytes.Equal(resolved, []byte(original)) {
		t.Errorf("round-trip mismatch: got %d bytes, want %d", len(resolved), len(original))
	}
}
```

**Step 2: Run the test**

Run: `go test ./internal/storage/blob/ -run TestRoundTrip -v`
Expected: PASS

**Step 3: Commit**

```
test(blob): add end-to-end round-trip test for externalize + resolve
```

---

### Task 14: Update docs

**Files:**
- Modify: `docs/dpkms/storage.md` (document blob storage configuration)
- Modify: `docs/ctxt/schema-object.md` (document TextContent field and blob:// references)

**Step 1: Add blob storage section to storage.md**

Document: configuration options, backend selection, threshold behavior, blob:// reference format.

**Step 2: Update schema-object.md**

Document: TextContent field purpose, when it's populated, FTS behavior with blob references.

**Step 3: Commit**

```
docs: add blob storage configuration and TextContent field documentation
```

---

### Summary

| Task | Component | Files | Tests |
|------|-----------|-------|-------|
| 1 | BlobStore interface | storage.go, types.go, storage_test.go | Interface satisfaction |
| 2 | TextContent field | types.go | Existing tests pass |
| 3 | BlobConfig | config.go, config_test.go | Config defaults |
| 4 | Stub backend | blob/stub/store.go | Put/Get/Delete/List/URL |
| 5 | Local backend | blob/local/store.go | Full CRUD + sharding |
| 6 | Factory + wire driver | blob/factory.go, driver.go | Factory selection |
| 7 | SQLite migration | 004_text_content.sql, objects.go | Migration + CRUD |
| 8 | Resolve helper | blob/resolve.go | Inline + blob ref |
| 9 | Externalize step | steps/externalize.go | Threshold logic |
| 10 | Register in builtins | builtins.go, capabilities.go | Capability check |
| 11 | S3 backend | blob/s3/store.go | Key paths + validation |
| 12 | App wiring | cmd entry point | Full test suite |
| 13 | Round-trip test | blob/roundtrip_test.go | E2E externalize→resolve |
| 14 | Documentation | storage.md, schema-object.md | N/A |

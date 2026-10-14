package storagetest

import (
	"context"
	"sync"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// AgedBlobs is a driver whose blob store lists each blob as last
// written at the time Touch set for its key, or at the default time
// NewAgedBlobs was given. Blob ages decide what blob.DeleteObject,
// blob.UpdateObject and blob.Sweep may delete; a test sets them here
// instead of waiting out the grace period.
type AgedBlobs struct {
	storage.StorageDriver
	mu      sync.Mutex
	def     time.Time
	written map[string]time.Time
}

// NewAgedBlobs wraps drv; every blob lists as written at def until
// Touch says otherwise.
func NewAgedBlobs(drv storage.StorageDriver, def time.Time) *AgedBlobs {
	return &AgedBlobs{StorageDriver: drv, def: def, written: map[string]time.Time{}}
}

// Touch makes blob key list as written at at.
func (d *AgedBlobs) Touch(key string, at time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.written[key] = at
}

func (d *AgedBlobs) writtenAt(key string) time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	if at, ok := d.written[key]; ok {
		return at
	}
	return d.def
}

// Blobs returns the wrapped blob store with its listings re-aged.
func (d *AgedBlobs) Blobs() storage.BlobStore {
	inner := d.StorageDriver.Blobs()
	if inner == nil {
		return nil
	}
	return agedBlobStore{BlobStore: inner, d: d}
}

type agedBlobStore struct {
	storage.BlobStore
	d *AgedBlobs
}

func (s agedBlobStore) List(ctx context.Context, prefix string) ([]storage.BlobInfo, error) {
	items, err := s.BlobStore.List(ctx, prefix)
	for i := range items {
		items[i].UpdatedAt = s.d.writtenAt(items[i].Key)
	}
	return items, err
}

// countHookDriver is drv whose CountBlobRefs calls after(key) once it
// has read the count: a fixed point between a delete's reference count
// and its blob delete, where a test lands a concurrent write.
type countHookDriver struct {
	storage.StorageDriver
	after func(key string)
}

func (d countHookDriver) Objects() storage.ObjectStore {
	return countHookObjects{ObjectStore: d.StorageDriver.Objects(), after: d.after}
}

type countHookObjects struct {
	storage.ObjectStore
	after func(key string)
}

func (o countHookObjects) CountBlobRefs(ctx context.Context, key string) (int, error) {
	n, err := o.ObjectStore.CountBlobRefs(ctx, key)
	if err == nil {
		o.after(key)
	}
	return n, err
}

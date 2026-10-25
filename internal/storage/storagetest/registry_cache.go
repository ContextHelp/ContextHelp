package storagetest

import (
	"context"
	"errors"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// RegistryCacheConformance pins RegistryStore.GetCachedManifest on a
// registry with no cache entry: storage.ErrNotFound on every driver, so
// callers tell "not cached yet" from a failed read.
func RegistryCacheConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	_, err := drv.Registries().GetCachedManifest(context.Background(), "https://registry-cache-missing.example")
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("GetCachedManifest(missing) = %v, want storage.ErrNotFound", err)
	}
}

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

var errRegistryRead = errors.New("registry read failed")

// failingRegistries fails GetCachedManifest and counts CacheManifest.
type failingRegistries struct {
	storage.RegistryStore
	cached int
}

func (f *failingRegistries) GetCachedManifest(context.Context, string) (*storage.RegistryCache, error) {
	return nil, errRegistryRead
}

func (f *failingRegistries) CacheManifest(context.Context, *storage.RegistryCache) error {
	f.cached++
	return nil
}

type registriesDriver struct {
	storage.StorageDriver
	reg *failingRegistries
}

func (d registriesDriver) Registries() storage.RegistryStore { return d.reg }

// EnsureDefaultRegistry seeds only when the entry is missing: a failed
// read is returned and nothing is rewritten over it.
func TestEnsureDefaultRegistry_ReadErrorDoesNotReseed(t *testing.T) {
	base := storageutil.NewTestDriver(t)
	reg := &failingRegistries{RegistryStore: base.Registries()}
	drv := registriesDriver{StorageDriver: base, reg: reg}
	svc := New(drv, nil, nil, search.NewEngine(drv), "", nil)

	err := svc.EnsureDefaultRegistry(ownerCtx())
	if !errors.Is(err, errRegistryRead) {
		t.Errorf("err = %v, want the read error", err)
	}
	if reg.cached != 0 {
		t.Errorf("re-seeded %d times after a read error", reg.cached)
	}
}

// A missing entry is seeded.
func TestEnsureDefaultRegistry_SeedsWhenMissing(t *testing.T) {
	drv := storageutil.NewTestDriver(t)
	svc := New(drv, nil, nil, search.NewEngine(drv), "", nil)
	if err := svc.EnsureDefaultRegistry(ownerCtx()); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureDefaultRegistry(ownerCtx()); err != nil {
		t.Fatalf("second call: %v", err)
	}
}

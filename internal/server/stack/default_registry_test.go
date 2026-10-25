package stack

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// The daemon seeds the bundled default registry into its store, so a
// store it created carries it for the read-only commands that never
// write it themselves.
func TestBuild_SeedsDefaultRegistry(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	st, err := buildWithDriver(&config.Config{}, driver)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if _, err := driver.Registries().GetCachedManifest(context.Background(), registry.DefaultRegistryURL); err != nil {
		t.Fatalf("default registry not seeded: %v", err)
	}
}

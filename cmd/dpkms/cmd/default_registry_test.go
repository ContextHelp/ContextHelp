package cmd

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// dpkms serve seeds the bundled default registry into its store, so a
// store it created carries it for the read-only commands that never
// write it themselves.
func TestSeedDefaultRegistry(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	svc := service.NewWithOptions(driver, nil, nil, nil, "", nil, nil, config.Config{})
	if err := seedDefaultRegistry(context.Background(), svc); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.Registries().GetCachedManifest(context.Background(), registry.DefaultRegistryURL); err != nil {
		t.Fatalf("default registry not seeded: %v", err)
	}
}

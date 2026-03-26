package registry_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/registry"
)

func TestLoadDefaultManifest(t *testing.T) {
	m, err := registry.LoadDefaultManifest()
	if err != nil {
		t.Fatalf("LoadDefaultManifest: %v", err)
	}
	if m.Name == "" {
		t.Error("manifest name is empty")
	}
	if m.Version == "" {
		t.Error("manifest version is empty")
	}
	if len(m.Taxonomy) == 0 {
		t.Error("manifest taxonomy is empty")
	}
	// Verify a known namespace is present.
	found := false
	for _, e := range m.Taxonomy {
		if e.Namespace == "tech" {
			found = true
			if e.Labels["en"] == "" {
				t.Error("tech namespace has no 'en' label")
			}
			if e.Descriptions["en"] == "" {
				t.Error("tech namespace has no 'en' description")
			}
		}
	}
	if !found {
		t.Error("expected 'tech' namespace in default taxonomy")
	}
}

func TestDefaultRegistryCache(t *testing.T) {
	cache, err := registry.DefaultRegistryCache()
	if err != nil {
		t.Fatalf("DefaultRegistryCache: %v", err)
	}
	if cache.RegistryURL != registry.DefaultRegistryURL {
		t.Errorf("unexpected URL: %s", cache.RegistryURL)
	}
	if cache.Manifest == nil {
		t.Fatal("cache manifest is nil")
	}
	if cache.AutoUpdate {
		t.Error("default registry should not auto-update")
	}
}

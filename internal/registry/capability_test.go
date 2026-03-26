package registry_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func makeManifest(minClient string, entitySync, taxonomy, translations bool) *storage.RegistryManifest {
	return &storage.RegistryManifest{
		Name:             "test",
		Version:          "1.0.0",
		MinClientVersion: minClient,
		Capabilities: storage.RegistryCapabilities{
			EntitySync:   entitySync,
			Taxonomy:     taxonomy,
			Translations: translations,
		},
	}
}

func TestCheckCapabilities_NoWarnings(t *testing.T) {
	m := makeManifest("0.1.0", true, true, true)
	warns := registry.CheckCapabilities(m, "1.0.0", "entity_sync", "taxonomy", "translations")
	if len(warns) != 0 {
		t.Errorf("expected no warnings, got: %v", warns)
	}
}

func TestCheckCapabilities_VersionTooOld(t *testing.T) {
	m := makeManifest("2.0.0", true, true, true)
	warns := registry.CheckCapabilities(m, "1.5.0")
	if len(warns) != 1 || warns[0].Feature != "version" {
		t.Errorf("expected version warning, got: %v", warns)
	}
}

func TestCheckCapabilities_VersionExact(t *testing.T) {
	m := makeManifest("1.5.0", false, false, false)
	warns := registry.CheckCapabilities(m, "1.5.0")
	if len(warns) != 0 {
		t.Errorf("exact version match should produce no version warning, got: %v", warns)
	}
}

func TestCheckCapabilities_DevVersionSkipped(t *testing.T) {
	m := makeManifest("99.0.0", true, true, true)
	warns := registry.CheckCapabilities(m, "dev")
	// dev version skips version check
	if len(warns) != 0 {
		t.Errorf("'dev' version should skip version check, got: %v", warns)
	}
}

func TestCheckCapabilities_MissingFeatures(t *testing.T) {
	m := makeManifest("", false, false, false)
	warns := registry.CheckCapabilities(m, "1.0.0", "entity_sync", "taxonomy", "translations")
	if len(warns) != 3 {
		t.Errorf("expected 3 feature warnings, got %d: %v", len(warns), warns)
	}
	features := map[string]bool{}
	for _, w := range warns {
		features[w.Feature] = true
	}
	for _, f := range []string{"entity_sync", "taxonomy", "translations"} {
		if !features[f] {
			t.Errorf("expected warning for feature %q", f)
		}
	}
}

func TestCheckCapabilities_NilManifest(t *testing.T) {
	warns := registry.CheckCapabilities(nil, "1.0.0", "entity_sync")
	if warns != nil {
		t.Errorf("nil manifest should return nil warnings, got: %v", warns)
	}
}

package registry

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

//go:embed assets/default.json
var defaultRegistryData []byte

// DefaultRegistryURL is the sentinel URL used to identify the bundled default registry.
const DefaultRegistryURL = "ctxt://default"

// LoadDefaultManifest parses the bundled default registry JSON.
func LoadDefaultManifest() (*storage.RegistryManifest, error) {
	var m storage.RegistryManifest
	if err := json.Unmarshal(defaultRegistryData, &m); err != nil {
		return nil, fmt.Errorf("parse default registry: %w", err)
	}
	return &m, nil
}

// DefaultRegistryCache returns a RegistryCache for the bundled default registry.
func DefaultRegistryCache() (*storage.RegistryCache, error) {
	m, err := LoadDefaultManifest()
	if err != nil {
		return nil, err
	}
	return &storage.RegistryCache{
		RegistryURL: DefaultRegistryURL,
		Manifest:    m,
		LastFetched: time.Time{}, // epoch → indicates bundled, not network-fetched
		AutoUpdate:  false,
	}, nil
}

package plugin

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	"gopkg.in/yaml.v3"
)

// LoadManifest reads and validates the manifest.yaml for a plugin
// located at pluginDir. Returns an error if the file is missing or invalid.
func LoadManifest(pluginDir string) (*pluginapi.PluginManifest, error) {
	path := filepath.Join(pluginDir, "manifest.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("plugin manifest: %w", err)
	}
	return ParseManifest(data)
}

// ParseManifest decodes and validates raw YAML manifest bytes.
func ParseManifest(data []byte) (*pluginapi.PluginManifest, error) {
	var m pluginapi.PluginManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("plugin manifest: yaml: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// CheckPermissionChange compares old and new manifests and returns any
// permissions that have been added. Callers should halt startup and prompt
// the user for confirmation when additions are detected.
func CheckPermissionChange(old, updated *pluginapi.PluginManifest) []string {
	if old == nil {
		return updated.Permissions
	}
	oldSet := make(map[string]bool, len(old.Permissions))
	for _, p := range old.Permissions {
		oldSet[p] = true
	}
	var added []string
	for _, p := range updated.Permissions {
		if !oldSet[p] {
			added = append(added, p)
		}
	}
	return added
}

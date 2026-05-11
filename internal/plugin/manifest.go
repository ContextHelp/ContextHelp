package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	"gopkg.in/yaml.v3"
)

// LoadManifest reads and validates the manifest.json or manifest.yaml for a plugin
// located at pluginDir. Returns an error if the file is missing or invalid.
func LoadManifest(pluginDir string) (*pluginapi.PluginManifest, error) {
	jsonPath := filepath.Join(pluginDir, "manifest.json")
	if data, err := os.ReadFile(jsonPath); err == nil {
		return ParseManifestJSON(data)
	}

	yamlPath := filepath.Join(pluginDir, "manifest.yaml")
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return nil, fmt.Errorf("plugin manifest not found (tried manifest.json and manifest.yaml): %w", err)
	}
	return ParseManifestYAML(data)
}

// ParseManifestYAML decodes and validates raw YAML manifest bytes.
func ParseManifestYAML(data []byte) (*pluginapi.PluginManifest, error) {
	var m pluginapi.PluginManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("plugin manifest: yaml: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// ParseManifestJSON decodes and validates raw JSON manifest bytes.
func ParseManifestJSON(data []byte) (*pluginapi.PluginManifest, error) {
	var m pluginapi.PluginManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("plugin manifest: json: %w", err)
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

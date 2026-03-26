// Package markdownexport provides an Obsidian-compatible Markdown output-generator plugin.
package markdownexport

import (
	"fmt"
	"os"
	"path/filepath"
)

// Config holds plugin settings sourced from config.yaml under plugins.markdown-export.
type Config struct {
	// Enabled controls whether the plugin is active (default true).
	Enabled bool
	// VaultPath is the default Obsidian vault directory for file writes.
	// Overridden per-call by OutputOptions.Destination when non-empty.
	VaultPath string
}

// DefaultConfig returns safe defaults.
func DefaultConfig() Config {
	return Config{
		Enabled:   true,
		VaultPath: filepath.Join(os.Getenv("HOME"), "Documents", "ObsidianVault"),
	}
}

// ConfigFromMap decodes a raw config map into Config.
func ConfigFromMap(m map[string]interface{}) (Config, error) {
	cfg := DefaultConfig()
	if m == nil {
		return cfg, nil
	}
	if v, ok := m["enabled"].(bool); ok {
		cfg.Enabled = v
	}
	if v, ok := m["vault_path"].(string); ok {
		if v == "" {
			return cfg, fmt.Errorf("markdown-export: vault_path must not be empty")
		}
		cfg.VaultPath = v
	}
	return cfg, nil
}

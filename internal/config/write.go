package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// WriteBack marshals cfg to YAML and atomically writes it to path.
// The write is atomic: it writes to <path>.tmp then renames.
func WriteBack(cfg *Config, path string) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("config: write tmp: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("config: rename: %w", err)
	}

	return nil
}

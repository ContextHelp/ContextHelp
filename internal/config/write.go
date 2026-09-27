package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// WriteBack marshals the whole of cfg to YAML and atomically writes it to
// path (writes <path>.tmp, then renames).
//
// A loaded Config is the merge of every layer (cascade files, -c overlays,
// env values, defaults), so WriteBack of one copies all of them into path.
// Commands that change one setting edit the target file's own layer with
// [EditLayer] instead. Loading config never writes a file.
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

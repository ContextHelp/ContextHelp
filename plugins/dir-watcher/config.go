// Package dirwatcher — configuration for the directory watcher plugin.
package dirwatcher

import (
	"fmt"
	"time"
)

// Config holds settings sourced from config.yaml under plugins.dir-watcher.
type Config struct {
	// Dir is the path to the directory to watch (required).
	Dir string
	// Interval is the polling interval (default 10s).
	Interval time.Duration
	// Extensions is an allowlist of file extensions to ingest (e.g. [".txt", ".md"]).
	// Empty list means all files are ingested.
	Extensions []string
	// MaxFileSizeBytes caps the file content read into RawContent (0 = 1 MiB).
	MaxFileSizeBytes int64
	// DeleteAfterIngest moves ingested files to a .ctxt-ingested/ subdir (default false).
	DeleteAfterIngest bool
}

// DefaultConfig returns safe defaults.
func DefaultConfig() Config {
	return Config{
		Interval:         10 * time.Second,
		MaxFileSizeBytes: 1 << 20, // 1 MiB
	}
}

// ConfigFromMap decodes a raw config map into Config.
func ConfigFromMap(m map[string]interface{}) (Config, error) {
	cfg := DefaultConfig()
	if m == nil {
		return cfg, nil
	}

	d, _ := m["dir"].(string)
	if d == "" {
		return cfg, fmt.Errorf("dir-watcher: config key 'dir' is required")
	}
	cfg.Dir = d

	if v, ok := m["interval"].(string); ok && v != "" {
		dur, err := time.ParseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("dir-watcher: invalid interval %q: %w", v, err)
		}
		cfg.Interval = dur
	}

	if v, ok := m["extensions"]; ok {
		switch tv := v.(type) {
		case []string:
			cfg.Extensions = tv
		case []interface{}:
			for _, item := range tv {
				s, ok := item.(string)
				if !ok {
					return cfg, fmt.Errorf("dir-watcher: extensions must be a list of strings")
				}
				cfg.Extensions = append(cfg.Extensions, s)
			}
		}
	}

	if v, ok := m["max_file_size_bytes"].(int); ok && v > 0 {
		cfg.MaxFileSizeBytes = int64(v)
	}

	if v, ok := m["delete_after_ingest"].(bool); ok {
		cfg.DeleteAfterIngest = v
	}

	return cfg, nil
}

// Package contentmonitor — configuration for the URL content change monitor plugin.
package contentmonitor

import (
	"fmt"
	"time"
)

// URLTarget describes a single URL to monitor.
type URLTarget struct {
	// URL is the endpoint to fetch (required).
	URL string
	// Label is a human-readable name used in events and object metadata.
	// Defaults to the URL.
	Label string
}

// Config holds settings sourced from config.yaml under plugins.content-monitor.
type Config struct {
	// Targets is the list of URLs to monitor.
	Targets []URLTarget
	// Interval is the polling interval (default 30m).
	Interval time.Duration
	// UserAgent overrides the HTTP User-Agent header.
	UserAgent string
	// TimeoutSeconds is the HTTP request timeout in seconds (default 30).
	TimeoutSeconds int
	// MaxBodyBytes caps the response body read per URL (default 512 KiB).
	MaxBodyBytes int64
}

// DefaultConfig returns safe defaults.
func DefaultConfig() Config {
	return Config{
		Interval:       30 * time.Minute,
		UserAgent:      "ctxt-plugin-content-monitor/1.0",
		TimeoutSeconds: 30,
		MaxBodyBytes:   512 << 10, // 512 KiB
	}
}

// ConfigFromMap decodes a raw config map into Config.
func ConfigFromMap(m map[string]interface{}) (Config, error) {
	cfg := DefaultConfig()
	if m == nil {
		return cfg, nil
	}

	// urls: ["https://..."] shorthand.
	if v, ok := m["urls"]; ok {
		switch tv := v.(type) {
		case []string:
			for _, u := range tv {
				cfg.Targets = append(cfg.Targets, URLTarget{URL: u, Label: u})
			}
		case []interface{}:
			for _, item := range tv {
				u, ok := item.(string)
				if !ok {
					return cfg, fmt.Errorf("content-monitor: urls must be a list of strings")
				}
				cfg.Targets = append(cfg.Targets, URLTarget{URL: u, Label: u})
			}
		}
	}

	// targets: [{url: ..., label: ...}] verbose form.
	if v, ok := m["targets"]; ok {
		if items, ok := v.([]interface{}); ok {
			for _, item := range items {
				entry, ok := item.(map[string]interface{})
				if !ok {
					return cfg, fmt.Errorf("content-monitor: each target must be an object")
				}
				u, _ := entry["url"].(string)
				if u == "" {
					return cfg, fmt.Errorf("content-monitor: target missing 'url'")
				}
				label, _ := entry["label"].(string)
				if label == "" {
					label = u
				}
				cfg.Targets = append(cfg.Targets, URLTarget{URL: u, Label: label})
			}
		}
	}

	if v, ok := m["interval"].(string); ok && v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("content-monitor: invalid interval %q: %w", v, err)
		}
		cfg.Interval = d
	}

	if v, ok := m["user_agent"].(string); ok && v != "" {
		cfg.UserAgent = v
	}

	if v, ok := m["timeout_seconds"].(int); ok && v > 0 {
		cfg.TimeoutSeconds = v
	}

	if v, ok := m["max_body_bytes"].(int); ok && v > 0 {
		cfg.MaxBodyBytes = int64(v)
	}

	return cfg, nil
}

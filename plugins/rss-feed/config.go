// Package rssfeed — configuration for the RSS/Atom feed ingestion plugin.
package rssfeed

import (
	"fmt"
	"time"
)

// Config holds settings sourced from config.yaml under plugins.rss-feed.
type Config struct {
	// Feeds is the list of RSS/Atom feed URLs to poll.
	Feeds []string
	// Interval is the polling interval (default 15m).
	Interval time.Duration
	// MaxItems caps items fetched per feed per poll cycle (0 = unlimited).
	MaxItems int
	// UserAgent overrides the HTTP User-Agent header.
	UserAgent string
	// TimeoutSeconds is the HTTP request timeout in seconds (default 30).
	TimeoutSeconds int
}

// DefaultConfig returns safe defaults.
func DefaultConfig() Config {
	return Config{
		Interval:       15 * time.Minute,
		MaxItems:       0,
		UserAgent:      "ctxt-plugin-rss-feed/1.0",
		TimeoutSeconds: 30,
	}
}

// ConfigFromMap decodes a raw config map into Config.
func ConfigFromMap(m map[string]interface{}) (Config, error) {
	cfg := DefaultConfig()
	if m == nil {
		return cfg, nil
	}

	if v, ok := m["feeds"]; ok {
		switch tv := v.(type) {
		case []string:
			cfg.Feeds = tv
		case []interface{}:
			for _, item := range tv {
				s, ok := item.(string)
				if !ok {
					return cfg, fmt.Errorf("rss-feed: feeds must be a list of strings")
				}
				cfg.Feeds = append(cfg.Feeds, s)
			}
		default:
			return cfg, fmt.Errorf("rss-feed: feeds must be a list of strings")
		}
	}

	if v, ok := m["interval"].(string); ok && v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("rss-feed: invalid interval %q: %w", v, err)
		}
		cfg.Interval = d
	}

	if v, ok := m["max_items"].(int); ok {
		cfg.MaxItems = v
	}

	if v, ok := m["user_agent"].(string); ok && v != "" {
		cfg.UserAgent = v
	}

	if v, ok := m["timeout_seconds"].(int); ok && v > 0 {
		cfg.TimeoutSeconds = v
	}

	return cfg, nil
}

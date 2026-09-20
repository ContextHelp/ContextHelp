// Package ica — configuration for the ICA integration plugin.
package ica

import (
	"fmt"
	"net/url"
)

// Config holds settings sourced from config.yaml under plugins.ica.
type Config struct {
	// APIURL is the base URL of the ICA API server.
	APIURL string `json:"api_url"`
	// ProcessorURL is the base URL of the ICA processor service.
	ProcessorURL string `json:"processor_url"`
	// DefaultFeedPipeline enables ICA as the default feed pipeline.
	DefaultFeedPipeline bool `json:"default_feed_pipeline"`
	// DistChannelsAsMentions maps distribution channels to mentions.
	DistChannelsAsMentions bool `json:"dist_channels_as_mentions"`
}

// DefaultConfig returns safe defaults.
func DefaultConfig() Config {
	return Config{}
}

// ConfigFromMap decodes a raw config map into Config.
func ConfigFromMap(m map[string]interface{}) (Config, error) {
	cfg := DefaultConfig()
	if m == nil {
		return cfg, nil
	}

	if v, ok := m["api_url"].(string); ok && v != "" {
		if _, err := url.ParseRequestURI(v); err != nil {
			return cfg, fmt.Errorf("ica: invalid api_url %q: %w", v, err)
		}
		cfg.APIURL = v
	}

	if v, ok := m["processor_url"].(string); ok && v != "" {
		cfg.ProcessorURL = v
	}

	if v, ok := m["default_feed_pipeline"].(bool); ok {
		cfg.DefaultFeedPipeline = v
	}

	if v, ok := m["dist_channels_as_mentions"].(bool); ok {
		cfg.DistChannelsAsMentions = v
	}

	return cfg, nil
}

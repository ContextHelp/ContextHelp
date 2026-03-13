package autosuggest

import "fmt"

// AutoSuggestConfig controls plugin behaviour.
type AutoSuggestConfig struct {
	// Mode is "select" (default, stores pending for human approval) or "generate" (auto-applies).
	Mode string
	// MaxTags is the maximum number of tag suggestions to produce (default 5).
	MaxTags int
	// MaxMentions is the maximum number of @mention suggestions to produce (default 3).
	MaxMentions int
	// VocabularyHint is a list of preferred tags to bias the LLM toward.
	VocabularyHint []string
	// Enabled controls whether the step runs at all (default true).
	Enabled bool
}

// DefaultConfig returns a config with safe defaults.
func DefaultConfig() AutoSuggestConfig {
	return AutoSuggestConfig{
		Mode:        "select",
		MaxTags:     5,
		MaxMentions: 3,
		Enabled:     true,
	}
}

// ConfigFromMap decodes a raw map (from PluginConfig.Config) into AutoSuggestConfig.
func ConfigFromMap(m map[string]interface{}) (AutoSuggestConfig, error) {
	cfg := DefaultConfig()
	if m == nil {
		return cfg, nil
	}
	if v, ok := m["mode"].(string); ok {
		if v != "select" && v != "generate" {
			return cfg, fmt.Errorf("autosuggest: mode must be 'select' or 'generate', got %q", v)
		}
		cfg.Mode = v
	}
	if v, ok := m["max_tags"].(int); ok {
		cfg.MaxTags = v
	}
	if v, ok := m["max_mentions"].(int); ok {
		cfg.MaxMentions = v
	}
	if v, ok := m["enabled"].(bool); ok {
		cfg.Enabled = v
	}
	if raw, ok := m["vocabulary_hint"].([]interface{}); ok {
		for _, item := range raw {
			if s, ok := item.(string); ok {
				cfg.VocabularyHint = append(cfg.VocabularyHint, s)
			}
		}
	}
	return cfg, nil
}

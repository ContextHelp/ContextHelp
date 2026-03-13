package aliasing

import "fmt"

// AliasConfig controls plugin behaviour.
type AliasConfig struct {
	// Enabled controls whether the plugin is active (default true).
	Enabled bool
	// DefaultScope is the scope used when none is specified in CLI/API: "global" or "profile".
	DefaultScope string
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() AliasConfig {
	return AliasConfig{
		Enabled:      true,
		DefaultScope: "global",
	}
}

// ConfigFromMap decodes a raw config map into AliasConfig.
func ConfigFromMap(m map[string]interface{}) (AliasConfig, error) {
	cfg := DefaultConfig()
	if m == nil {
		return cfg, nil
	}
	if v, ok := m["enabled"].(bool); ok {
		cfg.Enabled = v
	}
	if v, ok := m["default_scope"].(string); ok {
		if v != "global" && v != "profile" {
			return cfg, fmt.Errorf("aliasing: default_scope must be 'global' or 'profile', got %q", v)
		}
		cfg.DefaultScope = v
	}
	return cfg, nil
}

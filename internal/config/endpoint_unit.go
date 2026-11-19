package config

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
	kitconfig "hop.top/kit/go/core/config"
)

// endpointKeys are the server keys that form one client endpoint unit
// (ADR-077 §3, "Config layers"): the unit comes entirely from the most
// specific file layer that sets any of them.
var endpointKeys = []string{"url", "urls", "token"}

// endpointLayer returns the most specific file among layers (least to
// most specific) that sets server.url, server.urls or server.token, or
// "" when none does. Missing files are skipped.
func endpointLayer(layers []string) (string, error) {
	winner := ""
	for _, path := range layers {
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("read config %s: %w", path, err)
		}
		var doc struct {
			Server map[string]yaml.Node `yaml:"server"`
		}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			// Load itself reports a malformed file; a layer that does
			// not parse here cannot own the unit.
			continue
		}
		for _, k := range endpointKeys {
			if _, ok := doc.Server[k]; ok {
				winner = path
				break
			}
		}
	}
	return winner, nil
}

// applyEndpointUnit replaces cfg's server.url, server.urls and
// server.token with the values of the one file that owns the unit, so a
// less specific layer's endpoint keys never mix into a more specific
// layer's. Env and overrides are re-applied afterwards by the caller.
func applyEndpointUnit(cfg *Config, owner string) error {
	var only Config
	if err := kitconfig.Load(&only, kitconfig.Options{ExtraConfigPaths: []string{owner}}); err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	cfg.Server.URL = only.Server.URL
	cfg.Server.URLs = only.Server.URLs
	cfg.Server.Token = only.Server.Token
	return nil
}

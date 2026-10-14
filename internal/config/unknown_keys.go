package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// LayerFiles returns the config files LoadWithOverrides(bin, cfgFile,
// extraPaths, …) merges, lowest precedence first. A cascade slot whose
// file does not exist is left out, as the loader skips it.
func LayerFiles(bin, cfgFile string, extraPaths []string) []string {
	layers := layerSources(bin, cfgFile, extraPaths)
	files := make([]string, len(layers))
	for i, l := range layers {
		files[i] = l.Path
	}
	return files
}

// UnknownKeys decodes the config file at path strictly and returns one
// message per key Config does not declare, such as "line 3: field
// bogus not found in type config.StorageConfig". Load ignores those
// keys, so a typo silently leaves the setting at its default.
func UnknownKeys(path string) ([]string, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- a config layer the loader already read
	if err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var cfg Config
	err = dec.Decode(&cfg)
	if err == nil || errors.Is(err, io.EOF) {
		return nil, nil
	}
	var te *yaml.TypeError
	if !errors.As(err, &te) {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	var unknown []string
	for _, msg := range te.Errors {
		if strings.Contains(msg, " not found in type ") {
			unknown = append(unknown, msg)
		}
	}
	return unknown, nil
}

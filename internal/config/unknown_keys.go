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

// ignoredKeys are top-level keys Config declares but no code reads, with
// the reason `config validate` gives for each.
var ignoredKeys = map[string]string{
	"plugins": "plugins are not loaded at runtime yet; nothing reads this key",
}

// IgnoredKeys returns one message per top-level key in the config file at
// path that Load parses but no code reads, such as "line 3: plugins:
// plugins are not loaded at runtime yet; nothing reads this key". A key
// set to null still counts: the file names it.
func IgnoredKeys(path string) ([]string, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- a config layer the loader already read
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, nil
	}
	top := doc.Content[0].Content
	var out []string
	for i := 0; i+1 < len(top); i += 2 {
		k := top[i]
		if reason, ok := ignoredKeys[k.Value]; ok {
			out = append(out, fmt.Sprintf("line %d: %s: %s", k.Line, k.Value, reason))
		}
	}
	return out, nil
}

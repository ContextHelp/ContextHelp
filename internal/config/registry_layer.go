package config

import (
	"os"
	"strings"
)

// LayerKind names the slot a config file fills in LoadWithOverrides'
// merge order.
type LayerKind string

// Config layer slots, lowest precedence first. LayerExtra is a file
// named with -c/--config (or the legacy single-file override).
const (
	LayerSystem  LayerKind = "system"
	LayerUser    LayerKind = "user"
	LayerProject LayerKind = "project"
	LayerExtra   LayerKind = "extra"
)

// LayerSource is one config file and the slot it fills.
type LayerSource struct {
	Path string
	Kind LayerKind
}

// RegistriesSource returns the config file whose `registries` list is the
// one LoadWithOverrides(bin, cfgFile, extraPaths, …) ends up with. A file
// layer replaces the whole list (yaml decodes a sequence fresh, it does
// not merge entries), so it is the highest-precedence file that sets
// `registries` at all. found is false when no file sets it.
//
// A LayerProject source is a `.contexthelp/<bin>.yaml` found walking up
// from the working directory: a cloned repository can put one there, so
// a registry url read from it is not the user's own choice.
func RegistriesSource(bin, cfgFile string, extraPaths []string) (src LayerSource, found bool, err error) {
	layers := layerSources(bin, cfgFile, extraPaths)
	for i := len(layers) - 1; i >= 0; i-- {
		l, err := OpenLayer(layers[i].Path)
		if err != nil {
			return LayerSource{}, false, err
		}
		if l.Has("registries") {
			return layers[i], true, nil
		}
	}
	return LayerSource{}, false, nil
}

// ProjectLayerSets reports whether the project config layer
// (`.contexthelp/<bin>.yaml`) participating in LoadWithOverrides(bin, cfgFile,
// extraPaths, ...) sets dotted key (for example "daemon" or "jobs.pod").
// Unlike RegistriesSource, it inspects the project layer directly even when a
// higher layer shadows the key, so a cloned repository cannot inject daemon or
// pod-execution settings.
func ProjectLayerSets(bin, cfgFile string, extraPaths []string, key string) (path string, found bool, err error) {
	for _, src := range layerSources(bin, cfgFile, extraPaths) {
		if src.Kind != LayerProject {
			continue
		}
		l, err := OpenLayer(src.Path)
		if err != nil {
			return "", false, err
		}
		if l.Has(strings.Split(key, ".")...) {
			return src.Path, true, nil
		}
	}
	return "", false, nil
}

// layerSources lists the files LoadWithOverrides(bin, cfgFile,
// extraPaths, …) merges, lowest precedence first. A cascade slot whose
// file does not exist is left out, as the loader skips it.
func layerSources(bin, cfgFile string, extraPaths []string) []LayerSource {
	system, user, project, extra := layerSlots(bin, cfgFile, extraPaths)
	layers := make([]LayerSource, 0, 3+len(extra))
	for _, l := range []LayerSource{
		{Path: system, Kind: LayerSystem},
		{Path: user, Kind: LayerUser},
		{Path: project, Kind: LayerProject},
	} {
		if l.Path == "" {
			continue
		}
		if _, err := os.Stat(l.Path); err == nil {
			layers = append(layers, l)
		}
	}
	for _, p := range extra {
		layers = append(layers, LayerSource{Path: p, Kind: LayerExtra})
	}
	return layers
}

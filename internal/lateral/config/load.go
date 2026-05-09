package config

import (
	kitconfig "hop.top/kit/go/core/config"
)

// LoadOptions mirrors kit/core/config.Options for the slots lateral uses.
// Pass empty paths to skip a layer; the kit loader silently skips missing
// files in the System/User/Project slots (those are conventional locations,
// not user assertions). Missing files in ExtraConfigPaths are treated as
// errors per kit semantics (those are explicit -c <path> requests).
type LoadOptions struct {
	SystemConfigPath  string
	UserConfigPath    string
	ProjectConfigPath string
	ExtraConfigPaths  []string
	EnvPrefix         string // e.g. "CTXT_LATERAL"
}

// Load resolves the layered config into a typed Config. Defaults seed the
// result before any file layer is read; later layers overwrite earlier ones
// per kit semantics (project > user > system, then extras in order).
func Load(opts LoadOptions) (Config, error) {
	defaults := Defaults()
	cfg := Config{}
	err := kitconfig.Load(&cfg, kitconfig.Options{
		Defaults:          &defaults,
		SystemConfigPath:  opts.SystemConfigPath,
		UserConfigPath:    opts.UserConfigPath,
		ProjectConfigPath: opts.ProjectConfigPath,
		ExtraConfigPaths:  opts.ExtraConfigPaths,
		EnvPrefix:         opts.EnvPrefix,
	})
	return cfg, err
}

// EffectiveForStrategy is Load with strategyOverridePath appended as the
// highest-priority extra layer. Use this to apply per-strategy overrides
// without hand-rolling a deep-merge — kit's loader does the right merge
// (scalars and slices replaced, maps merged).
func EffectiveForStrategy(opts LoadOptions, strategyOverridePath string) (Config, error) {
	if strategyOverridePath != "" {
		opts.ExtraConfigPaths = append(opts.ExtraConfigPaths, strategyOverridePath)
	}
	return Load(opts)
}

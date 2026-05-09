package config

import (
	"context"
	"fmt"
	"syscall"

	kitconfig "hop.top/kit/go/core/config"
)

// NewReloadable builds a Reloadable[Config] seeded from Load(opts). The
// caller-facing entry point for daemons that want to subscribe to SIGHUP
// reloads. Pass the result to WatchSignal to start listening.
//
// Mutable subset (re-applied on reload): Scoring (entire), Lifecycle.cold_cycle_days,
// Lifecycle.soft_delete_days. Immutable (vetoed on reload): Jobs (entire),
// Lifecycle.p3_reference_threshold. Veto surfaces as kit's
// config.ErrImmutableChanged on the kit.config.snapshot.reload_failed bus
// topic; the in-memory snapshot retains the prior value.
func NewReloadable(opts LoadOptions) (*kitconfig.Reloadable[Config], error) {
	initial, err := Load(opts)
	if err != nil {
		return nil, fmt.Errorf("initial load: %w", err)
	}
	defaults := Defaults()
	kitOpts := kitconfig.Options{
		Defaults:          &defaults,
		SystemConfigPath:  opts.SystemConfigPath,
		UserConfigPath:    opts.UserConfigPath,
		ProjectConfigPath: opts.ProjectConfigPath,
		ExtraConfigPaths:  opts.ExtraConfigPaths,
		EnvPrefix:         opts.EnvPrefix,
	}
	return kitconfig.New(&initial, kitOpts), nil
}

// WatchSignal delegates to kit's signal-driven reload loop. Listens for
// SIGHUP. Returns when ctx is cancelled or kit's loop errors.
//
// Reload swap is atomic: callers reading via r.Snapshot() never see a
// partially-applied config. Reload errors (including immutable-field
// vetoes) surface as bus events on kit.config.snapshot.reload_failed,
// not as a return value — the watcher keeps running so the next SIGHUP
// is still honored.
func WatchSignal(ctx context.Context, r *kitconfig.Reloadable[Config]) error {
	return r.WatchSignal(ctx, syscall.SIGHUP)
}

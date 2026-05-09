// Package config defines the lateral-discovery configuration model and
// loads it via hop.top/kit/go/core/config (layered: system → user →
// project → extra → env-prefix overlay → CLI overrides).
//
// # Reload contract
//
// Runtime-mutable (reloadable via SIGHUP without process restart):
//   - Scoring (entire — weights, threshold, cap_k)
//   - Lifecycle.ColdCycleDays
//   - Lifecycle.SoftDeleteDays
//
// Static (always restart-required, vetoed on reload):
//   - Jobs (engine_kind, sqlite_path — engine choice can't hot-swap)
//   - Lifecycle.P3ReferenceThreshold (structural promotion decision)
//
// Wire the reloadable variant via NewReloadable + WatchSignal. The
// non-reloadable Load entry point is fine for one-shot tools and tests.
package config

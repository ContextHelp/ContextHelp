// Package daemon hosts the testable wiring that the cmd/ctxt lateral
// subcommand drives. cmd/ctxt/cmd/lateral.go owns the cobra surface
// and threads flags into Options; this package owns the logic that
// loads config, builds the strategy registry, wires kit collaborators
// (bus, breaker, job engine), and drives the cold-cycle reaper poller.
//
// The split exists for one practical reason: the cmd/ctxt/cmd test
// package fails to load (kit/cli double-registers --config in init),
// which makes any tests sitting in cmd/ctxt/cmd unrunnable. The cmd
// surface composes daemon primitives (LoadConfig, Build, NewLifecycle,
// Lifecycle.Run) directly, so all behaviour stays test-covered here.
//
// Lifecycle: (*Lifecycle).Run is blocking. It returns when ctx is
// cancelled or a fatal startup error occurs. SIGHUP-driven config
// reload is internal — observers see kit.config.snapshot.reload_failed
// bus events on veto, not a Run() error.
//
// Package layout:
//   - package skeleton + sentinel
//   - Options + config loader
//   - per-adapter packages under internal/lateral/adapters
//   - registry wiring
//   - ScanFunc + cold-cycle integration
//   - in-process smoke test (this package)
package daemon

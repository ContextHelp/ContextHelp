// Package daemon hosts the testable wiring that the cmd/ctxt lateral
// subcommand drives. cmd/ctxt/cmd/lateral.go owns the cobra surface
// and threads flags into Options; this package owns the logic that
// loads config, builds the strategy registry, wires kit collaborators
// (bus, breaker, job engine), and drives the cold-cycle reaper poller.
//
// The split exists for one practical reason: the cmd/ctxt/cmd test
// package fails to load (kit/cli double-registers --config in init),
// which makes any tests sitting in cmd/ctxt/cmd unrunnable. The daemon
// package is consumed via a thin Run() shim from cmd/ctxt/cmd, so all
// behaviour stays test-covered here.
//
// Lifecycle: daemon.Run is blocking. It returns when ctx is cancelled
// or a fatal startup error occurs. SIGHUP-driven config reload is
// internal — observers see kit.config.snapshot.reload_failed bus
// events on veto, not a Run() error.
//
// Tasks T-0312..T-0321 land here incrementally:
//   - T-0312: package skeleton + sentinel
//   - T-0313: Options + config loader
//   - T-0314..T-0319: per-adapter packages under internal/lateral/adapters
//   - T-0320: registry wiring
//   - T-0321: ScanFunc + cold-cycle integration
//   - T-0322: in-process smoke test (this package)
package daemon

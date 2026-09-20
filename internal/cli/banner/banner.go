// Package banner injects a one-line upgrade banner into ctxt/dpkms CLI
// commands while a dpkms upgrade is in flight (ADR-070 §5, T-0580).
//
// The banner reads the local shadow file at
// $XDG_DATA_HOME/contexthelp/run/upgrade-state.json — written by the
// upgrade.Manager on the daemon side — so each CLI invocation pays only a
// single os.Stat + small read. No HTTP round-trip is in the hot path.
//
// The banner is a no-op when the shadow file is absent, malformed, or
// older than upgrade.ShadowStaleAfter (guards against a daemon that died
// mid-upgrade leaving a stranded file behind).
package banner

import (
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// shadowFileName is the basename of the shadow file under config.RunDir().
// Kept private so the daemon and CLI never disagree on path semantics.
const shadowFileName = "upgrade-state.json"

// ShadowPath returns the canonical shadow-file path under the user's
// run-dir. Empty string + nil error is impossible — RunDir errors propagate.
func ShadowPath() (string, error) {
	dir, err := config.RunDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, shadowFileName), nil
}

// Inject writes the upgrade banner to stderr when an in-flight upgrade is
// reflected in the local shadow file. No-ops cleanly when the file is
// missing, stale, malformed, or unreachable — banner failure must NEVER
// interfere with the underlying CLI command's exit code.
//
// Format (matches ADR-070 §5):
//
//	ℹ ctxt: upgrading reingest_selective; 47/120 objects (39%); ETA 32s; details: ctxt upgrade status
func Inject(stderr io.Writer) error {
	path, err := ShadowPath()
	if err != nil {
		// Couldn't resolve the run dir — silently no-op. Banner is a
		// nice-to-have; surfacing this on every command would be noise,
		// and per the contract above it must never affect the exit code.
		return nil //nolint:nilerr // banner is advisory; must never affect the command's exit code
	}
	return injectFromPath(stderr, path, time.Now())
}

// injectFromPath is the testable inner: reads from path, treats now as
// "current time" for the staleness gate. Exposed only inside the package.
func injectFromPath(stderr io.Writer, path string, now time.Time) error {
	st, ok, err := upgrade.ReadShadowFresh(path, now)
	if err != nil {
		// A corrupt shadow file is the daemon's problem to clean up; do
		// not let it break the CLI. We choose to silently skip rather
		// than warn, mirroring the "banner is non-essential" contract.
		return nil //nolint:nilerr // banner is advisory; must never affect the command's exit code
	}
	if !ok {
		return nil
	}
	// Only render the banner for in-flight or failed runs. Any other
	// state (e.g. awaiting_consent reserved for T-0581+) renders too,
	// because the operator needs to act.
	if st.State == upgrade.StateIdle {
		return nil
	}

	line := Format(st)
	_, _ = fmt.Fprintln(stderr, line)
	return nil
}

// Format renders a Status into the canonical one-line banner. Exported so
// tests and any future TUI consumer can use the same renderer.
//
// Examples:
//
//	in_progress  → "ℹ ctxt: upgrading reingest_selective; 47/120 objects (39%); ETA 32s; details: ctxt upgrade status"
//	failed       → "✗ ctxt: upgrade failed (reingest_selective): disk full; details: ctxt upgrade status"
//	awaiting     → "ℹ ctxt: upgrade awaiting_consent (reingest_all); details: ctxt upgrade status"
func Format(st upgrade.Status) string {
	switch st.State {
	case upgrade.StateInProgress:
		pct := int(st.Progress*100 + 0.5)
		return fmt.Sprintf(
			"ℹ ctxt: upgrading %s; %d/%d objects (%d%%); ETA %ds; details: ctxt upgrade status",
			st.Bucket, st.Done, st.Total, pct, st.EtaSeconds,
		)
	case upgrade.StateFailed:
		return fmt.Sprintf(
			"✗ ctxt: upgrade failed (%s): %s; details: ctxt upgrade status",
			st.Bucket, st.LastError,
		)
	case upgrade.StateAwaitingConsent:
		return fmt.Sprintf(
			"ℹ ctxt: upgrade awaiting_consent (%s); details: ctxt upgrade status",
			st.Bucket,
		)
	default:
		return ""
	}
}

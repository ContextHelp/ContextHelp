// Package banner renders the one-line ADR-070 §5 upgrade banner that the
// ctxt and dpkms CLIs print on stderr while a dpkms upgrade is not idle.
//
// The state comes from the X-Dpkms-Upgrade header on dpkms API responses
// (upgrade.DecodeHeader): Arm hooks every response of one CLI invocation
// and prints the first state it sees, once. The banner follows the
// instance a command talks to, local or remote. Commands that never call
// the API print none.
package banner

import (
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

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

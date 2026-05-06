package integration

// US-0221: Meeting Auto-Detect Prompt (per ADR-069 + T-0517).
//
// E2E acceptance criteria from docs/stories/capture/US-0221-meeting-auto-detect-prompt.md.
// Verifies: foreground source feeds AutoDetect.OnForegroundChange; known
// meeting bundle-ids surface a prompt (default 10s timeout); auto-start
// rules skip the prompt; never rules suppress detection; CEL veto
// suppresses entirely; OS-native notification UI shown; per-app
// remember-my-choice persisted.
//
// **Status: skeleton.** AutoDetect logic is implemented + unit-tested
// (T-0517). E2E needs the daemon-driver wiring (cmd/ctxd connecting
// foreground events to AutoDetect.OnForegroundChange and AutoDetect's
// PromptRequest to OS notification UI per OS).

import (
	"testing"
)

func TestAutoDetect_PromptOnZoom(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: daemon-driver wiring + per-OS prompt UI " +
		"(UNUserNotification on macOS, Toast on Windows, notify-send on Linux)")
}

func TestAutoDetect_PromptTimeout(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: E2E harness — CheckPromptTimeout is unit-tested with " +
		"fakeClock; E2E with real-clock + real notification dismiss flow")
}

func TestAutoDetect_HotkeyConfirm(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: global hotkey listener registration (per OS) + Confirm " +
		"plumbing")
}

func TestAutoDetect_RememberAutoStart(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: kit/policy CEL rule generation + persistence " +
		"($XDG_CONFIG_HOME/contexthelp/policy.d/auto-record-*.cel files); the " +
		"auto-start path through AutoDetect.OnForegroundChange is unit-tested")
}

func TestAutoDetect_RememberSkip(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: same as above — CEL rule generation for 'never' action")
}

func TestAutoDetect_BrowserURLDetection(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: composition with browser-history source's last-recent-visit " +
		"(US-0221 acceptance: 'detect Google Meet in Chrome by checking " +
		"recent browser-history visit'); both substrates exist; wiring not yet built")
}

func TestAutoDetect_DisabledByDefault(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: E2E harness — config-driven enabled flag is unit-tested " +
		"in TestAutoDetect_DefaultsAreApplied")
}

func TestAutoDetect_VetoSuppressionInProfile(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: kit/policy CEL veto wiring at ctxt.ambient.meeting.auto_detected")
}

func TestAutoDetect_ExcludeBundlesTakesPrecedence(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: precedence rule (exclude_bundles overrides auto_detect.bundles) " +
		"needs cmd/ctxd config-merge logic that doesn't yet exist")
}

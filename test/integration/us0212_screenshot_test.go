package integration

// US-0212: Screenshot-on-Demand Source (re-grounded in ADR-066;
// rejects continuous screen capture per §Rationale 5).
//
// E2E acceptance criteria from docs/stories/capture/US-0212-screen-monitor.md.
// Verifies: hotkey + CLI triggered screenshot capture via OS-blessed APIs
// (ScreenCaptureKit / Graphics Capture / xdg-desktop-portal Screenshot),
// routing through image.ocr pipeline, perceptual-hash dedup, deny-list
// veto BEFORE OS permission prompt, schedule mode opt-in.
//
// **Status: skeleton.** Per-OS Capturer implementations (T-0504 follow-up)
// land separately; substrate (Source state machine + bus events) is shipped.
// E2E harness needs a real OS context to test capture.

import (
	"testing"
)

func TestScreenshot_HotkeyTrigger(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: per-OS Capturer (ScreenCaptureKit Swift bridge / Windows " +
		"Graphics Capture C++ bridge / xdg-desktop-portal D-Bus) + global " +
		"hotkey listener registration")
}

func TestScreenshot_CLITrigger(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: ctxt capture screenshot CLI subcommand (currently planned " +
		"in cmd/ctxt/cmd/capture_screenshot.go); substrate Source.Trigger ready")
}

func TestScreenshot_RoutesToImageOCR(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: E2E harness (start dpkms + ctxd, drive screenshot capture, " +
		"assert KnowledgeObject via image.ocr pipeline with non-empty OCR text)")
}

func TestScreenshot_BundleDenyList(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: per-OS Capturer (foreground bundle id detection at trigger " +
		"time); substrate-side ExcludeBundles check is implemented + tested")
}

func TestScreenshot_CELVeto(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: kit/policy CEL rule wiring at ctxt.ambient.event.captured")
}

func TestScreenshot_PerceptualHashDedup(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: per-OS Capturer must populate PerceptualHash (substrate " +
		"falls back to file-content SHA-256 today; image.ocr-fed pipeline " +
		"verifies dedup on the substrate side)")
}

func TestScreenshot_PermissionFlow(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: per-OS Capturer (must surface OS permission prompt on " +
		"first use; manual UI test, not automatable in CI)")
}

func TestScreenshot_MediaRetentionEviction(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: meeting/retention policy (T-0519) is implemented, but " +
		"screenshot media tier integration not wired; need shared retention " +
		"runner across meeting + screenshot media files")
}

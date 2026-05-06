package integration

// US-0215: Foreground Window / App Source (per ADR-066 + ADR-067 + T-0503).
//
// E2E acceptance criteria from docs/stories/capture/US-0215-foreground-window-source.md.
// Verifies: macOS AX-event subscription (AXFocusedWindowChanged,
// AXApplicationActivated), strictly bundle-id + window-title only (NOT
// AX tree, focused-element values, visible text, keystrokes), debounce
// of rapid focus-change bursts, ExcludeBundles deny-list. Linux X11/Wayland
// + Windows return ErrNotSupported (build-tagged).
//
// **Status: skeleton.** Substrate (T-0503) is implemented + unit-tested
// with a fakeReader. The Reader interface implementation for macOS lives
// behind GOOS=darwin in a follow-up commit (Swift bridge for AX events).

import (
	"testing"
)

func TestForeground_AXPermissionFlow(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: macOS AX Reader implementation (cgo + Swift bridge); " +
		"manual UI test for permission grant/deny flow")
}

func TestForeground_DebounceBursts(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: real OS focus-change events; substrate debounce is " +
		"unit-tested in TestSource_DebouncesRapidBursts")
}

func TestForeground_NoAXTreeCaptured(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: macOS AX Reader implementation. Critical privacy " +
		"assertion (RawEvent.Metadata never carries ax_tree / focused_element / " +
		"visible_text) is unit-tested in TestSource_EmitsRawEventOnFocusChange; " +
		"E2E verifies the Reader impl honors the substrate's narrow contract")
}

func TestForeground_BundleExcludeList(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: E2E harness — ExcludeBundles filter is unit-tested; " +
		"E2E asserts no bus event emits for excluded bundles")
}

func TestForeground_FeedsCutter(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: E2E harness — cutter integration is unit-tested via " +
		"the runner's stubCutter; E2E verifies the real session.Cutter " +
		"transitions correctly with real focus-change events")
}

func TestForeground_NotSupportedOnNonDarwin(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: build-tagged foreground_unsupported.go file; trivial to " +
		"unit-test once the per-OS files exist")
}

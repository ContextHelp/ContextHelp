package integration

// US-0214: Browser-History Ambient Source (per ADR-066 + T-0502).
//
// E2E acceptance criteria from docs/stories/capture/US-0214-browser-history-source.md.
// Verifies: per-browser SQLite-history poll (Chrome / Firefox / Safari /
// Edge / Brave / Arc), URL routing (github.com → url.repo, otherwise →
// url.generic), search-engine subtype tagging, allow/deny URL filters,
// last-seen timestamp persistence across daemon restarts, copy-to-temp
// avoiding lock contention with running browsers.
//
// **Status: skeleton.** Substrate (T-0502) is implemented + unit-tested
// with a fakeBrowserClient. Per-browser SQLite-poll BrowserClient
// implementations land separately behind build tags (per-OS path
// resolution: macOS / Linux / Windows differ).

import (
	"testing"
)

func TestBrowserHistory_PollsChromeSafely(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: ChromeBrowserClient implementation (per-OS SQLite-history " +
		"path resolution + copy-to-temp + visit-time column query). Reuses " +
		"existing internal/importer/chrome parsing helpers")
}

func TestBrowserHistory_DetectsNewVisits(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: real BrowserClient impl + a real browser writing to its " +
		"SQLite. Substrate-level new-visit detection is unit-tested via fakeBrowser")
}

func TestBrowserHistory_RestartResumesFromBookmark(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: SetLastSeen / LastSeen accessors are implemented and " +
		"unit-tested; full daemon-restart flow with on-disk last-seen state " +
		"needs the ctxd lifecycle harness")
}

func TestBrowserHistory_DenylistFiltersURLs(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: E2E harness — URLFilter is unit-tested; E2E verifies " +
		"ctxt.ambient.event.filtered emits to a real subscriber")
}

func TestBrowserHistory_RoutesRepoURLs(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: E2E harness — routing is unit-tested in " +
		"TestRoutePipeline; E2E asserts dpkms received the right pipeline name")
}

func TestBrowserHistory_HandlesBrowserLockContention(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: real BrowserClient (copy-to-temp must work while a real " +
		"browser holds the SQLite file's WAL lock)")
}

func TestBrowserHistory_MultiBrowserConcurrent(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: real BrowserClient impls for ≥2 browsers; substrate " +
		"concurrent-poll is unit-tested in TestSource_MultipleBrowsersConcurrent")
}

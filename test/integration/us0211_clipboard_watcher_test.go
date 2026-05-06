package integration

// US-0211: Passive Clipboard Watcher (re-grounded in ADR-066).
//
// E2E acceptance criteria from docs/stories/capture/US-0211-passive-clipboard-watcher.md.
// Verifies: ctxd's clipboard source emits RawEvents through the substrate Runner
// to a real dpkms over /api/v1/analyze, with substrate-level redaction, dedup,
// session tagging, and bus-event emission firing as specified.
//
// Test gate: INTEGRATION=1 env var required (running dpkms + ctxd).
//
// **Status: skeleton.** The named test functions below match the
// "planned" list in the user story document. Each currently calls
// t.Skip with a reason; the body is filled in when the corresponding
// implementation phase exercises this end-to-end. The skeletons exist
// so the acceptance criteria have a discoverable, executable home and
// `go test ./test/integration/...` reports the planned test surface
// even before the real assertions land.

import (
	"os"
	"testing"
)

func skipUnlessE2E(t *testing.T) {
	t.Helper()
	if os.Getenv("INTEGRATION") != "1" {
		t.Skip("integration test requires INTEGRATION=1 + running dpkms + ctxd")
	}
}

// TestClipboardWatcher_CapturesPlainText verifies that copying long-enough
// text to the OS clipboard produces a KnowledgeObject via the text.short
// pipeline with ambient_source=clipboard and a populated Fingerprint.
func TestClipboardWatcher_CapturesPlainText(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending implementation: substrate ready (T-0500); E2E harness " +
		"(start dpkms + ctxd, drive OS clipboard, assert KnowledgeObject) not built")
}

// TestClipboardWatcher_DedupeRapidUpdates verifies the substrate's
// fingerprint dedup at the enqueue boundary (T-0499) drops repeat copies
// of identical content within the configurable window.
func TestClipboardWatcher_DedupeRapidUpdates(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending implementation: substrate ready (T-0499 + T-0500); E2E " +
		"harness not built")
}

// TestClipboardWatcher_RespectsAppAllowlist verifies that the
// kit/runtime/policy CEL guard at ctxt.ambient.event.captured drops
// clipboard events while a configured deny-list bundle is foreground
// (e.g. com.1password.* per ADR-066).
func TestClipboardWatcher_RespectsAppAllowlist(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending implementation: kit/policy CEL wiring + foreground signal " +
		"plumbing into the clipboard event metadata")
}

// TestClipboardWatcher_PauseResume verifies the daemon-management
// commands (`ctxt capture --ambient stop` / start) cleanly pause + resume
// the source without losing buffered events.
func TestClipboardWatcher_PauseResume(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending implementation: ctxt capture --ambient stop/status CLI surface " +
		"(currently scaffolded, not wired to ctxd)")
}

// TestClipboardWatcher_RoutesToTextPipeline verifies the routing
// heuristics from ADR-066: URL → url.generic, code block → text.short
// (code subtype), long text → text.short.
func TestClipboardWatcher_RoutesToTextPipeline(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending implementation: substrate ready (T-0500); E2E harness " +
		"+ dpkms-side type-routing assertions not built")
}

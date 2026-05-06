package integration

// US-0213: File-Watch / Drop-Folder Source (per ADR-066 + T-0501).
//
// E2E acceptance criteria from docs/stories/capture/US-0213-file-watch-source.md.
// Verifies: fsnotify-driven watcher emits RawEvents for new files dropped
// into watched directories, routes by extension (.md → text.long, .png →
// image.ocr, .mp4 → video.full, etc.), respects max_file_size_mb, ignores
// hidden/system files, supports profile-scoped directories, moves processed
// files to <watch_dir>/processed/<YYYY-MM>/.
//
// **Status: skeleton.** Substrate (T-0501) is implemented + unit-tested.
// E2E harness needs a running dpkms to verify pipeline-side routing
// assertions.

import (
	"testing"
)

func TestFilewatch_RoutesByExtension(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: E2E harness (start dpkms + ctxd, drop sample files of each " +
		"supported extension into the watch dir, assert each KnowledgeObject " +
		"used the expected pipeline). Substrate routing is unit-tested in " +
		"internal/ambient/filewatch/filewatch_test.go::TestSource_RoutesByExtension")
}

func TestFilewatch_MovesToProcessed(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: daemon driver wiring — Source.MoveToProcessed is implemented " +
		"and unit-tested but isn't yet invoked by the Runner on enqueue success")
}

func TestFilewatch_RestartCatchUp(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: E2E harness — Source.initialScan is unit-tested but full " +
		"daemon-restart-with-pre-existing-files flow needs a real ctxd lifecycle")
}

func TestFilewatch_ProfileScopedDirs(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: profile-scoped directory configuration (per-directory " +
		"profile field documented in US-0213 acceptance; Config struct " +
		"currently treats all directories as global)")
}

func TestFilewatch_MaxFileSizeSkip(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: E2E harness — substrate-side cap is unit-tested in " +
		"TestSource_RejectsFilesOverSizeLimit; E2E verifies the bus-event " +
		"emission to a real subscriber")
}

func TestFilewatch_CELVetoOnPath(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: kit/policy CEL rule wiring at ctxt.ambient.event.captured")
}

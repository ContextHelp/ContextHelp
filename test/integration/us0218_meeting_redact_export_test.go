package integration

// US-0218: Meeting Redact + Export (per ADR-069 §6 + T-0518).
//
// E2E acceptance criteria from docs/stories/capture/US-0218-meeting-redact-export.md.
// Verifies: ctxt capture meeting redact uses supersede chain (new
// KnowledgeObject revision with segment removed, original marked
// superseded_by); media segment removal from local file (ffmpeg-style
// stream copy); S3 archive segment removal; export to md/srt/vtt
// formats honors redactions.
//
// **Status: skeleton.** Export + RedactSegmentRange + ParseTimeRange
// are implemented + unit-tested (T-0518). The dpkms-side supersede
// chain integration and the ffmpeg-driven media-segment removal are
// the deferred E2E pieces.

import (
	"testing"
)

func TestRedact_TranscriptSupersede(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: dpkms-side supersede chain wiring — RedactSegmentRange " +
		"is unit-tested at the slice level; E2E with a real meeting " +
		"KnowledgeObject + supersede_by link not yet built")
}

func TestRedact_MediaSegmentRemoval(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: ffmpeg-driven media-segment removal (stream copy where " +
		"possible, re-encode if needed). The substrate has the function shape " +
		"but no ffmpeg integration yet")
}

func TestRedact_S3ArchiveAlsoRedacted(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: S3-archive media + redact-from-archive is downstream of " +
		"both internal/ambient/buffer/s3 (events) and a separate media S3 " +
		"client (not yet wired)")
}

func TestRedact_MultipleRedactsExtendChain(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: dpkms-side supersede chain integration")
}

func TestRedact_AfterMediaEvicted(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: E2E harness — substrate-side warning behavior " +
		"(transcript-only redact when media is gone) needs the redact CLI + " +
		"dpkms-side integration before it can be exercised")
}

func TestExport_MarkdownFormat(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: E2E harness — Markdown formatting is unit-tested in " +
		"meeting/export_test.go::TestExport_MarkdownFormat; E2E asserts the " +
		"CLI command renders correctly against a real KnowledgeObject")
}

func TestExport_SRTFormat(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: E2E harness — SRT format is unit-tested; CLI integration " +
		"pending")
}

func TestExport_VTTFormat(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: E2E harness — VTT format is unit-tested; CLI integration " +
		"pending")
}

func TestExport_HonorsRedactions(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: end-to-end redact + export round-trip needs both pieces " +
		"of the supersede chain + export pipeline")
}

func TestAutoRedact_CELRuleOnPII(t *testing.T) {
	skipUnlessE2E(t)
	t.Skip("pending: kit/policy CEL invoke action wiring + ctxt.ambient.meeting." +
		"transcript_ready subscription. The redact engine itself is implemented")
}

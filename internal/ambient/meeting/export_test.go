package meeting

import (
	"strings"
	"testing"
	"time"
)

func sampleSegments() []TranscriptSegment {
	return []TranscriptSegment{
		{Speaker: "Alice", Start: 0, End: 5 * time.Second, Text: "Welcome everyone."},
		{Speaker: "Bob", Start: 5 * time.Second, End: 12 * time.Second, Text: "Thanks for joining."},
		{Speaker: "Alice", Start: 12 * time.Second, End: 20 * time.Second, Text: "Let's discuss Q3."},
	}
}

func TestExport_MarkdownFormat(t *testing.T) {
	t.Parallel()
	got, err := Export(sampleSegments(), ExportMarkdown)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if !strings.Contains(got, "**Alice** [00:00:00]: Welcome everyone.") {
		t.Errorf("markdown missing first segment: %q", got)
	}
	if !strings.Contains(got, "**Bob** [00:00:05]: Thanks for joining.") {
		t.Errorf("markdown missing second segment: %q", got)
	}
}

func TestExport_SRTFormat(t *testing.T) {
	t.Parallel()
	got, err := Export(sampleSegments(), ExportSRT)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if !strings.Contains(got, "1\n00:00:00,000 --> 00:00:05,000") {
		t.Errorf("SRT missing first cue: %q", got)
	}
	if !strings.Contains(got, "Alice: Welcome everyone.") {
		t.Errorf("SRT missing speaker prefix: %q", got)
	}
}

func TestExport_VTTFormat(t *testing.T) {
	t.Parallel()
	got, err := Export(sampleSegments(), ExportVTT)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if !strings.HasPrefix(got, "WEBVTT\n") {
		t.Errorf("VTT missing header: %q", got)
	}
	if !strings.Contains(got, "00:00:00.000 --> 00:00:05.000") {
		t.Errorf("VTT missing cue with period separator: %q", got)
	}
	if !strings.Contains(got, "<v Alice>") {
		t.Errorf("VTT missing speaker tag: %q", got)
	}
}

func TestExport_DefaultsToMarkdown(t *testing.T) {
	t.Parallel()
	got, _ := Export(sampleSegments(), "")
	if !strings.Contains(got, "**Alice**") {
		t.Errorf("empty format should default to markdown: %q", got)
	}
}

func TestExport_RejectsUnknownFormat(t *testing.T) {
	t.Parallel()
	if _, err := Export(sampleSegments(), "json"); err == nil {
		t.Error("unknown format: expected error")
	}
}

func TestRedactSegmentRange_RemovesOverlapping(t *testing.T) {
	t.Parallel()
	segments := sampleSegments()
	// Redact 4s-13s — overlaps both the first segment (ends at 5) and
	// the second (5-12) and the third (starts at 12).
	got := RedactSegmentRange(segments, 4*time.Second, 13*time.Second)
	if len(got) != 0 {
		t.Errorf("expected all 3 segments redacted; got %d remaining", len(got))
	}
}

func TestRedactSegmentRange_PreservesNonOverlapping(t *testing.T) {
	t.Parallel()
	segments := sampleSegments()
	// Redact only 5-12s — should remove only the middle segment.
	got := RedactSegmentRange(segments, 5*time.Second, 12*time.Second)
	if len(got) != 2 {
		t.Errorf("expected 2 segments remaining; got %d", len(got))
	}
	for _, s := range got {
		if s.Speaker == "Bob" && s.Text == "Thanks for joining." {
			t.Errorf("middle segment should have been redacted")
		}
	}
}

func TestRedactSegmentRange_NoOpForInvertedRange(t *testing.T) {
	t.Parallel()
	segments := sampleSegments()
	got := RedactSegmentRange(segments, 10*time.Second, 5*time.Second)
	if len(got) != len(segments) {
		t.Errorf("inverted range should be no-op; got %d/%d remaining", len(got), len(segments))
	}
}

func TestParseTimeRange_MMSSFormat(t *testing.T) {
	t.Parallel()
	start, end, err := ParseTimeRange("14:55-14:58")
	if err != nil {
		t.Fatalf("ParseTimeRange: %v", err)
	}
	if start != 14*time.Minute+55*time.Second {
		t.Errorf("start = %s, want 14m55s", start)
	}
	if end != 14*time.Minute+58*time.Second {
		t.Errorf("end = %s, want 14m58s", end)
	}
}

func TestParseTimeRange_HHMMSSFormat(t *testing.T) {
	t.Parallel()
	start, end, err := ParseTimeRange("01:14:55-01:15:00")
	if err != nil {
		t.Fatalf("ParseTimeRange: %v", err)
	}
	if start != time.Hour+14*time.Minute+55*time.Second {
		t.Errorf("start = %s, want 1h14m55s", start)
	}
	if end != time.Hour+15*time.Minute {
		t.Errorf("end = %s, want 1h15m", end)
	}
}

func TestParseTimeRange_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{
		"",
		"14:55",
		"14:55-",
		"14:55-13:00", // end before start
		"badformat",
	}
	for _, c := range cases {
		c := c
		t.Run(c, func(t *testing.T) {
			t.Parallel()
			if _, _, err := ParseTimeRange(c); err == nil {
				t.Errorf("ParseTimeRange(%q): expected error", c)
			}
		})
	}
}

func TestSortSegments_AscendingByStart(t *testing.T) {
	t.Parallel()
	segments := []TranscriptSegment{
		{Speaker: "C", Start: 20 * time.Second},
		{Speaker: "A", Start: 0},
		{Speaker: "B", Start: 10 * time.Second},
	}
	SortSegments(segments)
	if segments[0].Speaker != "A" || segments[1].Speaker != "B" || segments[2].Speaker != "C" {
		t.Errorf("SortSegments did not order ascending: %+v", segments)
	}
}

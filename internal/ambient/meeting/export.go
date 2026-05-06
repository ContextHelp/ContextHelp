package meeting

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// TranscriptSegment is one diarized segment from a meeting transcript.
// The audio.transcribe pipeline produces these as part of the
// KnowledgeObject's Sections.
type TranscriptSegment struct {
	Speaker string
	Start   time.Duration
	End     time.Duration
	Text    string
}

// ExportFormat is the on-disk format for transcript export.
type ExportFormat string

const (
	ExportMarkdown ExportFormat = "md"
	ExportSRT      ExportFormat = "srt"
	ExportVTT      ExportFormat = "vtt"
)

// Export renders segments to the supplied format. Used by
// `ctxt capture meeting export` and the auto-redaction CEL flow.
//
// Markdown: speaker-tagged blockquote-style transcript with [HH:MM:SS]
// timestamps. Suitable for pasting into Notion / Obsidian.
//
// SRT: standard subtitle format. Compatible with most video players.
//
// VTT: WebVTT — HTML5 <track>-compatible.
func Export(segments []TranscriptSegment, format ExportFormat) (string, error) {
	switch format {
	case ExportMarkdown, "":
		return exportMarkdown(segments), nil
	case ExportSRT:
		return exportSRT(segments), nil
	case ExportVTT:
		return exportVTT(segments), nil
	default:
		return "", fmt.Errorf("meeting export: unknown format %q", format)
	}
}

func exportMarkdown(segments []TranscriptSegment) string {
	var b strings.Builder
	for _, s := range segments {
		fmt.Fprintf(&b, "**%s** [%s]: %s\n\n",
			defaultIfEmpty(s.Speaker, "Unknown"),
			formatHMS(s.Start),
			s.Text,
		)
	}
	return b.String()
}

func exportSRT(segments []TranscriptSegment) string {
	var b strings.Builder
	for i, s := range segments {
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s: %s\n\n",
			i+1,
			formatSRTTimestamp(s.Start),
			formatSRTTimestamp(s.End),
			defaultIfEmpty(s.Speaker, "Speaker"),
			s.Text,
		)
	}
	return b.String()
}

func exportVTT(segments []TranscriptSegment) string {
	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	for i, s := range segments {
		fmt.Fprintf(&b, "%d\n%s --> %s\n<v %s>%s\n\n",
			i+1,
			formatVTTTimestamp(s.Start),
			formatVTTTimestamp(s.End),
			defaultIfEmpty(s.Speaker, "Speaker"),
			s.Text,
		)
	}
	return b.String()
}

// formatHMS returns "HH:MM:SS" — used by Markdown export for inline
// timestamps.
func formatHMS(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

// formatSRTTimestamp returns "HH:MM:SS,mmm" — SRT format.
func formatSRTTimestamp(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	ms := int(d.Milliseconds()) % 1000
	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, s, ms)
}

// formatVTTTimestamp returns "HH:MM:SS.mmm" — VTT format (period
// separator vs SRT's comma).
func formatVTTTimestamp(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	ms := int(d.Milliseconds()) % 1000
	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, ms)
}

func defaultIfEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// RedactSegmentRange filters segments, removing any whose [Start, End]
// overlaps the supplied [redactStart, redactEnd] range. Returns the
// filtered list.
//
// Per ADR-069 §6, redaction uses the supersede pattern: the original
// transcript is preserved with a superseded_by link to a new revision
// that has the segment removed. This function is the segment-level
// filter; the supersede chain itself is a dpkms-side concern (handled
// by the CLI redact command).
func RedactSegmentRange(segments []TranscriptSegment, redactStart, redactEnd time.Duration) []TranscriptSegment {
	if redactEnd <= redactStart {
		return segments
	}
	out := make([]TranscriptSegment, 0, len(segments))
	for _, s := range segments {
		// Overlaps if not entirely before redactStart and not entirely
		// after redactEnd.
		if s.End <= redactStart || s.Start >= redactEnd {
			out = append(out, s)
		}
	}
	return out
}

// ParseTimeRange parses a "HH:MM-HH:MM" or "MM:SS-MM:SS" range string
// into start/end durations. Used by `ctxt capture meeting redact --segment`.
func ParseTimeRange(spec string) (start, end time.Duration, err error) {
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, errors.New("expected HH:MM-HH:MM or MM:SS-MM:SS")
	}
	start, err = parseClockSpec(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("start: %w", err)
	}
	end, err = parseClockSpec(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, fmt.Errorf("end: %w", err)
	}
	if end <= start {
		return 0, 0, errors.New("end must be after start")
	}
	return start, end, nil
}

func parseClockSpec(s string) (time.Duration, error) {
	parts := strings.Split(s, ":")
	switch len(parts) {
	case 2:
		// MM:SS
		var m, sec int
		if _, err := fmt.Sscanf(s, "%d:%d", &m, &sec); err != nil {
			return 0, err
		}
		return time.Duration(m)*time.Minute + time.Duration(sec)*time.Second, nil
	case 3:
		// HH:MM:SS
		var h, m, sec int
		if _, err := fmt.Sscanf(s, "%d:%d:%d", &h, &m, &sec); err != nil {
			return 0, err
		}
		return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(sec)*time.Second, nil
	default:
		return 0, errors.New("expected MM:SS or HH:MM:SS")
	}
}

// SortSegments sorts segments by Start ascending.
func SortSegments(segments []TranscriptSegment) {
	sort.Slice(segments, func(i, j int) bool {
		return segments[i].Start < segments[j].Start
	})
}

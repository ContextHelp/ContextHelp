// Package contentmonitor — line-level diff utilities.
package contentmonitor

import (
	"strings"
)

// diffSummary produces a human-readable change summary between old and new
// content. It counts changed, added, and removed lines using a simple
// Myers-style split (no external dependency).
//
// Returns a short prose description and an approximate line-diff string
// suitable for embedding in KnowledgeObject.TextContent.
func diffSummary(oldContent, newContent string) (summary string, diff string) {
	oldLines := splitLines(oldContent)
	newLines := splitLines(newContent)

	oldSet := lineSet(oldLines)
	newSet := lineSet(newLines)

	var added, removed []string
	for _, l := range newLines {
		if _, ok := oldSet[l]; !ok {
			added = append(added, "+ "+l)
		}
	}
	for _, l := range oldLines {
		if _, ok := newSet[l]; !ok {
			removed = append(removed, "- "+l)
		}
	}

	parts := []string{}
	if len(added) > 0 {
		parts = append(parts, pluralise(len(added), "line")+" added")
	}
	if len(removed) > 0 {
		parts = append(parts, pluralise(len(removed), "line")+" removed")
	}
	if len(parts) == 0 {
		summary = "content updated (binary or whitespace change)"
	} else {
		summary = strings.Join(parts, "; ")
	}

	allChanges := append(removed, added...) //nolint:gocritic
	diff = strings.Join(allChanges, "\n")
	return summary, diff
}

func splitLines(s string) []string {
	return strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
}

// lineSet deduplicates lines for O(n) set membership.
func lineSet(lines []string) map[string]struct{} {
	m := make(map[string]struct{}, len(lines))
	for _, l := range lines {
		m[l] = struct{}{}
	}
	return m
}

func pluralise(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strings.Join([]string{itoa(n), " ", noun, "s"}, "")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	buf := make([]byte, 0, 10)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}

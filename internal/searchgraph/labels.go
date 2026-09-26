package searchgraph

import (
	"strings"
	"unicode/utf8"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// maxLabelRunes caps display labels; longer ones end in an ellipsis.
const maxLabelRunes = 120

// objectLabel picks an object's display label: metadata title, first
// summary, first line of text content, first line of raw content, then
// the id. Content is taken verbatim (no HTML escaping).
func objectLabel(id string, o *storage.KnowledgeObject) string {
	if o == nil {
		return id
	}
	if t, ok := o.Metadata["title"].(string); ok {
		if l := cleanLabel(t); l != "" {
			return l
		}
	}
	if len(o.Summaries) > 0 {
		if l := cleanLabel(o.Summaries[0]); l != "" {
			return l
		}
	}
	for _, body := range []string{o.TextContent, o.RawContent} {
		if l := cleanLabel(strings.TrimLeft(firstLine(body), "# \t")); l != "" {
			return l
		}
	}
	return id
}

// firstLine returns the first non-blank line of s.
func firstLine(s string) string {
	for line := range strings.Lines(s) {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

// cleanLabel makes s a single-line, valid UTF-8 label of at most
// maxLabelRunes runes.
func cleanLabel(s string) string {
	s = strings.TrimSpace(firstLine(strings.ToValidUTF8(s, "�")))
	if utf8.RuneCountInString(s) <= maxLabelRunes {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:maxLabelRunes-1])) + "…"
}

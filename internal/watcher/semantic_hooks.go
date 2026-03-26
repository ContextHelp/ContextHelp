package watcher

import (
	"regexp"
	"strings"
)

// MentionHint is an early-guess mention extracted from filenames or metadata.
// Pipelines remain authoritative; this is optimization only.
type MentionHint struct {
	Raw string // e.g. "@react.server-components"
}

// mentionRE matches @namespace.slug patterns (letters, digits, hyphens, dots).
var mentionRE = regexp.MustCompile(`@[a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?`)

// SemanticWatcherHooks performs lightweight pre-semantic analysis on a path
// or metadata string before a job is submitted to the pipeline.
//
// It detects inline @mention patterns and returns early-guess hints to attach
// to the job. Pipelines remain authoritative; these hints exist for
// optimisation only (e.g. pre-fetch entity metadata before full enrichment).
type SemanticWatcherHooks struct{}

// NewSemanticWatcherHooks returns a SemanticWatcherHooks.
func NewSemanticWatcherHooks() *SemanticWatcherHooks {
	return &SemanticWatcherHooks{}
}

// ExtractMentionHints scans text (filename, file metadata, or short snippet)
// for @namespace.slug patterns and returns de-duplicated hints.
// Only valid mentions containing at least one dot separator qualify, ensuring
// the pattern includes both namespace and slug components.
func (h *SemanticWatcherHooks) ExtractMentionHints(text string) []MentionHint {
	if text == "" {
		return nil
	}
	matches := mentionRE.FindAllString(text, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(matches))
	hints := make([]MentionHint, 0, len(matches))
	for _, m := range matches {
		// Require at least one dot: @namespace.slug
		if !strings.Contains(m, ".") {
			continue
		}
		if _, dup := seen[m]; dup {
			continue
		}
		seen[m] = struct{}{}
		hints = append(hints, MentionHint{Raw: m})
	}
	return hints
}

// OnFileDiscovered is called by the watcher before a job is enqueued.
// It returns mention hints extracted from the file path and any inline
// metadata (e.g. first-line YAML front-matter title).
// Callers may attach these as job metadata for pipeline acceleration.
func (h *SemanticWatcherHooks) OnFileDiscovered(absPath, metadata string) []MentionHint {
	combined := absPath + " " + metadata
	return h.ExtractMentionHints(combined)
}

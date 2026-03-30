package markdownexport

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	"github.com/ideacrafterslabs/ctxt/pkg/projection"
)

// Render converts obj into Obsidian-compatible Markdown with YAML frontmatter.
//
// Frontmatter fields: id, type, title, tags, entities (mentions), created, updated.
// Body: summary, sections, decisions, tasks, backlinks.
// Uses projection.ProjectDocument / ProjectIndex so graph-canonical KOs are
// handled correctly; falls back to flat fields when Graph is nil.
func Render(obj pluginapi.KnowledgeObject) ([]byte, error) {
	doc := projection.ProjectDocument(&obj)
	idx := projection.ProjectIndex(&obj)

	var b bytes.Buffer

	// ── frontmatter ──────────────────────────────────────────────────────────
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("id: %s\n", obj.ID))
	b.WriteString(fmt.Sprintf("type: %s\n", obj.Type))

	title := objectTitle(obj)
	b.WriteString(fmt.Sprintf("title: %q\n", title))

	if len(idx.Tags) > 0 {
		b.WriteString("tags:\n")
		for _, t := range idx.Tags {
			b.WriteString(fmt.Sprintf("  - %s\n", sanitiseTag(t.Label)))
		}
	}

	if len(idx.Mentions) > 0 {
		b.WriteString("entities:\n")
		for _, m := range idx.Mentions {
			b.WriteString(fmt.Sprintf("  - %s\n", m))
		}
	}

	b.WriteString(fmt.Sprintf("created: %s\n", obj.CreatedAt.UTC().Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("updated: %s\n", obj.UpdatedAt.UTC().Format(time.RFC3339)))
	if obj.Source != "" {
		b.WriteString(fmt.Sprintf("source: %q\n", obj.Source))
	}
	b.WriteString("---\n\n")

	// ── title heading ────────────────────────────────────────────────────────
	b.WriteString(fmt.Sprintf("# %s\n\n", title))

	// ── body / summary ───────────────────────────────────────────────────────
	if doc.Body != "" {
		b.WriteString(doc.Body)
		b.WriteString("\n\n")
	} else if len(obj.Summaries) > 0 && obj.Summaries[0] != "" {
		// flat-field fallback: summaries not in graph
		b.WriteString(obj.Summaries[0])
		b.WriteString("\n\n")
	}

	// ── sections ─────────────────────────────────────────────────────────────
	for _, s := range doc.Sections {
		if s.Title != "" {
			b.WriteString(fmt.Sprintf("## %s\n\n", s.Title))
		}
		if s.Content != "" {
			b.WriteString(s.Content)
			b.WriteString("\n\n")
		}
	}

	// ── decisions ────────────────────────────────────────────────────────────
	if len(obj.Decisions) > 0 {
		b.WriteString("## Decisions\n\n")
		for _, d := range obj.Decisions {
			b.WriteString(fmt.Sprintf("- **%s** (%s, %s)\n", d.Title, d.Status, d.Impact))
		}
		b.WriteString("\n")
	}

	// ── tasks ────────────────────────────────────────────────────────────────
	if len(obj.Tasks) > 0 {
		b.WriteString("## Tasks\n\n")
		for _, t := range obj.Tasks {
			checkbox := "[ ]"
			if strings.EqualFold(t.Status, "done") || strings.EqualFold(t.Status, "completed") {
				checkbox = "[x]"
			}
			b.WriteString(fmt.Sprintf("- %s %s\n", checkbox, t.Title))
		}
		b.WriteString("\n")
	}

	// ── backlinks (entity mentions as wiki-links) ─────────────────────────────
	if len(idx.Mentions) > 0 {
		b.WriteString("## Backlinks\n\n")
		for _, m := range idx.Mentions {
			b.WriteString(fmt.Sprintf("- [[%s]]\n", m))
		}
		b.WriteString("\n")
	}

	return b.Bytes(), nil
}

// objectTitle extracts a human-readable title for the object.
// Prefers metadata["title"], then projection body (graph-canonical path),
// then flat Summaries[0] (backward compat), then falls back to ID.
func objectTitle(obj pluginapi.KnowledgeObject) string {
	if v, ok := obj.Metadata["title"].(string); ok && v != "" {
		return v
	}
	// Graph-canonical path: use first line of FTSBody as title hint.
	if obj.Graph != nil && len(obj.Graph.Nodes) > 0 {
		idx := projection.ProjectIndex(&obj)
		if idx.FTSBody != "" {
			s := idx.FTSBody
			if nl := strings.IndexByte(s, '\n'); nl > 0 {
				s = s[:nl]
			}
			if len(s) > 80 {
				s = s[:77] + "..."
			}
			return s
		}
	}
	// Flat-field fallback: summaries not in graph.
	if len(obj.Summaries) > 0 && obj.Summaries[0] != "" {
		s := obj.Summaries[0]
		if len(s) > 80 {
			s = s[:77] + "..."
		}
		return s
	}
	return obj.ID
}

// sanitiseTag replaces whitespace with hyphens so tags are Obsidian-safe.
func sanitiseTag(label string) string {
	return strings.ReplaceAll(strings.TrimSpace(label), " ", "-")
}

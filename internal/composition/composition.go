// Package composition renders knowledge objects into a markdown
// composition (brief, plan, summary, draft). It is pure formatting over
// objects the caller already holds, so ctxt runs it on objects it read
// from dpkms and dpkms runs it in-process.
package composition

import (
	"fmt"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/citation"
	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Result is the structured output of WithCitations.
type Result struct {
	// Type is the requested composition type (brief, plan, summary, draft).
	Type string `json:"type"`
	// Content is the full markdown body including inline [ref:ID] markers
	// and an appended reference table.
	Content string `json:"content"`
	// Citations is the parsed list of inline citations found in Content.
	Citations []citation.Citation `json:"citations"`
	// SourceIDs are the IDs of every object used as input.
	SourceIDs []string `json:"source_ids"`
	// GeneratedAt records when the composition was produced.
	GeneratedAt time.Time `json:"generated_at"`
}

// Compose concatenates objects under a heading for compositionType: one
// section per object, its ID then its snippet.
func Compose(objects []*storage.KnowledgeObject, compositionType string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", compositionType)
	fmt.Fprintf(&b, "Generated from %d knowledge objects.\n\n", len(objects))

	for _, obj := range objects {
		fmt.Fprintf(&b, "## %s\n", obj.ID)
		if snippet := Snippet(obj); snippet != "" {
			b.WriteString(snippet)
			b.WriteString("\n\n")
		}
	}
	return b.String()
}

// WithCitations is Compose with an inline [ref:ID] marker after each
// object's snippet and an appended reference table.
//
// The markers are injected here. When an AI provider writes the body it
// should emit them itself; this layer then parses and validates them
// regardless of origin.
func WithCitations(objects []*storage.KnowledgeObject, compositionType string) *Result {
	cctx := citation.NewCompositionContext(objects)

	sourceIDs := make([]string, 0, len(objects))
	for _, obj := range objects {
		sourceIDs = append(sourceIDs, obj.ID)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", compositionType)
	fmt.Fprintf(&b, "Generated from %d knowledge objects.\n\n", len(objects))
	for _, obj := range objects {
		fmt.Fprintf(&b, "## %s\n", obj.ID)
		if snippet := Snippet(obj); snippet != "" {
			fmt.Fprintf(&b, "%s [ref:%s]\n\n", snippet, obj.ID)
		} else {
			fmt.Fprintf(&b, "[ref:%s]\n\n", obj.ID)
		}
	}
	body := b.String()

	citations := citation.ParseCitations(body)
	citation.EnrichCitationsWithEntities(citations, cctx.ObjectMap)
	refTable := citation.BuildReferenceTable(citations, cctx.ObjectMap)

	return &Result{
		Type:        compositionType,
		Content:     body + refTable,
		Citations:   citations,
		SourceIDs:   sourceIDs,
		GeneratedAt: time.Now(),
	}
}

// Snippet returns the best short text snippet for an object using
// projection helpers. Prefers graph-derived body; falls back to flat
// Summaries, then RawContent (truncated to 500 chars).
func Snippet(obj *storage.KnowledgeObject) string {
	doc := projection.ProjectDocument(obj)
	// Graph-canonical path: use first non-empty section content.
	if obj.Graph != nil && len(obj.Graph.Nodes) > 0 {
		if doc.Body != "" {
			if len(doc.Body) > 500 {
				return doc.Body[:500] + "..."
			}
			return doc.Body
		}
		for _, sec := range doc.Sections {
			if sec.Content != "" {
				if len(sec.Content) > 500 {
					return sec.Content[:500] + "..."
				}
				return sec.Content
			}
		}
	}
	// Flat-field fallback: Summaries first, then RawContent.
	if len(obj.Summaries) > 0 {
		return obj.Summaries[0]
	}
	if obj.RawContent != "" {
		if len(obj.RawContent) > 500 {
			return obj.RawContent[:500] + "..."
		}
		return obj.RawContent
	}
	return ""
}

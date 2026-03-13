// Package citation implements inline [ref:ID] citation parsing, validation,
// and reference table generation for compositions.
package citation

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Citation represents a single [ref:...] marker found in composition content.
type Citation struct {
	IDs      []string // Referenced knowledge object IDs
	Position int      // Byte offset of the opening '[' in the content
	Anchor   string   // Optional #anchor suffix
	Entities []string // @mention entities from cited objects (populated by Enrich)
}

// CompositionContext holds the objects available for citation during composition.
type CompositionContext struct {
	Objects   []*storage.KnowledgeObject
	ObjectMap map[string]*storage.KnowledgeObject
}

// NewCompositionContext builds a CompositionContext from a slice of objects.
func NewCompositionContext(objects []*storage.KnowledgeObject) *CompositionContext {
	m := make(map[string]*storage.KnowledgeObject, len(objects))
	for _, obj := range objects {
		m[obj.ID] = obj
	}
	return &CompositionContext{
		Objects:   objects,
		ObjectMap: m,
	}
}

// citationRe matches [ref:...] markers.
// The capture group captures everything between "ref:" and the closing "]".
var citationRe = regexp.MustCompile(`\[ref:([a-zA-Z0-9,\-#]+)\]`)

// ParseCitations scans content for [ref:ID], [ref:ID1,ID2], and [ref:ID#anchor]
// markers and returns a Citation for each one found.
func ParseCitations(content string) []Citation {
	matches := citationRe.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return nil
	}

	citations := make([]Citation, 0, len(matches))
	for _, m := range matches {
		refStr := content[m[2]:m[3]]
		c := Citation{Position: m[0]}

		// Split off optional #anchor.
		parts := strings.SplitN(refStr, "#", 2)
		if len(parts) == 2 {
			c.Anchor = parts[1]
		}

		// Split comma-separated IDs.
		rawIDs := strings.Split(parts[0], ",")
		c.IDs = make([]string, 0, len(rawIDs))
		for _, id := range rawIDs {
			id = strings.TrimSpace(id)
			if id != "" {
				c.IDs = append(c.IDs, id)
			}
		}

		citations = append(citations, c)
	}
	return citations
}

// ValidateCitations checks that every referenced ID exists in objectMap.
// Each unique missing ID produces one error regardless of how many times it
// is referenced.
func ValidateCitations(citations []Citation, objectMap map[string]*storage.KnowledgeObject) []error {
	seen := make(map[string]bool)
	var errs []error

	for _, c := range citations {
		for _, id := range c.IDs {
			if seen[id] {
				continue
			}
			seen[id] = true

			if _, exists := objectMap[id]; !exists {
				errs = append(errs, fmt.Errorf("invalid citation: object %s not found", id))
			}
		}
	}
	return errs
}

// BuildReferenceTable generates a markdown reference table for all cited objects.
// Citations to the same object are deduplicated; citations to missing objects are
// silently skipped.
func BuildReferenceTable(citations []Citation, objectMap map[string]*storage.KnowledgeObject) string {
	// Collect unique IDs preserving first-occurrence order.
	seen := make(map[string]bool)
	var ids []string
	for _, c := range citations {
		for _, id := range c.IDs {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}

	var b strings.Builder
	b.WriteString("\n---\n\n## References\n\n")
	b.WriteString("| ID | Type | Summary | Source | Created |\n")
	b.WriteString("|----|------|---------|--------|--------|\n")

	for _, id := range ids {
		obj := objectMap[id]
		if obj == nil {
			continue
		}

		summary := ""
		if len(obj.Summaries) > 0 {
			summary = obj.Summaries[0]
		} else if obj.RawContent != "" {
			summary = obj.RawContent
		}

		if len(summary) > 50 {
			summary = summary[:50] + "..."
		}

		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n",
			id,
			obj.Type,
			summary,
			obj.Source,
			obj.CreatedAt.Format("2006-01-02"),
		)
	}

	return b.String()
}

// EnrichCitationsWithEntities populates each Citation's Entities field with
// the @mention entities from all of its cited objects.
func EnrichCitationsWithEntities(citations []Citation, objectMap map[string]*storage.KnowledgeObject) {
	for i := range citations {
		for _, id := range citations[i].IDs {
			obj := objectMap[id]
			if obj == nil {
				continue
			}
			citations[i].Entities = append(citations[i].Entities, obj.Mentions...)
		}
	}
}

// ExtractCitationIDs returns a deduplicated slice of all object IDs referenced
// across the given citations.
func ExtractCitationIDs(citations []Citation) []string {
	seen := make(map[string]bool)
	var ids []string
	for _, c := range citations {
		for _, id := range c.IDs {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	return ids
}

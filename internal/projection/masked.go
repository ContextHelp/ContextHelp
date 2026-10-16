package projection

import (
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// SearchableText is a structured type's masked projection (ADR-080 §7,
// decision D21): the text built from its searchable minimum fields
// only, which is what a restricted viewer's FTS and vector search match
// on. It returns ok false for an object it does not render, in which
// case the masked projection is the body with every classed part
// removed.
type SearchableText func(ko *storage.KnowledgeObject) (text string, ok bool)

// searchableTexts holds one renderer per structured object type.
var searchableTexts = map[string]SearchableText{}

// RegisterSearchableText installs the masked projection of type typ.
// Structured types register themselves at init; a second registration
// of a type replaces the first.
func RegisterSearchableText(typ string, fn SearchableText) {
	searchableTexts[typ] = fn
}

// maskedParts is the masked projection of a graph-canonical object: the
// indexed parts (summaries and sections) minus the nodes that carry a
// class. withheld reports whether any node was dropped; when none was,
// the masked projection is the raw one and callers reuse it.
func maskedParts(ko *storage.KnowledgeObject) (parts segments, withheld bool) {
	for _, n := range ko.Graph.Nodes {
		switch n.NodeType {
		case storage.NodeTypeSummary, storage.NodeTypeSection:
			if len(storage.NodeClasses(n)) > 0 {
				withheld = true
				continue
			}
			parts.add(n.Content)
		}
	}
	return parts, withheld
}

// masked fills the masked fields of p for ko: a registered structured
// type's searchable text; otherwise the graph parts without classed
// nodes when a node was withheld; otherwise the raw projection, since
// nothing was withheld. A graph whose only indexed parts are classed
// yields an empty masked projection, never the flat body: that body may
// repeat what the class withholds.
func masked(ko *storage.KnowledgeObject, p *storage.IndexProjection) {
	if fn, ok := searchableTexts[ko.Type]; ok {
		if text, ok := fn(ko); ok {
			p.MaskedFTSBody = text
			p.MaskedEmbeddingText = text
			return
		}
	}
	if ko.Graph != nil && len(ko.Graph.Nodes) > 0 {
		if parts, withheld := maskedParts(ko); withheld {
			p.MaskedFTSBody = strings.Join(parts.list, " ")
			p.MaskedEmbeddingText = strings.Join(parts.list, "\n")
			return
		}
	}
	p.MaskedFTSBody = p.FTSBody
	p.MaskedEmbeddingText = p.EmbeddingText
}

// MaskedEmbeddingText returns the text ingest embeds as the masked
// variant for ko: ProjectIndex's MaskedEmbeddingText with the body
// taken from BodyText, as EmbeddingText does for the raw variant. ko is
// not modified.
func MaskedEmbeddingText(ko *storage.KnowledgeObject) string {
	if ko == nil {
		return ""
	}
	if ko.TextContent != "" || ko.RawContent == "" {
		return ProjectIndex(ko).MaskedEmbeddingText
	}
	defaulted := *ko
	defaulted.TextContent = BodyText(ko)
	return ProjectIndex(&defaulted).MaskedEmbeddingText
}

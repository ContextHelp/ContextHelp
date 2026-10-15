// Package projection derives DocumentProjection and IndexProjection from
// a KnowledgeObject. Use these helpers everywhere; never access KO fields ad hoc.
package projection

import (
	"sort"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blobref"
)

// ProjectDocument derives a DocumentProjection from a KnowledgeObject.
// Uses graph nodes when Graph is non-nil and non-empty; falls back to flat fields.
func ProjectDocument(ko *storage.KnowledgeObject) storage.DocumentProjection {
	if ko == nil {
		return storage.DocumentProjection{}
	}
	if ko.Graph == nil || len(ko.Graph.Nodes) == 0 {
		return storage.DocumentProjection{
			Body:     ko.TextContent,
			Sections: ko.Sections,
		}
	}
	sections := make([]storage.Section, 0, len(ko.Graph.Nodes))
	for _, n := range ko.Graph.Nodes {
		if n.NodeType != storage.NodeTypeSection {
			continue
		}
		sections = append(sections, storage.Section{
			Title:   n.Label,
			Content: n.Content,
			Order:   n.Order,
		})
	}
	sort.SliceStable(sections, func(i, j int) bool {
		return sections[i].Order < sections[j].Order
	})
	return storage.DocumentProjection{Sections: sections}
}

// ProjectIndex derives an IndexProjection from a KnowledgeObject.
// Uses graph nodes when Graph is non-nil and non-empty; falls back to flat fields.
// The masked fields follow (see masked).
func ProjectIndex(ko *storage.KnowledgeObject) storage.IndexProjection {
	if ko == nil {
		return storage.IndexProjection{}
	}
	p := rawIndexProjection(ko)
	masked(ko, &p)
	return p
}

// rawIndexProjection is ProjectIndex without the masked fields.
func rawIndexProjection(ko *storage.KnowledgeObject) storage.IndexProjection {
	if ko.Graph == nil || len(ko.Graph.Nodes) == 0 {
		return flatIndexProjection(ko)
	}
	var parts segments
	var tags []storage.Tag
	var mentions []string

	for _, n := range ko.Graph.Nodes {
		switch n.NodeType {
		case storage.NodeTypeSummary, storage.NodeTypeSection:
			parts.add(n.Content)
		case storage.NodeTypeTag:
			label := n.Label
			if label == "" {
				label = n.Content
			}
			tags = append(tags, storage.Tag{Label: label})
		case storage.NodeTypeEntityMention:
			if n.Content != "" {
				mentions = append(mentions, n.Content)
			}
		}
	}
	// If the graph carries Tag/Mention nodes only (e.g. text.short
	// pipeline runs no markdown_parser/sectioner), `parts` is empty and
	// FTSBody would be too — making the document invisible to FTS even
	// though TextContent holds the full body. Fall back to flat text in
	// that case while keeping the graph-derived Tags and Mentions.
	if len(parts.list) == 0 {
		flat := flatIndexProjection(ko)
		flat.Tags = tags
		flat.Mentions = mentions
		return flat
	}
	return storage.IndexProjection{
		FTSBody:       strings.Join(parts.list, " "),
		Tags:          tags,
		Mentions:      mentions,
		EmbeddingText: strings.Join(parts.list, "\n"),
	}
}

func flatIndexProjection(ko *storage.KnowledgeObject) storage.IndexProjection {
	var parts segments
	parts.add(ko.TextContent)
	for _, s := range ko.Summaries {
		parts.add(s)
	}
	for _, s := range ko.Sections {
		parts.add(s.Content)
	}
	mentions := make([]string, len(ko.Mentions))
	for i := range ko.Mentions {
		mentions[i] = ko.Mentions[i].String()
	}
	return storage.IndexProjection{
		FTSBody:       strings.Join(parts.list, " "),
		Tags:          ko.Tags,
		Mentions:      mentions,
		EmbeddingText: strings.Join(parts.list, "\n"),
	}
}

// segments collects the text segments of an index projection in order,
// keeping only the first occurrence of each. A root summary and a section
// often carry the same text, and a body can repeat its summary; indexing
// both copies doubles the text in the FTS body and the embedding input.
// Segments compare after trimming surrounding whitespace; empty segments
// and blob:// refs (pointers to content, not text) are dropped.
type segments struct {
	list []string
	seen map[string]struct{}
}

func (p *segments) add(s string) {
	key := strings.TrimSpace(s)
	if key == "" || blobref.Is(key) {
		return
	}
	if _, dup := p.seen[key]; dup {
		return
	}
	if p.seen == nil {
		p.seen = map[string]struct{}{}
	}
	p.seen[key] = struct{}{}
	p.list = append(p.list, s)
}

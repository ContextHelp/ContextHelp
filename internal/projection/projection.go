// Package projection derives DocumentProjection and IndexProjection from
// a KnowledgeObject. Use these helpers everywhere; never access KO fields ad hoc.
package projection

import (
	"sort"
	"strings"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// ProjectDocument derives a DocumentProjection from a KnowledgeObject.
// Uses graph nodes when Graph is non-nil and non-empty; falls back to flat fields.
func ProjectDocument(ko *pluginapi.KnowledgeObject) pluginapi.DocumentProjection {
	if ko == nil {
		return pluginapi.DocumentProjection{}
	}
	if ko.Graph == nil || len(ko.Graph.Nodes) == 0 {
		return pluginapi.DocumentProjection{
			Body:     ko.TextContent,
			Sections: ko.Sections,
		}
	}
	var sections []pluginapi.Section
	for _, n := range ko.Graph.Nodes {
		if n.NodeType != pluginapi.NodeTypeSection {
			continue
		}
		sections = append(sections, pluginapi.Section{
			Title:   n.Label,
			Content: n.Content,
			Order:   n.Order,
		})
	}
	sort.SliceStable(sections, func(i, j int) bool {
		return sections[i].Order < sections[j].Order
	})
	return pluginapi.DocumentProjection{Sections: sections}
}

// ProjectIndex derives an IndexProjection from a KnowledgeObject.
// Uses graph nodes when Graph is non-nil and non-empty; falls back to flat fields.
func ProjectIndex(ko *pluginapi.KnowledgeObject) pluginapi.IndexProjection {
	if ko == nil {
		return pluginapi.IndexProjection{}
	}
	if ko.Graph == nil || len(ko.Graph.Nodes) == 0 {
		return flatIndexProjection(ko)
	}
	var parts []string
	var tags []pluginapi.Tag
	var mentions []string

	for _, n := range ko.Graph.Nodes {
		switch n.NodeType {
		case pluginapi.NodeTypeSummary, pluginapi.NodeTypeSection:
			if n.Content != "" {
				parts = append(parts, n.Content)
			}
		case pluginapi.NodeTypeTag:
			label := n.Label
			if label == "" {
				label = n.Content
			}
			tags = append(tags, pluginapi.Tag{Label: label})
		case pluginapi.NodeTypeEntityMention:
			if n.Content != "" {
				mentions = append(mentions, n.Content)
			}
		}
	}
	// T-0565: if the graph carries Tag/Mention nodes only (e.g. text.short
	// pipeline runs no markdown_parser/sectioner), `parts` is empty and
	// FTSBody would be too — making the document invisible to FTS even
	// though TextContent holds the full body. Fall back to flat text in
	// that case while keeping the graph-derived Tags and Mentions.
	if len(parts) == 0 {
		flat := flatIndexProjection(ko)
		flat.Tags = tags
		flat.Mentions = mentions
		return flat
	}
	body := strings.Join(parts, " ")
	return pluginapi.IndexProjection{
		FTSBody:       body,
		Tags:          tags,
		Mentions:      mentions,
		EmbeddingText: strings.Join(parts, "\n"),
	}
}

func flatIndexProjection(ko *pluginapi.KnowledgeObject) pluginapi.IndexProjection {
	var parts []string
	if ko.TextContent != "" {
		parts = append(parts, ko.TextContent)
	}
	for _, s := range ko.Summaries {
		parts = append(parts, s)
	}
	for _, s := range ko.Sections {
		if s.Content != "" {
			parts = append(parts, s.Content)
		}
	}
	var mentions []string
	for i := range ko.Mentions {
		mentions = append(mentions, ko.Mentions[i].String())
	}
	body := strings.Join(parts, " ")
	return pluginapi.IndexProjection{
		FTSBody:       body,
		Tags:          ko.Tags,
		Mentions:      mentions,
		EmbeddingText: strings.Join(parts, "\n"),
	}
}

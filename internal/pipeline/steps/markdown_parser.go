package steps

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

var markdownHeadingRe = regexp.MustCompile(`(?m)^(#{1,6})\s+(.+)$`)

// markdownBulletRe matches an unordered-list bullet at the start of a line:
// "- item", "* item", or "+ item". Used by HasMarkdownStructure for routing.
var markdownBulletRe = regexp.MustCompile(`(?m)^[\t ]*[-*+][\t ]+\S`)

// HasMarkdownStructure returns true when content has at least one markdown
// heading (e.g. "# Title", "## Heading") OR at least 5 unordered list items
// (lines starting with "-", "*", or "+" followed by whitespace and content).
//
// This helper is consumed by pipeline selectors (see builtins/text_long.go) so
// that structured markdown — which benefits from sectioner / markdown_parser —
// is routed to text.long even when the length-based heuristic is borderline
// or the routing input is otherwise truncated. A bullet-list-only doc with
// no headings still qualifies as long as it has a real list (>=5 items),
// which keeps random prose with a stray hyphen out.
func HasMarkdownStructure(content string) bool {
	if markdownHeadingRe.MatchString(content) {
		return true
	}
	matches := markdownBulletRe.FindAllStringIndex(content, 5)
	return len(matches) >= 5
}

// MarkdownParser parses Markdown headings and creates sections.
type MarkdownParser struct {
	pipeline.BaseContract
}

// NewMarkdownParser creates a MarkdownParser.
func NewMarkdownParser() *MarkdownParser {
	return &MarkdownParser{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"Sections"},
		}),
	}
}

func (s *MarkdownParser) Name() string { return "markdown_parser" }

func (s *MarkdownParser) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	content := draft.RawContent
	matches := markdownHeadingRe.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return draft, nil
	}

	for i, match := range matches {
		level := len(content[match[2]:match[3]])
		title := strings.TrimSpace(content[match[4]:match[5]])

		// Extract content between this heading and the next.
		start := match[1]
		end := len(content)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		body := strings.TrimSpace(content[start:end])

		draft.Sections = append(draft.Sections, storage.Section{
			Title:   title,
			Content: body,
			Order:   len(draft.Sections),
			Metadata: map[string]any{
				"type":          "heading",
				"heading_level": level,
			},
		})
	}

	draft.Metadata["markdown_heading_count"] = len(matches)

	// Emit canonical graph nodes: root summary + section nodes with contains edges.
	// Skip if ID is empty (e.g., in-pipeline drafts before ID assignment).
	if draft.ID == "" {
		return draft, nil
	}
	if draft.Graph == nil {
		draft.Graph = &pluginapi.ObjectGraph{}
	}
	rootID := pluginapi.NewNodeID(draft.ID, pluginapi.NodeTypeSummary, 0)
	if draft.Graph.FindNode(rootID) == nil {
		draft.Graph.Nodes = append(draft.Graph.Nodes, pluginapi.GraphNode{
			ID:       rootID,
			NodeType: pluginapi.NodeTypeSummary,
			Label:    "root",
			Content:  draft.RawContent,
			Order:    0,
		})
	}
	for i, sec := range draft.Sections {
		secID := pluginapi.NewNodeID(draft.ID, pluginapi.NodeTypeSection, i)
		if draft.Graph.FindNode(secID) != nil {
			continue
		}
		draft.Graph.Nodes = append(draft.Graph.Nodes, pluginapi.GraphNode{
			ID:       secID,
			NodeType: pluginapi.NodeTypeSection,
			Label:    sec.Title,
			Content:  sec.Content,
			Order:    i,
			Metadata: sec.Metadata,
		})
		draft.Graph.Edges = append(draft.Graph.Edges, pluginapi.GraphEdge{
			ID:       fmt.Sprintf("%s->%s", rootID, secID),
			FromID:   rootID,
			ToID:     secID,
			EdgeType: pluginapi.EdgeTypeContains,
		})
	}

	return draft, nil
}

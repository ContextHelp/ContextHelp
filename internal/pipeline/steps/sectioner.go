package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

type Sectioner struct {
	pipeline.BaseContract
	llm providers.LLMProvider
}

func NewSectioner() *Sectioner {
	return &Sectioner{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"Sections"},
		}),
	}
}

// NewSectionerWithLLM creates a Sectioner that uses an LLM to generate section summaries.
func NewSectionerWithLLM(llm providers.LLMProvider) *Sectioner {
	s := NewSectioner()
	s.llm = llm
	return s
}

func (s *Sectioner) Name() string { return "sectioner" }

func (s *Sectioner) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	content := draft.RawContent

	// Split on markdown headings (## ...).
	lines := strings.Split(content, "\n")
	var sections []storage.Section
	var currentTitle string
	var currentContent strings.Builder
	order := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			// Flush previous section.
			if currentContent.Len() > 0 || currentTitle != "" {
				sections = append(sections, storage.Section{
					Title:   currentTitle,
					Content: strings.TrimSpace(currentContent.String()),
					Order:   order,
				})
				order++
			}
			currentTitle = strings.TrimPrefix(trimmed, "## ")
			currentContent.Reset()
		} else {
			currentContent.WriteString(line)
			currentContent.WriteString("\n")
		}
	}

	// Flush last section.
	if currentContent.Len() > 0 || currentTitle != "" {
		sections = append(sections, storage.Section{
			Title:   currentTitle,
			Content: strings.TrimSpace(currentContent.String()),
			Order:   order,
		})
	}

	// If no headings found, treat entire content as one section.
	if len(sections) == 0 {
		sections = []storage.Section{{
			Title:   "",
			Content: strings.TrimSpace(content),
			Order:   0,
		}}
	}

	// If an LLM is available and the content is substantial, generate a summary
	// for each section that lacks one.
	if s.llm != nil && len(draft.RawContent) >= 200 {
		for i, sec := range sections {
			body := sec.Content
			if len(body) < 50 {
				continue
			}
			if len(body) > 1000 {
				body = body[:1000]
			}
			prompt := "Write a one-sentence summary of the following text. Return only the summary, no explanation.\n\nText:\n" + body
			if summary, err := s.llm.Generate(ctx, prompt); err == nil {
				if sections[i].Metadata == nil {
					sections[i].Metadata = make(map[string]any)
				}
				sections[i].Metadata["summary"] = strings.TrimSpace(summary)
			}
			// On failure, leave Summary empty and continue.
		}
	}

	draft.Sections = sections

	// Emit canonical graph nodes: root summary + one section node per section.
	// Skip if ID is empty (e.g., in-pipeline drafts before ID assignment).
	if draft.ID == "" {
		return draft, nil
	}
	if draft.Graph == nil {
		draft.Graph = &pluginapi.ObjectGraph{}
	}
	rootID := pluginapi.NewNodeID(draft.ID, pluginapi.NodeTypeSummary, 0)
	// Only add root if not already present (textcleaner may have added it).
	if draft.Graph.FindNode(rootID) == nil {
		draft.Graph.Nodes = append(draft.Graph.Nodes, pluginapi.GraphNode{
			ID:       rootID,
			NodeType: pluginapi.NodeTypeSummary,
			Label:    "root",
			Content:  draft.RawContent,
			Order:    0,
		})
	}
	for i, sec := range sections {
		secID := pluginapi.NewNodeID(draft.ID, pluginapi.NodeTypeSection, i)
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

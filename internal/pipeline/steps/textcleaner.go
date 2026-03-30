package steps

import (
	"context"
	"regexp"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

type TextCleaner struct {
	pipeline.BaseContract
}

func NewTextCleaner() *TextCleaner {
	return &TextCleaner{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"RawContent"},
		}),
	}
}

func (s *TextCleaner) Name() string { return "text_cleaner" }

var multiSpace = regexp.MustCompile(`[ \t]+`)
var multiNewline = regexp.MustCompile(`\n{3,}`)

func (s *TextCleaner) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	text := draft.RawContent
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = multiSpace.ReplaceAllString(text, " ")
	text = multiNewline.ReplaceAllString(text, "\n\n")
	text = strings.TrimSpace(text)
	draft.RawContent = text

	// Emit canonical graph node: summary node holding the cleaned text.
	// Skip if ID is empty (e.g., in-pipeline drafts before ID assignment).
	if draft.ID != "" {
		if draft.Graph == nil {
			draft.Graph = &pluginapi.ObjectGraph{}
		}
		draft.Graph.Nodes = append(draft.Graph.Nodes, pluginapi.GraphNode{
			ID:       pluginapi.NewNodeID(draft.ID, pluginapi.NodeTypeSummary, 0),
			NodeType: pluginapi.NodeTypeSummary,
			Label:    "cleaned text",
			Content:  text,
			Order:    0,
		})
	}

	return draft, nil
}

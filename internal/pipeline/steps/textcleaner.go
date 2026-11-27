package steps

import (
	"context"
	"regexp"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/representation"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type TextCleaner struct {
	pipeline.BaseContract
}

func NewTextCleaner() *TextCleaner {
	return &TextCleaner{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"RawContent", "Graph"},
		}),
	}
}

func (s *TextCleaner) Name() string { return "text_cleaner" }

var multiSpace = regexp.MustCompile(`[ \t]+`)
var multiNewline = regexp.MustCompile(`\n{3,}`)

func (s *TextCleaner) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	text := collapseWhitespace(draft.RawContent)
	draft.RawContent = text

	// Emit canonical graph node: summary node holding the cleaned text.
	// Skip if ID is empty (e.g., in-pipeline drafts before ID assignment).
	if draft.ID != "" && !representation.HasContent(draft.Graph) {
		if draft.Graph == nil {
			draft.Graph = &storage.ObjectGraph{}
		}
		// summary/0 is the cleaned text unless something already holds it;
		// a summary representation there is not the cleaned text, so this
		// one takes the next free ordinal. A plain summary/0 is left alone.
		id := storage.NewNodeID(draft.ID, storage.NodeTypeSummary, 0)
		add := true
		if n := draft.Graph.FindNode(id); n != nil {
			if _, rep := representation.MetaOf(*n); rep {
				id = freeNodeID(draft.Graph, draft.ID, storage.NodeTypeSummary, 0)
			} else {
				add = false
			}
		}
		if add {
			draft.Graph.Nodes = append(draft.Graph.Nodes, storage.GraphNode{
				ID:       id,
				NodeType: storage.NodeTypeSummary,
				Label:    "cleaned text",
				Content:  text,
				Order:    0,
			})
		}
	}

	return draft, nil
}

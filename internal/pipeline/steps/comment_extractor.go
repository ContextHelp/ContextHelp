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

// annotationRe matches TODO, FIXME, HACK, and NOTE annotations in code comments.
var annotationRe = regexp.MustCompile(`(?i)(//|#|/\*)\s*(TODO|FIXME|HACK|NOTE)[:\s]+(.+)`)

// CommentExtractor extracts TODO/FIXME/HACK/NOTE annotations from code.
type CommentExtractor struct {
	pipeline.BaseContract
}

// NewCommentExtractor creates a CommentExtractor.
func NewCommentExtractor() *CommentExtractor {
	return &CommentExtractor{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"Sections", "Graph"},
		}),
	}
}

func (s *CommentExtractor) Name() string { return "comment_extractor" }

func (s *CommentExtractor) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	lines := strings.Split(draft.RawContent, "\n")
	var annotations []map[string]string

	for i, line := range lines {
		m := annotationRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		kind := strings.ToUpper(m[2])
		text := strings.TrimSpace(m[3])
		lineNum := i + 1

		annotations = append(annotations, map[string]string{
			"kind": kind,
			"text": text,
		})

		draft.Sections = append(draft.Sections, storage.Section{
			Title:   kind + ": " + text,
			Content: line,
			Order:   len(draft.Sections),
			Metadata: map[string]any{
				"type":        "comment",
				"annotation":  kind,
				"line_number": lineNum,
			},
		})
	}

	draft.Metadata["annotation_count"] = len(annotations)
	draft.Metadata["annotations"] = annotations

	// Intra-object graph: one NodeTypeTask node per annotation.
	// These are purely within-object nodes; no storage.Edge is written here (ADR-063).
	if draft.ID != "" && len(annotations) > 0 {
		if draft.Graph == nil {
			draft.Graph = &pluginapi.ObjectGraph{}
		}
		for i, ann := range annotations {
			nodeID := pluginapi.NewNodeID(draft.ID, pluginapi.NodeTypeTask, i)
			draft.Graph.Nodes = append(draft.Graph.Nodes, pluginapi.GraphNode{
				ID:       nodeID,
				NodeType: pluginapi.NodeTypeTask,
				Label:    fmt.Sprintf("%s: %s", ann["kind"], ann["text"]),
				Order:    i,
				Metadata: map[string]any{
					"annotation": ann["kind"],
				},
			})
		}
	}

	return draft, nil
}

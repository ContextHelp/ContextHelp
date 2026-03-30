package projection_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	"github.com/ideacrafterslabs/ctxt/pkg/projection"
	"github.com/stretchr/testify/assert"
)

func TestProjectDocument_NilKO(t *testing.T) {
	doc := projection.ProjectDocument(nil)
	assert.Equal(t, pluginapi.DocumentProjection{}, doc)
}

func TestProjectDocument_FlatFallback(t *testing.T) {
	ko := &pluginapi.KnowledgeObject{
		TextContent: "body text",
		Sections: []pluginapi.Section{
			{Title: "Intro", Content: "intro body", Order: 0},
		},
	}
	doc := projection.ProjectDocument(ko)
	assert.Equal(t, "body text", doc.Body)
	assert.Len(t, doc.Sections, 1)
	assert.Equal(t, "Intro", doc.Sections[0].Title)
}

func TestProjectDocument_GraphNodes(t *testing.T) {
	ko := &pluginapi.KnowledgeObject{
		ID: "obj1",
		Graph: &pluginapi.ObjectGraph{
			Nodes: []pluginapi.GraphNode{
				{
					ID:       pluginapi.NewNodeID("obj1", pluginapi.NodeTypeSection, 0),
					NodeType: pluginapi.NodeTypeSection,
					Label:    "Section A",
					Content:  "content A",
					Order:    0,
				},
			},
		},
	}
	doc := projection.ProjectDocument(ko)
	assert.Len(t, doc.Sections, 1)
	assert.Equal(t, "Section A", doc.Sections[0].Title)
	assert.Equal(t, "content A", doc.Sections[0].Content)
}

func TestProjectIndex_NilKO(t *testing.T) {
	idx := projection.ProjectIndex(nil)
	assert.Equal(t, pluginapi.IndexProjection{}, idx)
}

func TestProjectIndex_FlatFallback(t *testing.T) {
	ko := &pluginapi.KnowledgeObject{
		TextContent: "flat body",
		Tags:        []pluginapi.Tag{{Label: "go"}},
	}
	idx := projection.ProjectIndex(ko)
	assert.Contains(t, idx.FTSBody, "flat body")
	assert.Len(t, idx.Tags, 1)
	assert.Equal(t, "go", idx.Tags[0].Label)
}

func TestProjectIndex_GraphNodes(t *testing.T) {
	ko := &pluginapi.KnowledgeObject{
		ID: "obj2",
		Graph: &pluginapi.ObjectGraph{
			Nodes: []pluginapi.GraphNode{
				{
					ID:       pluginapi.NewNodeID("obj2", pluginapi.NodeTypeSummary, 0),
					NodeType: pluginapi.NodeTypeSummary,
					Content:  "graph summary",
					Order:    0,
				},
				{
					ID:       pluginapi.NewNodeID("obj2", pluginapi.NodeTypeTag, 0),
					NodeType: pluginapi.NodeTypeTag,
					Label:    "architecture",
					Order:    0,
				},
			},
		},
	}
	idx := projection.ProjectIndex(ko)
	assert.Contains(t, idx.FTSBody, "graph summary")
	assert.Len(t, idx.Tags, 1)
	assert.Equal(t, "architecture", idx.Tags[0].Label)
}

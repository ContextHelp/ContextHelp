package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/representation"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func shareViewer(t *testing.T, fields ...string) sharing.Viewer {
	t.Helper()
	p := sharing.Policy{Name: "p", Data: "type==note", Fields: map[string][]string{"*": fields}}
	v, err := sharing.Compile(sharing.Input{PrincipalID: "prn", Grants: []sharing.Grant{{ID: "g", Audience: "p", Policy: p}}})
	require.NoError(t, err)
	return v
}

// The graph of an object derived by a built-in pipeline shows to a viewer
// whose policy projects content and graph, and stays withheld from one
// whose policy projects the graph without the content: every
// graph-producing step reads the content.
func TestShareGraphFollowsContentForBuiltInPipelines(t *testing.T) {
	e := newRealEnv(t)
	for _, name := range []string{"doc.markdown", "doc.code", "doc.pdf", "doc.office", "url.generic", "image.ocr"} {
		t.Run(name, func(t *testing.T) {
			obj := &storage.KnowledgeObject{
				ID: "sh-" + name, Type: "note", RawContent: "visible text", Pipeline: name + "@v0",
				Graph: &storage.ObjectGraph{Stamped: true, Nodes: []storage.GraphNode{
					{ID: "sh/section/0", NodeType: storage.NodeTypeSection, Content: "node text", Metadata: map[string]any{storage.ProducedByKey: "sectioner", "k": "v"}},
					{ID: "sh/section/1", NodeType: storage.NodeTypeSection, Content: "more"},
				}, Edges: []storage.GraphEdge{{ID: "e", FromID: "sh/section/0", ToID: "sh/section/1", Metadata: map[string]any{storage.ProducedByKey: "sectioner"}}}},
			}
			shown := sharing.Render(shareViewer(t, "content", "graph"), obj, []bool{true}, e.svc.provenance)
			require.NotNil(t, shown.Object.Graph, "graph withheld from a viewer who sees content and graph")
			assert.Len(t, shown.Object.Graph.Nodes, 2)
			assert.False(t, shown.Object.Graph.Stamped)
			assert.Equal(t, map[string]any{"k": "v"}, shown.Object.Graph.Nodes[0].Metadata, "step name reached a viewer")
			assert.Empty(t, shown.Object.Graph.Edges[0].Metadata, "step name reached a viewer")
			assert.Equal(t, "sectioner", obj.Graph.Nodes[0].ProducedBy(), "the stored graph was changed")

			hidden := sharing.Render(shareViewer(t, "graph"), obj, []bool{true}, e.svc.provenance)
			assert.Nil(t, hidden.Object.Graph, "graph shown without the content it derives from")
		})
	}
}

// A viewer's section metadata carries no step name.
func TestProjectDocumentOmitsStepNames(t *testing.T) {
	g := &storage.ObjectGraph{Nodes: []storage.GraphNode{{
		ID: "p/section/0", NodeType: storage.NodeTypeSection, Label: "s", Content: "c",
		Metadata: map[string]any{storage.ProducedByKey: "ocr_extractor", representation.KeyRepresentation: string(representation.Transcript)},
	}}}
	got := projection.ProjectDocument(&storage.KnowledgeObject{ID: "p", Graph: g})
	require.NotEmpty(t, got.Sections)
	require.NotNil(t, got.Sections[0].Metadata, "the node is not a representation: the check is vacuous")
	for _, s := range got.Sections {
		assert.NotContains(t, s.Metadata, storage.ProducedByKey)
	}
	assert.Equal(t, "ocr_extractor", g.Nodes[0].ProducedBy(), "the stored node was changed")
}

// A viewer's copy of an edge never aliases the stored one.
func TestRenderedEdgeMetadataIsACopy(t *testing.T) {
	e := newRealEnv(t)
	obj := &storage.KnowledgeObject{
		ID: "al", Type: "note", RawContent: "visible text", Pipeline: "doc.markdown@v0",
		Graph: &storage.ObjectGraph{Nodes: []storage.GraphNode{
			{ID: "al/section/0", NodeType: storage.NodeTypeSection, Content: "a"},
			{ID: "al/section/1", NodeType: storage.NodeTypeSection, Content: "b"},
		}, Edges: []storage.GraphEdge{{ID: "e", FromID: "al/section/0", ToID: "al/section/1", Metadata: map[string]any{"k": "v"}}}},
	}
	r := sharing.Render(shareViewer(t, "content", "graph"), obj, []bool{true}, e.svc.provenance)
	require.NotNil(t, r.Object.Graph)
	require.Len(t, r.Object.Graph.Edges, 1)
	r.Object.Graph.Edges[0].Metadata["k"] = "changed"
	assert.Equal(t, "v", obj.Graph.Edges[0].Metadata["k"], "the rendered edge aliases the stored one")
}

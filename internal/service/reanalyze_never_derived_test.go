package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/ingest"
	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// importAdapter emits one record carrying its own section (the whole
// body) and one contact with a card and a note node.
type importAdapter struct{}

func (importAdapter) Name() string { return "imp" }

func (importAdapter) Fetch(context.Context) ([]ingest.Object, error) {
	return []ingest.Object{{
		ID: "doc", Type: "note", Content: invText, Tags: []string{"imported"},
		Graph: &storage.ObjectGraph{Nodes: []storage.GraphNode{{NodeType: storage.NodeTypeSection, Label: "body", Content: invText}}},
	}, {
		ID: "card", Type: "contact", Content: "Sarah Lee",
		Graph: &storage.ObjectGraph{Nodes: []storage.GraphNode{
			{NodeType: storage.NodeTypeSection, Label: "card", Content: "Sarah Lee"},
			{NodeType: storage.NodeTypeSection, Label: "note", Content: "private", Order: 1},
		}},
	}}, nil
}

// An object no pipeline derived (an import) has nothing to re-run: the
// reanalysis is refused, with and without the network, and the row, graph
// and document projection stay as imported.
func TestReanalyzeRefusesAnObjectNoPipelineDerived(t *testing.T) {
	e := newRealEnv(t)
	_, err := ingest.NewRunner(e.driver.Objects()).Run(ownerCtx(), importAdapter{})
	require.NoError(t, err)
	objs, _, err := e.driver.Objects().List(ownerCtx(), storage.ObjectFilter{Status: storage.ObjectStatusAll})
	require.NoError(t, err)
	require.Len(t, objs, 2)
	for _, o := range objs {
		before := e.get(t, o.ID)
		require.Empty(t, before.Pipeline)
		docBefore := projection.ProjectDocument(before)
		for _, opts := range []ReanalyzeOptions{{}, {AllowNetwork: true}} {
			_, _, err := e.svc.ReanalyzeObjectWith(ownerCtx(), o.ID, opts)
			require.ErrorIs(t, err, upgrade.ErrCannotReanalyze, "%s", o.Type)
			assert.Contains(t, err.Error(), "never derived")
			after := e.get(t, o.ID)
			assert.Equal(t, before, after, "the row changed")
			assert.Equal(t, docBefore, projection.ProjectDocument(after))
		}
	}
}

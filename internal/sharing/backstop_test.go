package sharing_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
)

func ledgerWith(objects, entities, edges []string, withheld ...sharing.Withheld) *sharing.Ledger {
	l := sharing.NewLedger()
	for _, o := range objects {
		l.AddObject(o)
	}
	for _, e := range entities {
		l.AddEntity(e)
	}
	for _, e := range edges {
		l.AddEdge(e)
	}
	l.AddWithheld(withheld)
	return l
}

func run(t *testing.T, body string, paths []sharing.IDPath, l *sharing.Ledger) sharing.BackstopResult {
	t.Helper()
	res, err := sharing.Backstop([]byte(body), paths, l)
	require.NoError(t, err)
	return res
}

// Elements whose id is not in the ledger are removed from lists; the
// others stay as they were.
func TestBackstopStripsUnreleasedListElements(t *testing.T) {
	l := ledgerWith([]string{"a", "b"}, nil, nil)
	res := run(t, `{"data":[{"id":"a","x":1},{"id":"hidden","x":2},{"id":"b"}],"total":3}`, sharing.HTTPPaths(sharing.ShapeObjects), l)
	assert.JSONEq(t, `{"data":[{"id":"a","x":1},{"id":"b"}],"total":3}`, string(res.Body))
	assert.Equal(t, []string{"data[].id=hidden"}, res.Hits)
	assert.False(t, res.Absent)
}

// A root id not in the ledger marks the body absent.
func TestBackstopRootIDMarksAbsent(t *testing.T) {
	l := ledgerWith([]string{"a"}, nil, nil)
	res := run(t, `{"id":"hidden","type":"note"}`, sharing.HTTPPaths(sharing.ShapeObjects), l)
	assert.True(t, res.Absent)
	assert.Contains(t, res.Hits, "id=hidden")
	ok := run(t, `{"id":"a","type":"note"}`, sharing.HTTPPaths(sharing.ShapeObjects), l)
	assert.False(t, ok.Absent)
	assert.Empty(t, ok.Hits)
}

// Link hops: the edge id, both ends (an object or an entity URI) and
// the hop's object must all be released.
func TestBackstopLinkHops(t *testing.T) {
	l := ledgerWith([]string{"a", "b"}, []string{"person.sarah"}, []string{"e1", "e2"})
	body := `{"data":[
		{"from":"a","object_id":"b","edge":{"id":"e1","from_id":"a","to_id":"b"}},
		{"from":"a","object_id":"hidden","edge":{"id":"e9","from_id":"a","to_id":"hidden"}},
		{"from":"a","object_id":"a","edge":{"id":"e2","from_id":"a","to_id":"ctxt://entity/person.sarah"}},
		{"from":"a","object_id":"a","edge":{"id":"e3","from_id":"a","to_id":"ctxt://entity/person.secret"}}
	]}`
	res := run(t, body, sharing.HTTPPaths(sharing.ShapeObjects), l)
	var out struct {
		Data []struct {
			Edge struct{ ID string } `json:"edge"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(res.Body, &out))
	require.Len(t, out.Data, 2)
	assert.Equal(t, "e1", out.Data[0].Edge.ID)
	assert.Equal(t, "e2", out.Data[1].Edge.ID)
	// A removed element is counted once, for the first path that hit it.
	assert.Equal(t, []string{"data[].object_id=hidden", "data[].edge.id=e3"}, res.Hits)
}

// Graph node ids carry their object id as prefix; derived source lists
// lose the ids of unreleased objects.
func TestBackstopNodeIDsAndSourceIDs(t *testing.T) {
	l := ledgerWith([]string{"a"}, nil, nil)
	body := `{"id":"a","graph":{"nodes":[{"id":"a/section/0"},{"id":"hidden/section/0"}]},"metadata":{"source_ids":["a","hidden"]}}`
	res := run(t, body, sharing.HTTPPaths(sharing.ShapeObjects), l)
	assert.JSONEq(t, `{"id":"a","graph":{"nodes":[{"id":"a/section/0"}]},"metadata":{"source_ids":["a"]}}`, string(res.Body))
	assert.Len(t, res.Hits, 2)
}

// Entity bodies: a slug not in the ledger goes, whatever shape the
// route declared; an object route's walker never looks at slugs.
func TestBackstopEntities(t *testing.T) {
	l := ledgerWith(nil, []string{"person.sarah"}, nil)
	res := run(t, `{"data":[{"slug":"person.sarah"},{"slug":"person.secret"}]}`, sharing.HTTPPaths(sharing.ShapeEntities), l)
	assert.JSONEq(t, `{"data":[{"slug":"person.sarah"}]}`, string(res.Body))
	root := run(t, `{"slug":"person.secret","title":"Secret"}`, sharing.HTTPPaths(sharing.ShapeEntities), l)
	assert.True(t, root.Absent)
}

// The search graph: node keys and edge ends must name released
// objects or entities; the query node always stays.
func TestBackstopSearchGraph(t *testing.T) {
	l := ledgerWith([]string{"a"}, []string{"person.sarah"}, nil)
	body := `{"nodes":{"query":{},"obj:a":{},"obj:hidden":{},"ent:person.sarah":{},"ent:person.secret":{}},
		"edges":[{"source":"query","target":"obj:a"},{"source":"query","target":"obj:hidden"},{"source":"obj:a","target":"ent:person.secret"}]}`
	res := run(t, body, sharing.HTTPPaths(sharing.ShapeMixed), l)
	assert.JSONEq(t, `{"nodes":{"query":{},"obj:a":{},"ent:person.sarah":{}},"edges":[{"source":"query","target":"obj:a"}]}`, string(res.Body))
	assert.Len(t, res.Hits, 4)
}

// MCP tools/call answers hold the search results as a JSON document
// inside a string; the walker follows it.
func TestBackstopMCPNestedResults(t *testing.T) {
	l := ledgerWith([]string{"a"}, nil, nil, sharing.Withheld{Category: "email", Value: "sarah.lee@acme.example"})
	inner := `{"count":2,"results":[{"id":"a","snippet":"Sarah Lee sarah.lee@acme.example"},{"id":"hidden","snippet":"x"}]}`
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{
		"content": []map[string]any{{"type": "text", "text": inner}},
	}})
	res := run(t, string(body), sharing.HTTPPaths(sharing.ShapeObjects), l)
	var out struct {
		Result struct {
			Content []struct{ Text string } `json:"content"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(res.Body, &out))
	require.Len(t, out.Result.Content, 1)
	assert.JSONEq(t, `{"count":2,"results":[{"id":"a","snippet":"Sarah Lee [email]"}]}`, out.Result.Content[0].Text)
	assert.ElementsMatch(t, []string{"result.content[].text!json.results[].id=hidden", "canary:email"}, res.Hits)
}

// The canary replaces withheld values in string values only, longest
// first, in every shape including none; keys and numbers are left
// alone, and a value that is not there is no hit.
func TestBackstopCanary(t *testing.T) {
	l := ledgerWith(nil, nil, nil,
		sharing.Withheld{Category: "note", Value: "prefers mornings"},
		sharing.Withheld{Category: "phone", Value: "+1 555 0100 2001"},
	)
	body := `{"prefers mornings":1,"text":"she prefers mornings, call +1 555 0100 2001","n":2001,"list":["+1 555 0100 2001"]}`
	res := run(t, body, nil, l)
	assert.JSONEq(t, `{"prefers mornings":1,"text":"she [note], call [phone]","n":2001,"list":["[phone]"]}`, string(res.Body))
	assert.ElementsMatch(t, []string{"canary:note", "canary:phone", "canary:phone"}, res.Hits)

	clean := run(t, `{"text":"nothing to see"}`, nil, l)
	assert.Empty(t, clean.Hits)
	assert.JSONEq(t, `{"text":"nothing to see"}`, string(clean.Body))
}

// A body that is not JSON cannot be walked and is refused when there
// are paths; with none the canary still runs on the text.
func TestBackstopNotJSON(t *testing.T) {
	l := ledgerWith(nil, nil, nil, sharing.Withheld{Category: "note", Value: "prefers mornings"})
	_, err := sharing.Backstop([]byte("plain text"), sharing.HTTPPaths(sharing.ShapeObjects), l)
	assert.ErrorIs(t, err, sharing.ErrNotJSON)
	res := run(t, "she prefers mornings", nil, l)
	assert.Equal(t, "she [note]", string(res.Body))
	empty := run(t, "", sharing.HTTPPaths(sharing.ShapeObjects), l)
	assert.Empty(t, empty.Hits)
}

// Error envelopes and bodies without the walked paths pass untouched.
func TestBackstopLeavesOtherBodiesAlone(t *testing.T) {
	l := ledgerWith([]string{"a"}, nil, nil)
	env := `{"error":{"code":"NOT_FOUND","message":"object not found","details":{}}}`
	res := run(t, env, sharing.HTTPPaths(sharing.ShapeObjects), l)
	assert.JSONEq(t, env, string(res.Body))
	assert.Empty(t, res.Hits)
	whoami := `{"principal":"prn_x","roles":["reader"],"shared":{"owner":"Owner","policies":["colleagues-network"]}}`
	res = run(t, whoami, sharing.HTTPPaths(sharing.ShapeNone), l)
	assert.JSONEq(t, whoami, string(res.Body))
}

// gRPC bodies in protojson: node-aware results and node hits.
func TestBackstopGRPCPaths(t *testing.T) {
	l := ledgerWith([]string{"a"}, nil, nil)
	body := `{"results":[
		{"object":{"id":"a"},"node_hits":[{"object_id":"a","node_ref":"ctxt:node/a/section/0"},{"object_id":"hidden","node_ref":"ctxt:node/hidden/section/0"}]},
		{"object":{"id":"hidden"}}
	],"total":2}`
	res := run(t, body, sharing.GRPCPaths(sharing.ShapeObjects), l)
	assert.JSONEq(t, `{"results":[{"object":{"id":"a"},"node_hits":[{"object_id":"a","node_ref":"ctxt:node/a/section/0"}]}],"total":2}`, string(res.Body))
	assert.Equal(t, []string{"results[].object.id=hidden", "results[].node_hits[].object_id=hidden"}, res.Hits)
}

func TestBackstopHitsCount(t *testing.T) {
	before := sharing.BackstopHits()
	run(t, `{"data":[{"id":"hidden"}]}`, sharing.HTTPPaths(sharing.ShapeObjects), ledgerWith(nil, nil, nil))
	assert.Equal(t, before+1, sharing.BackstopHits())
}

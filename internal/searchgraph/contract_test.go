package searchgraph

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const jgfSchemaID = "http://jsongraphformat.info/v2.1/json-graph-schema.json"

var (
	schemaOnce sync.Once
	schema     *jsonschema.Schema
	schemaErr  error
)

// jgfSchema compiles the vendored JGF v2.1 schema once.
func jgfSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	schemaOnce.Do(func() {
		f, err := os.Open(filepath.Join("testdata", "json-graph-schema_v2.json"))
		if err != nil {
			schemaErr = err
			return
		}
		defer f.Close()
		doc, err := jsonschema.UnmarshalJSON(f)
		if err != nil {
			schemaErr = err
			return
		}
		c := jsonschema.NewCompiler()
		if err := c.AddResource(jgfSchemaID, doc); err != nil {
			schemaErr = err
			return
		}
		schema, schemaErr = c.Compile(jgfSchemaID)
	})
	if schemaErr != nil {
		t.Fatalf("compile JGF schema: %v", schemaErr)
	}
	return schema
}

func validateJGF(t *testing.T, raw []byte) {
	t.Helper()
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse document: %v", err)
	}
	if err := jgfSchema(t).Validate(inst); err != nil {
		t.Fatalf("document violates JGF v2.1 schema: %v", err)
	}
}

// Contract key sets, restated from the vocabulary.
var (
	graphKeys     = keySet("id", "type", "label", "directed", "metadata", "nodes", "edges")
	graphMetaKeys = keySet("vocabulary", "generated_at", "query", "fts_query", "mode", "vector_error", "vector_model", "semantic_status",
		"fts_pool", "vector_pool", "rrf_k", "limit", "fts_weight", "vector_weight", "threshold",
		"weights", "counts", "truncated", "caps")
	weightKeys = keySet("mention_boost", "max_mention_boost", "direct_backlink", "hop_backlink", "word_overlap")
	countKeys  = keySet("fts_hits", "vector_hits", "candidates", "both", "fts_only", "vector_only",
		"returned", "cut_limit", "cut_threshold", "nodes", "edges", "entities")
	capKeys      = keySet("max_nodes", "max_edges")
	nodeKeys     = keySet("label", "metadata")
	nodeMetaKeys = map[string]map[string]bool{
		KindQuery: keySet("kind"),
		KindObject: keySet("kind", "object_id", "stage", "legs", "rank", "fts_rank", "vector_rank",
			"fts_raw", "vector_raw", "rrf", "score", "object_type", "source", "created_at"),
		KindEntity: keySet("kind", "slug", "mention_count"),
	}
	scoreKeys    = keySet("fts", "vector", "mention_boost", "graph_relevance", "word_overlap", "total")
	edgeKeys     = keySet("source", "target", "relation", "directed", "metadata")
	edgeMetaKeys = keySet("derivation", "weight")

	stages = keySet("returned", "cut_limit", "cut_threshold")
	legs   = keySet("fts", "vector", "both")
	modes  = keySet("hybrid", "fts_only", "fts_fallback")
)

// relationRule is one row of the edge table.
type relationRule struct {
	srcKind, dstKind string
	directed         bool
	derivation       string
	weighted         bool
}

var relationRules = map[string]relationRule{
	"matched":      {KindQuery, KindObject, true, "derived", true},
	"mentions":     {KindObject, KindEntity, true, "stored", false},
	"extends":      {KindObject, KindObject, true, "stored", false},
	"contradicts":  {KindObject, KindObject, true, "stored", false},
	"supersedes":   {KindObject, KindObject, true, "stored", false},
	"supports":     {KindObject, KindObject, true, "stored", false},
	"related-to":   {KindObject, KindObject, false, "stored", false},
	"derived-from": {KindObject, KindObject, true, "stored", false},
	"co_mention":   {KindObject, KindObject, false, "derived", true},
	"similar":      {KindObject, KindObject, false, "derived", true},
}

func keySet(ks ...string) map[string]bool {
	m := make(map[string]bool, len(ks))
	for _, k := range ks {
		m[k] = true
	}
	return m
}

func checkKeys(t *testing.T, where string, obj map[string]any, allowed map[string]bool) {
	t.Helper()
	for k, v := range obj {
		if !allowed[k] {
			t.Errorf("%s: key %q not in contract", where, k)
		}
		if v == nil {
			t.Errorf("%s: key %q is null; absent values must be omitted", where, k)
		}
	}
}

func asObj(t *testing.T, where string, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%s: want object, got %T", where, v)
	}
	return m
}

// checkContract asserts a serialized document uses only contract keys and
// honours the node-id, relation, direction, derivation and endpoint rules.
func checkContract(t *testing.T, raw []byte) {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	checkKeys(t, "document", doc, keySet("graph"))
	g := asObj(t, "graph", doc["graph"])
	md := checkHeader(t, g)
	kinds := checkNodes(t, asObj(t, "graph.nodes", g["nodes"]))
	edges, ok := g["edges"].([]any)
	if !ok {
		t.Fatalf("graph.edges: want array, got %T", g["edges"])
	}
	checkEdges(t, edges, kinds)

	entities := 0
	for _, k := range kinds {
		if k == KindEntity {
			entities++
		}
	}
	counts := asObj(t, "counts", md["counts"])
	if int(counts["nodes"].(float64)) != len(kinds) || int(counts["edges"].(float64)) != len(edges) ||
		int(counts["entities"].(float64)) != entities {
		t.Errorf("counts nodes/edges/entities = %v/%v/%v, graph has %d/%d/%d",
			counts["nodes"], counts["edges"], counts["entities"], len(kinds), len(edges), entities)
	}
}

func checkHeader(t *testing.T, g map[string]any) map[string]any {
	t.Helper()
	checkKeys(t, "graph", g, graphKeys)
	if g["id"] != GraphID || g["type"] != GraphType || g["directed"] != true {
		t.Errorf("graph header = %v/%v/%v", g["id"], g["type"], g["directed"])
	}
	md := asObj(t, "graph.metadata", g["metadata"])
	checkKeys(t, "graph.metadata", md, graphMetaKeys)
	checkKeys(t, "graph.metadata.weights", asObj(t, "weights", md["weights"]), weightKeys)
	checkKeys(t, "graph.metadata.counts", asObj(t, "counts", md["counts"]), countKeys)
	checkKeys(t, "graph.metadata.caps", asObj(t, "caps", md["caps"]), capKeys)
	if md["vocabulary"] != Vocabulary {
		t.Errorf("vocabulary = %v", md["vocabulary"])
	}
	if m, _ := md["mode"].(string); !modes[m] {
		t.Errorf("mode %q not in contract", m)
	}
	return md
}

// checkNodes validates every node and returns node id -> kind.
func checkNodes(t *testing.T, nodes map[string]any) map[string]string {
	t.Helper()
	kinds := make(map[string]string, len(nodes))
	queries := 0
	for id, n := range nodes {
		kind := checkNode(t, id, asObj(t, "node "+id, n))
		kinds[id] = kind
		if kind == KindQuery {
			queries++
		}
	}
	if queries != 1 {
		t.Errorf("want exactly one query node, got %d", queries)
	}
	return kinds
}

func checkNode(t *testing.T, id string, node map[string]any) string {
	t.Helper()
	checkKeys(t, "node "+id, node, nodeKeys)
	nm := asObj(t, "node "+id+" metadata", node["metadata"])
	kind, _ := nm["kind"].(string)
	allowed, ok := nodeMetaKeys[kind]
	if !ok {
		t.Errorf("node %s: kind %q not in contract", id, kind)
		return kind
	}
	checkKeys(t, "node "+id+" metadata", nm, allowed)
	switch kind {
	case KindQuery:
		if id != QueryNodeID {
			t.Errorf("query node id = %q", id)
		}
	case KindObject:
		if !strings.HasPrefix(id, ObjectNodePrefix) || nm["object_id"] != strings.TrimPrefix(id, ObjectNodePrefix) {
			t.Errorf("object node %s: bad id/object_id %v", id, nm["object_id"])
		}
		s, _ := nm["stage"].(string)
		l, _ := nm["legs"].(string)
		if !stages[s] || !legs[l] {
			t.Errorf("object node %s: stage %q legs %q", id, s, l)
		}
		checkKeys(t, "node "+id+" score", asObj(t, "score", nm["score"]), scoreKeys)
	case KindEntity:
		if !strings.HasPrefix(id, EntityNodePrefix) || nm["slug"] != strings.TrimPrefix(id, EntityNodePrefix) {
			t.Errorf("entity node %s: bad id/slug %v", id, nm["slug"])
		}
	}
	return kind
}

func checkEdges(t *testing.T, edges []any, kinds map[string]string) {
	t.Helper()
	seen := map[string]bool{}
	for i, e := range edges {
		edge := asObj(t, "edge", e)
		key := checkEdge(t, i, edge, kinds)
		if seen[key] {
			t.Errorf("edge %d: duplicate %s", i, key)
		}
		seen[key] = true
	}
}

// checkEdge validates one edge against its relation rule and returns its
// identity key.
func checkEdge(t *testing.T, i int, edge map[string]any, kinds map[string]string) string {
	t.Helper()
	checkKeys(t, "edge", edge, edgeKeys)
	em := asObj(t, "edge metadata", edge["metadata"])
	checkKeys(t, "edge metadata", em, edgeMetaKeys)
	src, _ := edge["source"].(string)
	dst, _ := edge["target"].(string)
	rel, _ := edge["relation"].(string)
	key := rel + "|" + src + "|" + dst
	rule, ok := relationRules[rel]
	if !ok {
		t.Errorf("edge %d: relation %q not in contract", i, rel)
		return key
	}
	if kinds[src] != rule.srcKind || kinds[dst] != rule.dstKind {
		t.Errorf("edge %d %s: endpoints %s(%s) -> %s(%s) not in graph or wrong kind",
			i, rel, src, kinds[src], dst, kinds[dst])
	}
	if edge["directed"] != rule.directed || em["derivation"] != rule.derivation {
		t.Errorf("edge %d %s: directed = %v derivation = %v", i, rel, edge["directed"], em["derivation"])
	}
	if _, has := em["weight"]; has != rule.weighted {
		t.Errorf("edge %d %s: weight present = %v", i, rel, has)
	}
	if !rule.directed && src >= dst {
		t.Errorf("edge %d %s: undirected source %s >= target %s", i, rel, src, dst)
	}
	return key
}

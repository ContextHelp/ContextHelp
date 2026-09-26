//go:build e2e && unix

package searchgraph_test

import (
	"bytes"
	"cmp"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
)

// Placeholders for values that differ between runs.
const (
	placeholderGeneratedAt = "<generated_at>"
	placeholderCreatedAt   = "<created_at>"
	placeholderVectorError = "<vector_error>"
)

// jgf is a decoded JGF document, kept generic so goldens compare the
// document as emitted, not as this package's structs read it.
type jgf = map[string]any

// decodeJGF parses a bare JGF document; anything else fails the test.
func decodeJGF(t *testing.T, raw []byte) jgf {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc jgf
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("decode JGF: %v\n%s", err, raw)
	}
	if dec.More() {
		t.Fatalf("trailing data after JGF document:\n%s", raw)
	}
	if _, ok := doc["graph"].(map[string]any); !ok || len(doc) != 1 {
		t.Fatalf("not a bare single-graph JGF document:\n%s", raw)
	}
	return doc
}

// Accessors over the generic document. A missing or mistyped field
// panics, which fails the test with the offending path in the trace.

func graphOf(doc jgf) map[string]any  { return doc["graph"].(map[string]any) }
func metaOf(doc jgf) map[string]any   { return graphOf(doc)["metadata"].(map[string]any) }
func nodesOf(doc jgf) map[string]any  { return graphOf(doc)["nodes"].(map[string]any) }
func countsOf(doc jgf) map[string]any { return metaOf(doc)["counts"].(map[string]any) }
func nodeMeta(n any) map[string]any   { return n.(map[string]any)["metadata"].(map[string]any) }
func edgeField(e any, k string) any   { return e.(map[string]any)[k] }
func edgeMeta(e any) map[string]any   { return e.(map[string]any)["metadata"].(map[string]any) }
func count(doc jgf, k string) int     { return intOf(countsOf(doc)[k]) }
func relationOf(e any) string         { return str(edgeField(e, "relation")) }
func sourceOf(e any) string           { return str(edgeField(e, "source")) }
func targetOf(e any) string           { return str(edgeField(e, "target")) }
func edgeDerivation(e any) string     { return str(edgeMeta(e)["derivation"]) }
func objectNode(alias string) string  { return "obj:" + alias }

func edgesOf(doc jgf) []any {
	e, _ := graphOf(doc)["edges"].([]any)
	return e
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func intOf(v any) int {
	n, _ := strconv.Atoi(string(v.(json.Number)))
	return n
}

func isUndirected(e any) bool {
	d, _ := edgeField(e, "directed").(bool)
	return !d
}

func hasNode(doc jgf, id string) bool {
	_, ok := nodesOf(doc)[id]
	return ok
}

func nodeStage(doc jgf, id string) string {
	return str(nodeMeta(nodesOf(doc)[id])["stage"])
}

func edgeKey(e any) [3]string {
	return [3]string{relationOf(e), sourceOf(e), targetOf(e)}
}

func compareEdgeKeys(x, y [3]string) int {
	return cmp.Or(cmp.Compare(x[0], y[0]), cmp.Compare(x[1], y[1]), cmp.Compare(x[2], y[2]))
}

func canonical(doc jgf) []byte {
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		panic(err)
	}
	return b
}

func sameDoc(a, b jgf) bool { return bytes.Equal(canonical(a), canonical(b)) }

// normalize makes a document comparable across runs and machines:
// object ids become fixture aliases, timestamps and the vector error
// become placeholders, undirected edges list their endpoints in alias
// order, edges sort by (relation, source, target) and floats round to 10
// significant digits (SQLite's bm25 goes through libm). vector_raw rounds
// to 6: it is a float32 cosine distance computed in C by sqlite-vec, whose
// last bits may differ with the compiler's multiply-add fusion. Edge
// emission order is asserted separately, since it follows the random ids.
func normalize(t *testing.T, raw []byte) jgf {
	t.Helper()
	doc := decodeJGF(t, []byte(suite.corpus.alias(string(raw))))
	meta := metaOf(doc)
	meta["generated_at"] = placeholderGeneratedAt
	if _, ok := meta["vector_error"]; ok {
		meta["vector_error"] = placeholderVectorError
	}
	for _, n := range nodesOf(doc) {
		m := nodeMeta(n)
		if m["created_at"] != nil {
			m["created_at"] = placeholderCreatedAt
		}
		if v, ok := m["vector_raw"].(json.Number); ok {
			if f, err := v.Float64(); err == nil {
				m["vector_raw"] = json.Number(strconv.FormatFloat(f, 'g', 6, 64))
			}
		}
	}
	edges := edgesOf(doc)
	for _, e := range edges {
		em := e.(map[string]any)
		if isUndirected(e) && str(em["target"]) < str(em["source"]) {
			em["source"], em["target"] = em["target"], em["source"]
		}
	}
	slices.SortStableFunc(edges, func(x, y any) int { return compareEdgeKeys(edgeKey(x), edgeKey(y)) })
	return roundFloats(doc).(jgf)
}

func roundFloats(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			x[k] = roundFloats(e)
		}
	case []any:
		for i, e := range x {
			x[i] = roundFloats(e)
		}
	case json.Number:
		if _, err := x.Int64(); err == nil {
			return x
		}
		f, err := x.Float64()
		if err != nil {
			return x
		}
		return json.Number(strconv.FormatFloat(f, 'g', 10, 64))
	}
	return v
}

// assertGolden compares a normalized document with testdata/golden/<name>.json,
// rewriting the file under -update.
func assertGolden(t *testing.T, name string, doc jgf) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".json")
	got := append(canonical(doc), '\n')
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil { //nolint:gosec // committed golden file
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (regenerate with -update): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s: document differs from golden %s (regenerate with -update if intended)\n--- got\n%s", name, path, got)
	}
}

package searchgraph

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// External-tool checks: xmllint against the vendored official schemas,
// and NetworkX (plus igraph when installed) loading the GraphML export.
// Each skips with a message when its tool is missing.

var (
	graphMLSchemaDir = filepath.Join("testdata", "schemas", "graphml")
	gexfSchemaDir    = filepath.Join("testdata", "schemas", "gexf-1.3")
	// vizElement matches one viz element line of the GEXF output.
	vizElement = regexp.MustCompile(`(?m)^[ \t]*<viz:[a-z]+ [^>]*/>\n`)
)

func requireXmllint(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("xmllint")
	if err != nil {
		t.Skip("xmllint not found on PATH; skipping schema validation (install libxml2 to run it)")
	}
	return path
}

// xmllint validates doc (fed on stdin) offline; extra holds the schema
// flags. The GraphML schemas import each other by absolute URL, so an XML
// catalog maps those onto the vendored copies.
func xmllint(t *testing.T, bin string, doc []byte, extra ...string) error {
	t.Helper()
	catalog, err := filepath.Abs(filepath.Join(graphMLSchemaDir, "catalog.xml"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	args := append([]string{"--nonet", "--noout"}, extra...)
	cmd := exec.CommandContext(ctx, bin, append(args, "-")...) //nolint:gosec // test-controlled args
	cmd.Env = append(os.Environ(), "XML_CATALOG_FILES="+catalog)
	cmd.Stdin = bytes.NewReader(doc)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return &xmllintError{err: err, out: out.String()}
	}
	return nil
}

type xmllintError struct {
	err error
	out string
}

func (e *xmllintError) Error() string { return e.err.Error() + "\n" + e.out }

// validateExport checks raw against the official schemas of its format:
// GraphML against the GraphML 1.0 XSD; GEXF against the GEXF 1.3 RELAX NG
// schema and, with its viz elements removed, the GEXF 1.3 XSD (whose viz
// elements sit in the wrong namespace; see the schemas' LICENSE note).
func validateExport(t *testing.T, bin, ext string, raw []byte) error {
	t.Helper()
	switch ext {
	case extGraphML:
		return xmllint(t, bin, raw, "--schema", filepath.Join(graphMLSchemaDir, "graphml.xsd"))
	case extGEXF:
		if err := xmllint(t, bin, raw, "--relaxng", filepath.Join(gexfSchemaDir, "gexf.rng")); err != nil {
			return err
		}
		return xmllint(t, bin, stripViz(raw), "--schema", filepath.Join(gexfSchemaDir, "gexf.xsd"))
	}
	t.Fatalf("unknown format %s", ext)
	return nil
}

func stripViz(raw []byte) []byte { return vizElement.ReplaceAll(raw, nil) }

// TestExportSchemaValid validates the goldens and the hostile-text
// document against the official schemas.
func TestExportSchemaValid(t *testing.T) {
	bin := requireXmllint(t)
	for _, f := range exportFormats {
		for _, tc := range exportCases {
			t.Run(tc.name+"."+f.ext, func(t *testing.T) {
				raw, err := os.ReadFile(filepath.Join("testdata", tc.name+"."+f.ext))
				if err != nil {
					t.Fatal(err)
				}
				if err := validateExport(t, bin, f.ext, raw); err != nil {
					t.Errorf("schema validation: %v", err)
				}
			})
		}
		t.Run("hostile."+f.ext, func(t *testing.T) {
			if err := validateExport(t, bin, f.ext, encodeXML(t, hostileDoc(t), f.ext)); err != nil {
				t.Errorf("schema validation: %v", err)
			}
		})
	}
}

// TestExportSchemaValidationIsLive proves each validator rejects an
// off-schema document, so a pass above means something.
func TestExportSchemaValidationIsLive(t *testing.T) {
	bin := requireXmllint(t)
	graphml := encodeXML(t, sampleDoc(t), extGraphML)
	gexf := encodeXML(t, sampleDoc(t), extGEXF)
	for name, tc := range map[string]struct {
		ext      string
		raw      []byte
		old, new string
	}{
		"graphml attr.type":     {extGraphML, graphml, `attr.type="double"`, `attr.type="decimal"`},
		"graphml node id":       {extGraphML, graphml, `id="ent:search.2Frrf"`, `id="ent:search/rrf"`},
		"gexf edge type":        {extGEXF, gexf, `type="undirected"`, `type="sideways"`},
		"gexf viz element":      {extGEXF, gexf, `<viz:size `, `<viz:bulk `},
		"gexf structure (xsd)":  {extGEXF, gexf, `mode="static" defaultedgetype`, `mode="frozen" defaultedgetype`},
		"gexf attribute type":   {extGEXF, gexf, `type="integer"`, `type="int"`},
		"gexf viz in gexf ns":   {extGEXF, gexf, `<viz:color `, `<color `},
		"graphml unknown child": {extGraphML, graphml, `<data key="g_type">`, `<datum key="g_type">`},
	} {
		t.Run(name, func(t *testing.T) {
			if !bytes.Contains(tc.raw, []byte(tc.old)) {
				t.Fatalf("output lacks %q", tc.old)
			}
			bad := bytes.Replace(tc.raw, []byte(tc.old), []byte(tc.new), 1)
			if err := validateExport(t, bin, tc.ext, bad); err == nil {
				t.Errorf("validator accepted %q -> %q", tc.old, tc.new)
			}
		})
	}
}

// findPython returns a python3 that can import module, or skips. Every
// python3 on PATH is tried (a version-manager shim may fail in this
// directory), run from a temp dir.
func findPython(t *testing.T, module string) string {
	t.Helper()
	candidates := []string{}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		candidates = append(candidates, filepath.Join(dir, "python3"))
	}
	candidates = append(candidates, "/usr/bin/python3")
	for _, c := range candidates {
		if st, err := os.Stat(c); err != nil || st.IsDir() {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cmd := exec.CommandContext(ctx, c, "-c", "import "+module) //nolint:gosec // test-controlled args
		cmd.Dir = t.TempDir()
		err := cmd.Run()
		cancel()
		if err == nil {
			return c
		}
	}
	t.Skipf("no python3 on PATH can import %s; skipping (pip install %s to run it)", module, module)
	return ""
}

// pyValue is a Python value with its type name, as the loader script
// dumps it.
type pyValue struct {
	Type  string
	Value any
}

func (p *pyValue) UnmarshalJSON(b []byte) error {
	var pair [2]json.RawMessage
	if err := json.Unmarshal(b, &pair); err != nil {
		return err
	}
	if err := json.Unmarshal(pair[0], &p.Type); err != nil {
		return err
	}
	return json.Unmarshal(pair[1], &p.Value)
}

type pyGraph struct {
	Directed bool                          `json:"directed"`
	Graph    map[string]pyValue            `json:"graph"`
	Nodes    map[string]map[string]pyValue `json:"nodes"`
	Edges    []struct {
		Source string             `json:"source"`
		Target string             `json:"target"`
		ID     string             `json:"id"`
		Data   map[string]pyValue `json:"data"`
	} `json:"edges"`
	IGraph *struct {
		Nodes    int      `json:"nodes"`
		Edges    int      `json:"edges"`
		Labels   []string `json:"labels"`
		Relation []string `json:"relation"`
	} `json:"igraph"`
}

// loadWithPython runs the loader script on a GraphML export.
func loadWithPython(t *testing.T, python string, raw []byte) *pyGraph {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "graph.graphml")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs(filepath.Join("testdata", "graphml_load.py"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, script, path) //nolint:gosec // test-controlled args
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("loader failed: %v\n%s", err, stderr.String())
	}
	var g pyGraph
	if err := json.Unmarshal(out, &g); err != nil {
		t.Fatalf("loader output: %v\n%s", err, out)
	}
	return &g
}

// pyTypes maps attribute types to the Python types NetworkX must produce.
var pyTypes = map[attrType]string{attrString: "str", attrInt: "int", attrDouble: "float", attrBool: "bool"}

// checkPyAttrs asserts a loaded element carries exactly the exported
// attributes with the right Python types and values.
func checkPyAttrs[T any](t *testing.T, where string, attrs []attr[T], v T, got map[string]pyValue) {
	t.Helper()
	want := map[string]bool{}
	for _, a := range attrs {
		raw, ok := a.get(v)
		if !ok {
			continue
		}
		want[a.name] = true
		pv, ok := got[a.name]
		if !ok {
			t.Errorf("%s: %s lost", where, a.name)
			continue
		}
		if pv.Type != pyTypes[a.typ] {
			t.Errorf("%s: %s loaded as %s, want %s", where, a.name, pv.Type, pyTypes[a.typ])
			continue
		}
		switch x := raw.(type) {
		case string:
			if want := sanitizedText(x); pv.Value != want {
				t.Errorf("%s: %s = %q, want %q", where, a.name, pv.Value, want)
			}
		case bool:
			if pv.Value != x {
				t.Errorf("%s: %s = %v, want %v", where, a.name, pv.Value, x)
			}
		case int:
			if pv.Value != float64(x) {
				t.Errorf("%s: %s = %v, want %d", where, a.name, pv.Value, x)
			}
		case float64:
			if pv.Value != x {
				t.Errorf("%s: %s = %v, want %v", where, a.name, pv.Value, x)
			}
		}
	}
	for name := range got {
		if !want[name] && !networkxExtra[name] {
			t.Errorf("%s: unexpected attribute %s", where, name)
		}
	}
}

// networkxExtra are attributes NetworkX adds on load: the edge id when
// the graph is not a multigraph, and empty per-domain default maps.
var networkxExtra = map[string]bool{"id": true, "node_default": true, "edge_default": true}

// sanitizedText is s as an XML parser reads it back from the export.
func sanitizedText(s string) string {
	if s == hostileText {
		return hostileWant
	}
	return s
}

// TestGraphMLNetworkXRoundTrip loads the GraphML export with NetworkX and
// checks counts, typed attributes, hostile labels, relations and
// directedness survive. igraph is checked too when installed.
func TestGraphMLNetworkXRoundTrip(t *testing.T) {
	python := findPython(t, "networkx")
	docs := map[string]*Document{"sample": sampleDoc(t), "hostile": hostileDoc(t)}
	for _, tc := range exportCases[1:] {
		tr, f := sampleFixture()
		docs[tc.name] = build(t, tr, f, tc.opts)
	}
	for name, doc := range docs {
		t.Run(name, func(t *testing.T) {
			g := loadWithPython(t, python, encodeXML(t, doc, extGraphML))
			if !g.Directed {
				t.Error("NetworkX loaded an undirected graph")
			}
			checkPyAttrs(t, "graph", graphAttrs, &doc.Graph, g.Graph)
			if len(g.Nodes) != len(doc.Graph.Nodes) || len(g.Edges) != len(doc.Graph.Edges) {
				t.Fatalf("NetworkX nodes/edges = %d/%d, want %d/%d",
					len(g.Nodes), len(g.Edges), len(doc.Graph.Nodes), len(doc.Graph.Edges))
			}
			for id, n := range doc.Graph.Nodes {
				got, ok := g.Nodes[GraphMLID(id)]
				if !ok {
					t.Errorf("node %s missing", id)
					continue
				}
				checkPyAttrs(t, "node "+id, graphMLNodeAttrs, n, got)
			}
			want := map[string]int{}
			for _, e := range doc.Graph.Edges {
				want[GraphMLID(e.Source)+" "+GraphMLID(e.Target)+" "+e.Relation+" "+strconv.FormatBool(e.Directed)]++
			}
			for i, e := range g.Edges {
				rel, _ := e.Data["relation"].Value.(string)
				dir, _ := e.Data["directed"].Value.(bool)
				key := e.Source + " " + e.Target + " " + rel + " " + strconv.FormatBool(dir)
				if want[key] == 0 {
					t.Errorf("NetworkX edge %d %q not in document", i, key)
				}
				want[key]--
				if e.Data["directed"].Type != "bool" || e.Data["relation"].Type != "str" {
					t.Errorf("edge %d: directed/relation types %s/%s", i, e.Data["directed"].Type, e.Data["relation"].Type)
				}
			}
			if g.IGraph == nil {
				t.Log("python-igraph not installed; igraph load skipped")
				return
			}
			if g.IGraph.Nodes != len(doc.Graph.Nodes) || g.IGraph.Edges != len(doc.Graph.Edges) {
				t.Errorf("igraph nodes/edges = %d/%d", g.IGraph.Nodes, g.IGraph.Edges)
			}
			if !strings.Contains(strings.Join(g.IGraph.Relation, " "), RelMatched) {
				t.Error("igraph lost edge relations")
			}
		})
	}
}

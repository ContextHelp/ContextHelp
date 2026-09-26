package searchgraph

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

const (
	extGraphML = "graphml"
	extGEXF    = "gexf"
)

// exportFormats are the XML exporters under test.
var exportFormats = []struct {
	ext    string
	encode func(*Document, io.Writer) error
}{
	{extGraphML, (*Document).EncodeGraphML},
	{extGEXF, (*Document).EncodeGEXF},
}

func encodeXML(t *testing.T, doc *Document, ext string) []byte {
	t.Helper()
	for _, f := range exportFormats {
		if f.ext == ext {
			var buf bytes.Buffer
			if err := f.encode(doc, &buf); err != nil {
				t.Fatalf("encode %s: %v", ext, err)
			}
			return buf.Bytes()
		}
	}
	t.Fatalf("unknown format %s", ext)
	return nil
}

// exportCases are the golden export fixtures.
var exportCases = []struct {
	name string
	opts Options
}{
	{"sample", Options{Similar: true}},
	{"truncated_objects", Options{MaxNodes: 8}},
	{"truncated_entities_edges", Options{MaxNodes: 13, MaxEdges: 20, Similar: true}},
}

func sampleDoc(t *testing.T) *Document {
	t.Helper()
	tr, f := sampleFixture()
	return build(t, tr, f, Options{Similar: true})
}

// hostileText holds markup, CDATA, quotes, C0 controls, noncharacters and
// an invalid UTF-8 byte; hostileWant is what an XML parser must read back.
const (
	hostileText = "</x> & <![CDATA[ ]]> \"q\" 'a' \x01\x08\x0b\x0c\x1f ￾￿ bad\xff tab\tnl\ncr\r end"
	hostileWant = "</x> & <![CDATA[ ]]> \"q\" 'a' ����� �� bad� tab\tnl\ncr\r end"
	// hostileJSONWant is hostileText read back from JSON inside XML: JSON
	// escapes the C0 controls, so only U+FFFE, U+FFFF and the invalid byte
	// are replaced.
	hostileJSONWant = "</x> & <![CDATA[ ]]> \"q\" 'a' \x01\x08\x0b\x0c\x1f \ufffd\ufffd bad\ufffd tab\tnl\ncr\r end"
	// hostileID needs GraphML id escaping.
	hostileID = "ent:we<i>rd&\"sl ug'/x.y"
)

// hostileDoc is the sample with hostile text in the graph label, the
// query, an object label and an extra entity node with a hostile id.
func hostileDoc(t *testing.T) *Document {
	t.Helper()
	doc := sampleDoc(t)
	g := &doc.Graph
	g.Label = hostileText
	g.Metadata.Query = hostileText
	n := g.Nodes["obj:kb-xss"]
	n.Label = hostileText
	g.Nodes["obj:kb-xss"] = n
	g.Nodes[hostileID] = Node{Label: hostileText, Metadata: NodeMetadata{
		Kind: KindEntity, Slug: strings.TrimPrefix(hostileID, EntityNodePrefix), MentionCount: 1,
	}}
	g.Edges = append(g.Edges, Edge{
		Source: "obj:kb-xss", Target: hostileID, Relation: RelMentions, Directed: true,
		Metadata: EdgeMetadata{Derivation: DerivationStored},
	})
	return doc
}

// TestExportGoldens pins the GraphML and GEXF output of representative
// graphs (rerun with -update after an intended change).
func TestExportGoldens(t *testing.T) {
	for _, tc := range exportCases {
		for _, f := range exportFormats {
			t.Run(tc.name+"."+f.ext, func(t *testing.T) {
				tr, fs := sampleFixture()
				got := encodeXML(t, build(t, tr, fs, tc.opts), f.ext)
				checkWellFormed(t, got)
				path := filepath.Join("testdata", tc.name+"."+f.ext)
				if *update {
					if err := os.WriteFile(path, got, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read golden (run with -update): %v", err)
				}
				if !bytes.Equal(got, want) {
					t.Errorf("%s differs from golden; rerun with -update and review the diff\n got:\n%s", path, got)
				}
			})
		}
	}
}

// checkWellFormed parses raw with a strict XML decoder.
func checkWellFormed(t *testing.T, raw []byte) {
	t.Helper()
	if !utf8.Valid(raw) {
		t.Fatal("output is not valid UTF-8")
	}
	dec := xml.NewDecoder(bytes.NewReader(raw))
	for {
		_, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			t.Fatalf("output is not well-formed XML: %v", err)
		}
	}
}

func TestExportDeterministic(t *testing.T) {
	for _, f := range exportFormats {
		first := encodeXML(t, sampleDoc(t), f.ext)
		tr, fs := sampleFixture()
		fs.reverseEdges = true
		second := encodeXML(t, build(t, tr, fs, Options{Similar: true}), f.ext)
		if !bytes.Equal(first, second) {
			t.Errorf("%s output depends on store row order", f.ext)
		}
	}
}

// flattenJSON flattens a JSON object the way the exporters name
// attributes, dropping empty strings (absent values).
func flattenJSON(prefix string, m map[string]any, out map[string]any) {
	for k, v := range m {
		name := k
		if prefix != "" {
			name = prefix + "_" + k
		}
		switch x := v.(type) {
		case map[string]any:
			flattenJSON(name, x, out)
		case string:
			if x != "" {
				out[name] = x
			}
		default:
			if _, dup := out[name]; dup {
				panic("flattened name collision: " + name)
			}
			out[name] = x
		}
	}
}

// checkAttrs asserts that the exported values of one element are exactly
// the flattened JGF values, with matching types.
func checkAttrs[T any](t *testing.T, where string, attrs []attr[T], v T, want map[string]any) {
	t.Helper()
	types := map[string]attrType{}
	for _, a := range attrs {
		types[a.name] = a.typ
	}
	got, err := values(attrs, v)
	if err != nil {
		t.Fatalf("%s: %v", where, err)
	}
	gotNames := map[string]bool{}
	for _, av := range got {
		gotNames[av.name] = true
		jv, ok := want[av.name]
		if !ok {
			t.Errorf("%s: exported %s=%q is not in the JGF document", where, av.name, av.value)
			continue
		}
		switch x := jv.(type) {
		case string:
			if types[av.name] != attrString || av.value != x {
				t.Errorf("%s: %s = %q (%v), JGF %q", where, av.name, av.value, types[av.name], x)
			}
		case bool:
			if types[av.name] != attrBool || av.value != strconv.FormatBool(x) {
				t.Errorf("%s: %s = %q (%v), JGF %v", where, av.name, av.value, types[av.name], x)
			}
		case float64:
			f, err := strconv.ParseFloat(av.value, 64)
			if err != nil || f != x {
				t.Errorf("%s: %s = %q, JGF %v", where, av.name, av.value, x)
			}
			if types[av.name] == attrInt && x != math.Trunc(x) {
				t.Errorf("%s: %s is int but JGF has %v", where, av.name, x)
			}
			if types[av.name] != attrInt && types[av.name] != attrDouble {
				t.Errorf("%s: %s is not numeric", where, av.name)
			}
		default:
			t.Errorf("%s: %s has JGF type %T", where, av.name, jv)
		}
	}
	for name := range want {
		if !gotNames[name] {
			t.Errorf("%s: JGF value %s is not exported", where, name)
		}
	}
}

// TestExportAttributesMatchJGF guards against contract drift: for every
// graph, node and edge of the sample, the exported attribute set equals
// the flattened JGF metadata, value for value and with matching types.
func TestExportAttributesMatchJGF(t *testing.T) {
	doc := sampleDoc(t)
	var raw bytes.Buffer
	if err := doc.Encode(&raw, false); err != nil {
		t.Fatal(err)
	}
	var jd struct {
		Graph struct {
			Type     string                    `json:"type"`
			Label    string                    `json:"label"`
			Metadata map[string]any            `json:"metadata"`
			Nodes    map[string]map[string]any `json:"nodes"`
			Edges    []map[string]any          `json:"edges"`
		} `json:"graph"`
	}
	if err := json.Unmarshal(raw.Bytes(), &jd); err != nil {
		t.Fatal(err)
	}

	gw := map[string]any{"type": jd.Graph.Type, "label": jd.Graph.Label}
	flattenJSON("", jd.Graph.Metadata, gw)
	checkAttrs(t, "graph", graphAttrs, &doc.Graph, gw)

	for id, jn := range jd.Graph.Nodes {
		nw := map[string]any{"label": jn["label"]}
		flattenJSON("", jn["metadata"].(map[string]any), nw)
		checkAttrs(t, "node "+id, graphMLNodeAttrs, doc.Graph.Nodes[id], nw)
	}
	for i, je := range jd.Graph.Edges {
		ew := map[string]any{"relation": je["relation"], "directed": je["directed"]}
		flattenJSON("", je["metadata"].(map[string]any), ew)
		checkAttrs(t, "edge "+strconv.Itoa(i), graphMLEdgeAttrs, doc.Graph.Edges[i], ew)
	}
}

// TestExportAttributeNamesUnique checks the flattened names cannot
// collide within a domain.
func TestExportAttributeNamesUnique(t *testing.T) {
	check := func(domain string, names []string) {
		seen := map[string]bool{}
		for _, n := range names {
			if seen[n] {
				t.Errorf("%s: duplicate attribute %s", domain, n)
			}
			seen[n] = true
		}
	}
	names := func(n int, get func(int) string) []string {
		out := make([]string, n)
		for i := range n {
			out[i] = get(i)
		}
		return out
	}
	check("graph", names(len(graphAttrs), func(i int) string { return graphAttrs[i].name }))
	check("node", names(len(graphMLNodeAttrs), func(i int) string { return graphMLNodeAttrs[i].name }))
	check("edge", names(len(graphMLEdgeAttrs), func(i int) string { return graphMLEdgeAttrs[i].name }))
}

func TestExportNonFiniteFails(t *testing.T) {
	for _, f := range exportFormats {
		for name, mut := range map[string]func(*Document){
			"score": func(d *Document) {
				n := d.Graph.Nodes["obj:kb-rrf"]
				s := *n.Metadata.Score
				s.Total = math.NaN()
				n.Metadata.Score = &s
				d.Graph.Nodes["obj:kb-rrf"] = n
			},
			"weight": func(d *Document) {
				w := math.Inf(1)
				d.Graph.Edges[0].Metadata.Weight = &w
			},
		} {
			doc := sampleDoc(t)
			mut(doc)
			var buf bytes.Buffer
			err := f.encode(doc, &buf)
			if !errors.Is(err, errNonFinite) {
				t.Errorf("%s %s: err = %v, want non-finite error", f.ext, name, err)
			}
			if buf.Len() != 0 {
				t.Errorf("%s %s: wrote %d bytes before failing", f.ext, name, buf.Len())
			}
		}
	}
}

func TestFormatFloatMatchesJSON(t *testing.T) {
	for _, f := range []float64{0, 1, -1, 0.1, 0.016, 1e-7, 1.5e-9, 123456789, 1e21, 2.5e22, -7.25, 0.8333333333333334} {
		got, err := formatFloat(f)
		if err != nil {
			t.Fatal(err)
		}
		want, _ := json.Marshal(f)
		if got != string(want) {
			t.Errorf("formatFloat(%v) = %s, json %s", f, got, want)
		}
	}
}

// decodeGraphMLID inverts GraphMLID.
func decodeGraphMLID(t *testing.T, s string) string {
	t.Helper()
	var out []byte
	for i := 0; i < len(s); i++ {
		if s[i] != '.' {
			out = append(out, s[i])
			continue
		}
		if i+3 > len(s) {
			t.Fatalf("truncated escape in %q", s)
		}
		b, err := strconv.ParseUint(s[i+1:i+3], 16, 8)
		if err != nil {
			t.Fatalf("bad escape in %q: %v", s, err)
		}
		out = append(out, byte(b))
		i += 2
	}
	return string(out)
}

func TestGraphMLID(t *testing.T) {
	cases := map[string]string{
		"query":          "query",
		"obj:kb-rrf":     "obj:kb-rrf",
		"obj:01J_ABC-9":  "obj:01J_ABC-9",
		"ent:search/rrf": "ent:search.2Frrf",
		"a.b":            "a.2Eb",
		"a b":            "a.20b",
		"é":              ".C3.A9",
		hostileID:        "ent:we.3Ci.3Erd.26.22sl.20ug.27.2Fx.2Ey",
	}
	for in, want := range cases {
		got := GraphMLID(in)
		if got != want {
			t.Errorf("GraphMLID(%q) = %q, want %q", in, got, want)
		}
		if back := decodeGraphMLID(t, got); back != in {
			t.Errorf("decode(%q) = %q, want %q", got, back, in)
		}
	}
}

// graphML mirrors the GraphML elements the exporter writes.
type graphML struct {
	XMLName xml.Name `xml:"http://graphml.graphdrawing.org/xmlns graphml"`
	Keys    []struct {
		ID   string `xml:"id,attr"`
		For  string `xml:"for,attr"`
		Name string `xml:"attr.name,attr"`
		Type string `xml:"attr.type,attr"`
	} `xml:"key"`
	Graph struct {
		ID          string        `xml:"id,attr"`
		EdgeDefault string        `xml:"edgedefault,attr"`
		Data        []graphMLData `xml:"data"`
		Nodes       []struct {
			ID   string        `xml:"id,attr"`
			Data []graphMLData `xml:"data"`
		} `xml:"node"`
		Edges []struct {
			ID       string        `xml:"id,attr"`
			Source   string        `xml:"source,attr"`
			Target   string        `xml:"target,attr"`
			Directed *string       `xml:"directed,attr"`
			Data     []graphMLData `xml:"data"`
		} `xml:"edge"`
	} `xml:"graph"`
}

type graphMLData struct {
	Key   string `xml:"key,attr"`
	Value string `xml:",chardata"`
}

func dataMap(ds []graphMLData) map[string]string {
	m := make(map[string]string, len(ds))
	for _, d := range ds {
		m[d.Key] = d.Value
	}
	return m
}

func parseGraphML(t *testing.T, raw []byte) *graphML {
	t.Helper()
	var g graphML
	if err := xml.Unmarshal(raw, &g); err != nil {
		t.Fatalf("parse graphml: %v", err)
	}
	return &g
}

func TestGraphMLStructure(t *testing.T) {
	doc := hostileDoc(t)
	g := parseGraphML(t, encodeXML(t, doc, extGraphML))
	checkGraphMLKeys(t, g)
	checkGraphMLGraph(t, g)
	checkGraphMLNodes(t, g, doc)
	checkGraphMLEdges(t, g, doc)
}

func checkGraphMLKeys(t *testing.T, g *graphML) {
	t.Helper()
	keyType := map[string]string{}
	for _, k := range g.Keys {
		if _, dup := keyType[k.ID]; dup {
			t.Errorf("duplicate key id %s", k.ID)
		}
		keyType[k.ID] = k.For + ":" + k.Type
		if !strings.HasSuffix(k.ID, "_"+k.Name) {
			t.Errorf("key %s: id does not end in its name %s", k.ID, k.Name)
		}
	}
	for id, want := range map[string]string{
		"g_truncated": "graph:boolean", "g_counts_nodes": "graph:int", "g_threshold": "graph:double",
		"g_query": "graph:string", "g_weights_mention_boost": "graph:double",
		"n_label": "node:string", "n_rank": "node:int", "n_score_total": "node:double",
		"n_fts_raw": "node:double", "n_mention_count": "node:int", "n_kind": "node:string",
		"e_directed": "edge:boolean", "e_relation": "edge:string", "e_weight": "edge:double",
		"e_derivation": "edge:string",
	} {
		if keyType[id] != want {
			t.Errorf("key %s = %q, want %q", id, keyType[id], want)
		}
	}
	if n := len(g.Keys); n != len(graphAttrs)+len(graphMLNodeAttrs)+len(graphMLEdgeAttrs) {
		t.Errorf("declared %d keys", n)
	}
}

func checkGraphMLGraph(t *testing.T, g *graphML) {
	t.Helper()
	if g.Graph.EdgeDefault != "directed" || g.Graph.ID != GraphID {
		t.Errorf("graph id/edgedefault = %s/%s", g.Graph.ID, g.Graph.EdgeDefault)
	}
	gd := dataMap(g.Graph.Data)
	if gd["g_query"] != hostileWant || gd["g_label"] != hostileWant {
		t.Errorf("graph query/label = %q / %q", gd["g_query"], gd["g_label"])
	}
	if _, has := gd["g_vector_error"]; has {
		t.Error("absent vector_error written")
	}
}

func checkGraphMLNodes(t *testing.T, g *graphML, doc *Document) {
	t.Helper()
	if len(g.Graph.Nodes) != len(doc.Graph.Nodes) {
		t.Fatalf("nodes = %d, want %d", len(g.Graph.Nodes), len(doc.Graph.Nodes))
	}
	for _, n := range g.Graph.Nodes {
		jid := decodeGraphMLID(t, n.ID)
		jn, ok := doc.Graph.Nodes[jid]
		if !ok {
			t.Errorf("node %s (%s) not in document", n.ID, jid)
			continue
		}
		d := dataMap(n.Data)
		want := jn.Label
		if want == hostileText {
			want = hostileWant
		}
		if d["n_label"] != want || d["n_kind"] != jn.Metadata.Kind {
			t.Errorf("node %s: label %q kind %q", n.ID, d["n_label"], d["n_kind"])
		}
		for k, v := range d {
			if v == "" {
				t.Errorf("node %s: empty value for %s", n.ID, k)
			}
		}
	}
}

func checkGraphMLEdges(t *testing.T, g *graphML, doc *Document) {
	t.Helper()
	if len(g.Graph.Edges) != len(doc.Graph.Edges) {
		t.Fatalf("edges = %d, want %d", len(g.Graph.Edges), len(doc.Graph.Edges))
	}
	undirected := 0
	for i, e := range g.Graph.Edges {
		je := doc.Graph.Edges[i]
		d := dataMap(e.Data)
		if e.Directed != nil {
			t.Errorf("edge %s: native directed attribute %q (breaks NetworkX)", e.ID, *e.Directed)
		}
		if e.ID != edgeID(i) || e.Source != GraphMLID(je.Source) || e.Target != GraphMLID(je.Target) {
			t.Errorf("edge %d: id/source/target %s %s %s", i, e.ID, e.Source, e.Target)
		}
		if d["e_relation"] != je.Relation || d["e_directed"] != strconv.FormatBool(je.Directed) ||
			d["e_derivation"] != je.Metadata.Derivation {
			t.Errorf("edge %d: data %v, want %s directed=%v", i, d, je.Relation, je.Directed)
		}
		if _, has := d["e_weight"]; has != (je.Metadata.Weight != nil) {
			t.Errorf("edge %d: weight present = %v", i, has)
		}
		if !je.Directed {
			undirected++
		}
	}
	if undirected == 0 {
		t.Error("fixture has no undirected edge")
	}
}

// gexfDoc mirrors the GEXF elements the exporter writes.
type gexfDoc struct {
	XMLName xml.Name `xml:"http://gexf.net/1.3 gexf"`
	Version string   `xml:"version,attr"`
	Meta    struct {
		LastModified string `xml:"lastmodifieddate,attr"`
		Creator      string `xml:"creator"`
		Keywords     string `xml:"keywords"`
		Description  string `xml:"description"`
	} `xml:"meta"`
	Graph struct {
		Mode            string `xml:"mode,attr"`
		DefaultEdgeType string `xml:"defaultedgetype,attr"`
		Attributes      []struct {
			Class string `xml:"class,attr"`
			Attrs []struct {
				ID    string `xml:"id,attr"`
				Title string `xml:"title,attr"`
				Type  string `xml:"type,attr"`
			} `xml:"attribute"`
		} `xml:"attributes"`
		Nodes []struct {
			ID        string      `xml:"id,attr"`
			Label     *string     `xml:"label,attr"`
			Attvalues []gexfValue `xml:"attvalues>attvalue"`
			Color     *struct {
				R int `xml:"r,attr"`
				G int `xml:"g,attr"`
				B int `xml:"b,attr"`
			} `xml:"http://gexf.net/1.3/viz color"`
			Size *struct {
				Value float64 `xml:"value,attr"`
			} `xml:"http://gexf.net/1.3/viz size"`
		} `xml:"nodes>node"`
		Edges []struct {
			ID        string      `xml:"id,attr"`
			Source    string      `xml:"source,attr"`
			Target    string      `xml:"target,attr"`
			Type      string      `xml:"type,attr"`
			Label     string      `xml:"label,attr"`
			Kind      string      `xml:"kind,attr"`
			Weight    *string     `xml:"weight,attr"`
			Attvalues []gexfValue `xml:"attvalues>attvalue"`
		} `xml:"edges>edge"`
	} `xml:"graph"`
}

type gexfValue struct {
	For   string `xml:"for,attr"`
	Value string `xml:"value,attr"`
}

func TestGEXFStructure(t *testing.T) {
	doc := hostileDoc(t)
	var g gexfDoc
	if err := xml.Unmarshal(encodeXML(t, doc, extGEXF), &g); err != nil {
		t.Fatalf("parse gexf: %v", err)
	}
	checkGEXFHeader(t, &g, doc)
	checkGEXFAttributes(t, &g)
	checkGEXFNodes(t, &g, doc)
	checkGEXFEdges(t, &g, doc)
}

func checkGEXFHeader(t *testing.T, g *gexfDoc, doc *Document) {
	t.Helper()
	if g.Version != "1.3" || g.Graph.DefaultEdgeType != "directed" || g.Graph.Mode != "static" {
		t.Errorf("header version/defaultedgetype/mode = %s/%s/%s", g.Version, g.Graph.DefaultEdgeType, g.Graph.Mode)
	}

	// meta carries graph.metadata verbatim.
	if g.Meta.Creator != "ctxt" || g.Meta.Keywords != Vocabulary || g.Meta.LastModified != "2026-09-26" {
		t.Errorf("meta = %+v", g.Meta)
	}
	var md GraphMetadata
	if err := json.Unmarshal([]byte(g.Meta.Description), &md); err != nil {
		t.Fatalf("meta description is not JSON: %v", err)
	}
	wantMD := doc.Graph.Metadata
	wantMD.Query = hostileJSONWant
	if !reflect.DeepEqual(md, wantMD) {
		t.Errorf("meta description = %+v\nwant %+v", md, wantMD)
	}
}

func checkGEXFAttributes(t *testing.T, g *gexfDoc) {
	t.Helper()
	declared := map[string]string{}
	for _, as := range g.Graph.Attributes {
		for _, a := range as.Attrs {
			declared[as.Class+":"+a.ID] = a.Title + ":" + a.Type
		}
	}
	for id, want := range map[string]string{
		"node:n_rank": "rank:integer", "node:n_score_total": "score_total:double",
		"node:n_kind": "kind:string", "node:n_mention_count": "mention_count:integer",
		"node:n_vector_raw": "vector_raw:double", "edge:e_derivation": "derivation:string",
	} {
		if declared[id] != want {
			t.Errorf("attribute %s = %q, want %q", id, declared[id], want)
		}
	}
	if len(declared) != len(nodeAttrs)+len(gexfEdgeAttrs) {
		t.Errorf("declared %d attributes", len(declared))
	}
}

func checkGEXFNodes(t *testing.T, g *gexfDoc, doc *Document) {
	t.Helper()
	maxTotal := 0.0
	for _, n := range doc.Graph.Nodes {
		if s := n.Metadata.Score; s != nil {
			maxTotal = max(maxTotal, s.Total)
		}
	}
	if len(g.Graph.Nodes) != len(doc.Graph.Nodes) {
		t.Fatalf("nodes = %d, want %d", len(g.Graph.Nodes), len(doc.Graph.Nodes))
	}
	sawMax := false
	for _, n := range g.Graph.Nodes {
		jn, ok := doc.Graph.Nodes[n.ID]
		if !ok {
			t.Errorf("node %q not in document", n.ID)
			continue
		}
		want := jn.Label
		if want == hostileText {
			want = hostileWant
		}
		if n.Label == nil || *n.Label != want {
			t.Errorf("node %s: label %v, want %q", n.ID, n.Label, want)
		}
		if n.Color == nil || n.Size == nil {
			t.Errorf("node %s: missing viz color/size", n.ID)
			continue
		}
		if c := Palette.nodeColor(jn); n.Color.R != int(c.R) || n.Color.G != int(c.G) || n.Color.B != int(c.B) {
			t.Errorf("node %s: color %+v, want %s", n.ID, *n.Color, c.Hex())
		}
		checkGEXFNodeSize(t, n.ID, jn, n.Size.Value, maxTotal, &sawMax)
		kind := ""
		for _, av := range n.Attvalues {
			if av.Value == "" {
				t.Errorf("node %s: empty value for %s", n.ID, av.For)
			}
			if av.For == "n_kind" {
				kind = av.Value
			}
		}
		if kind != jn.Metadata.Kind {
			t.Errorf("node %s: kind %q", n.ID, kind)
		}
	}
	if !sawMax {
		t.Error("no object node has the maximum size")
	}
}

func checkGEXFEdges(t *testing.T, g *gexfDoc, doc *Document) {
	t.Helper()
	if len(g.Graph.Edges) != len(doc.Graph.Edges) {
		t.Fatalf("edges = %d, want %d", len(g.Graph.Edges), len(doc.Graph.Edges))
	}
	undirected := 0
	for i, e := range g.Graph.Edges {
		je := doc.Graph.Edges[i]
		wantType := "directed"
		if !je.Directed {
			wantType = "undirected"
			undirected++
		}
		if e.ID != edgeID(i) || e.Source != je.Source || e.Target != je.Target || e.Type != wantType {
			t.Errorf("edge %d: %s %s->%s type %s, want %s->%s %s", i, e.ID, e.Source, e.Target, e.Type,
				je.Source, je.Target, wantType)
		}
		if e.Label != je.Relation || e.Kind != je.Relation {
			t.Errorf("edge %d: label/kind %s/%s, want %s", i, e.Label, e.Kind, je.Relation)
		}
		if (e.Weight != nil) != (je.Metadata.Weight != nil) {
			t.Errorf("edge %d: weight %v", i, e.Weight)
		} else if e.Weight != nil {
			if w, _ := formatFloat(*je.Metadata.Weight); *e.Weight != w {
				t.Errorf("edge %d: weight %s, want %s", i, *e.Weight, w)
			}
		}
		if len(e.Attvalues) != 1 || e.Attvalues[0].For != "e_derivation" || e.Attvalues[0].Value != je.Metadata.Derivation {
			t.Errorf("edge %d: attvalues %+v", i, e.Attvalues)
		}
	}
	if undirected == 0 {
		t.Error("fixture has no undirected edge")
	}
}

func checkGEXFNodeSize(t *testing.T, id string, n Node, size, maxTotal float64, sawMax *bool) {
	t.Helper()
	p := Palette
	switch n.Metadata.Kind {
	case KindQuery:
		if size != p.QuerySize {
			t.Errorf("query size %v", size)
		}
	case KindEntity:
		if size != p.EntitySize {
			t.Errorf("entity %s size %v", id, size)
		}
	default:
		if size < p.ObjectMinSize || size > p.ObjectMaxSize {
			t.Errorf("object %s size %v outside [%v, %v]", id, size, p.ObjectMinSize, p.ObjectMaxSize)
		}
		if n.Metadata.Score != nil && n.Metadata.Score.Total == maxTotal {
			*sawMax = *sawMax || size == p.ObjectMaxSize
		}
	}
}

func TestVizPaletteSizes(t *testing.T) {
	p := Palette
	obj := func(total float64) Node {
		return Node{Metadata: NodeMetadata{Kind: KindObject, Score: &Score{Total: total}}}
	}
	for _, tc := range []struct {
		n     Node
		max   float64
		want  float64
		label string
	}{
		{obj(1), 1, p.ObjectMaxSize, "top"},
		{obj(0.5), 1, (p.ObjectMinSize + p.ObjectMaxSize) / 2, "half"},
		{obj(0), 1, p.ObjectMinSize, "zero"},
		{obj(-1), 1, p.ObjectMinSize, "negative"},
		{obj(0.3), 0, p.ObjectMinSize, "no positive max"},
		{Node{Metadata: NodeMetadata{Kind: KindObject}}, 1, p.ObjectMinSize, "no score"},
		{Node{Metadata: NodeMetadata{Kind: KindQuery}}, 1, p.QuerySize, "query"},
		{Node{Metadata: NodeMetadata{Kind: KindEntity}}, 1, p.EntitySize, "entity"},
	} {
		if got := p.nodeSize(tc.n, tc.max); got != tc.want {
			t.Errorf("%s: size %v, want %v", tc.label, got, tc.want)
		}
	}
	stages := map[string]RGB{"returned": p.Returned, "cut_limit": p.CutLimit, "cut_threshold": p.CutThreshold, "bogus": p.Unknown}
	for stage, want := range stages {
		if got := p.nodeColor(Node{Metadata: NodeMetadata{Kind: KindObject, Stage: stage}}); got != want {
			t.Errorf("stage %s: color %s, want %s", stage, got.Hex(), want.Hex())
		}
	}
	colors := []RGB{p.Query, p.Entity, p.Returned, p.CutLimit, p.CutThreshold, p.Unknown}
	for i, c := range colors {
		if slices.Contains(colors[i+1:], c) {
			t.Errorf("palette color %s used twice", c.Hex())
		}
	}
}

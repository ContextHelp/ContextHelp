package viewer

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Budget for the gzip-compressed bundle. The 3D renderer, the 2D fallback
// and three.js (with the WebGPU renderer stubbed out) measure ~245 KB.
const gzipBudget = 300 * 1024

var fixturePath = filepath.Join("..", "..", "..", "web", "searchgraph", "fixtures", "sample.jgf.json")

func readFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return b
}

func TestAssetsPresent(t *testing.T) {
	assets := Assets()
	for _, name := range []string{IndexFile, ScriptFile, StyleFile, NoticesFile} {
		b, err := fs.ReadFile(assets, name)
		if err != nil {
			t.Fatalf("embedded %s: %v", name, err)
		}
		if len(b) == 0 {
			t.Fatalf("embedded %s is empty", name)
		}
	}
	if _, err := fs.Stat(assets, DataFile); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("%s must be supplied by the caller, not embedded (stat err=%v)", DataFile, err)
	}

	page, _ := fs.ReadFile(assets, IndexFile)
	for _, frag := range []string{cspServed, styleLink, noticesLink, scriptSrcTag} {
		if n := strings.Count(string(page), frag); n != 1 {
			t.Errorf("%s: want exactly one %q, found %d", IndexFile, frag, n)
		}
	}

	notices, _ := fs.ReadFile(assets, NoticesFile)
	for _, pkg := range []string{"3d-force-graph@1.80.0", "force-graph@1.51.4", "three@0.186.1"} {
		if !strings.Contains(string(notices), pkg) {
			t.Errorf("%s does not list %s", NoticesFile, pkg)
		}
	}
	script, _ := fs.ReadFile(assets, ScriptFile)
	if !strings.Contains(string(script), NoticesFile) {
		t.Errorf("%s does not link %s", ScriptFile, NoticesFile)
	}
}

func TestBundleSizeBudget(t *testing.T) {
	script, err := fs.ReadFile(Assets(), ScriptFile)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if _, err := zw.Write(script); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: %d bytes, %d gzipped (budget %d)", ScriptFile, len(script), buf.Len(), gzipBudget)
	if buf.Len() > gzipBudget {
		t.Fatalf("%s gzipped is %d bytes, over the %d byte budget", ScriptFile, buf.Len(), gzipBudget)
	}
}

func TestStandaloneEmbedsDocument(t *testing.T) {
	doc := readFixture(t)
	out, err := Standalone(doc)
	if err != nil {
		t.Fatalf("Standalone: %v", err)
	}
	p := parsePage(t, out)

	if got := len(p.scripts); got != 2 {
		t.Fatalf("want 2 script elements (data + viewer), got %d", got)
	}
	assertSameJSON(t, p.data, doc)

	if p.links != 0 {
		t.Errorf("standalone page still links %d external resources", p.links)
	}
	if p.styles != 1 {
		t.Errorf("want 1 inline style element, got %d", p.styles)
	}
	if !strings.Contains(p.notices, "three@0.186.1") {
		t.Errorf("inline notices missing three.js license")
	}

	sum := sha256.Sum256([]byte(p.viewer))
	hash := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
	if !strings.Contains(p.csp, "script-src "+hash) {
		t.Errorf("CSP %q does not allow the inline viewer script by hash %s", p.csp, hash)
	}
	if !strings.Contains(p.csp, "connect-src 'none'") {
		t.Errorf("CSP %q allows network access", p.csp)
	}
	script, _ := fs.ReadFile(Assets(), ScriptFile)
	if p.viewer != string(script) {
		t.Errorf("inline viewer script differs from embedded %s", ScriptFile)
	}
}

func TestStandaloneHostileStrings(t *testing.T) {
	hostile := []string{
		`</script><script>alert(1)</script>`,
		`</SCRIPT ><script>alert(2)</script>`,
		`<!--<script>alert(3)`,
		`<script>alert(4)</script>`,
		"line\u2028separator\u2029",
		`& &amp; " ' \`,
	}
	nodes := map[string]any{"query": map[string]any{"label": hostile[0], "metadata": map[string]any{"kind": "query"}}}
	for i, s := range hostile {
		nodes["obj:"+s] = map[string]any{
			"label":    s,
			"metadata": map[string]any{"kind": "object", "object_id": s, "stage": "returned", "rank": i + 1},
		}
	}
	doc, err := json.Marshal(map[string]any{"graph": map[string]any{
		"label":    hostile[0],
		"directed": true,
		"metadata": map[string]any{"vocabulary": "ctxt.search-graph/v1", "query": hostile[0]},
		"nodes":    nodes,
		"edges":    []any{},
	}})
	if err != nil {
		t.Fatal(err)
	}
	// json.Marshal escapes <, >, & and U+2028/2029 itself; undo that so the
	// input carries the raw characters a non-Go producer could emit.
	doc = []byte(strings.NewReplacer(
		`\u003c`, "<", `\u003e`, ">", `\u0026`, "&", `\u2028`, "\u2028", `\u2029`, "\u2029",
	).Replace(string(doc)))
	if !bytes.Contains(doc, []byte("</script><script>alert(1)")) {
		t.Fatal("test input lost its raw hostile label")
	}

	out, err := Standalone(doc)
	if err != nil {
		t.Fatalf("Standalone: %v", err)
	}
	p := parsePage(t, out)
	if got := len(p.scripts); got != 2 {
		t.Fatalf("hostile labels changed the script element count: want 2, got %d", got)
	}
	for _, s := range p.scripts {
		if strings.Contains(s, "alert(") && s != p.data {
			t.Fatalf("hostile label leaked into an executable script: %.80q", s)
		}
	}
	assertSameJSON(t, p.data, doc)
}

func TestStandaloneRejectsInvalidJSON(t *testing.T) {
	for _, in := range []string{"", "{", `{"graph":}`, "</script>"} {
		if _, err := Standalone([]byte(in)); !errors.Is(err, ErrInvalidDocument) {
			t.Errorf("Standalone(%q): want ErrInvalidDocument, got %v", in, err)
		}
	}
}

func TestFixtureFollowsContract(t *testing.T) {
	var doc struct {
		Graph struct {
			Metadata struct {
				Vocabulary string `json:"vocabulary"`
			} `json:"metadata"`
			Nodes map[string]struct {
				Label    string         `json:"label"`
				Metadata map[string]any `json:"metadata"`
			} `json:"nodes"`
			Edges []struct {
				Source   string `json:"source"`
				Target   string `json:"target"`
				Relation string `json:"relation"`
			} `json:"edges"`
		} `json:"graph"`
	}
	if err := json.Unmarshal(readFixture(t), &doc); err != nil {
		t.Fatal(err)
	}
	g := doc.Graph
	if g.Metadata.Vocabulary != "ctxt.search-graph/v1" {
		t.Fatalf("vocabulary = %q", g.Metadata.Vocabulary)
	}
	seen := map[string]bool{}
	for id, n := range g.Nodes {
		kind, _ := n.Metadata["kind"].(string)
		seen["kind:"+kind] = true
		if kind == "object" {
			stage, _ := n.Metadata["stage"].(string)
			seen["stage:"+stage] = true
		}
		prefix := map[string]string{"query": "query", "object": "obj:", "entity": "ent:"}[kind]
		if prefix == "" || !strings.HasPrefix(id, prefix) {
			t.Errorf("node %q: kind %q does not match id prefix", id, kind)
		}
	}
	for _, e := range g.Edges {
		seen["rel:"+e.Relation] = true
		if _, ok := g.Nodes[e.Source]; !ok {
			t.Errorf("edge source %q is not a node", e.Source)
		}
		if _, ok := g.Nodes[e.Target]; !ok {
			t.Errorf("edge target %q is not a node", e.Target)
		}
	}
	for _, want := range []string{
		"kind:query", "kind:object", "kind:entity",
		"stage:returned", "stage:cut_limit", "stage:cut_threshold",
		"rel:matched", "rel:mentions", "rel:extends", "rel:contradicts", "rel:supersedes",
		"rel:supports", "rel:related-to", "rel:derived-from", "rel:co_mention", "rel:similar",
	} {
		if !seen[want] {
			t.Errorf("fixture lacks %s", want)
		}
	}
}

type page struct {
	scripts []string // text of every <script>, in order
	data    string   // text of script#graph-data
	viewer  string   // text of the executable inline script
	csp     string
	notices string
	links   int // non-data: <link> or src= references
	styles  int
}

func parsePage(t *testing.T, b []byte) page {
	t.Helper()
	root, err := html.Parse(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("parse standalone html: %v", err)
	}
	var p page
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			attr := map[string]string{}
			for _, a := range n.Attr {
				attr[a.Key] = a.Val
			}
			switch n.DataAtom {
			case atom.Script:
				text := textOf(n)
				p.scripts = append(p.scripts, text)
				switch {
				case attr["id"] == "graph-data" && attr["type"] == "application/json":
					p.data = text
				case attr["type"] == "":
					p.viewer = text
				}
			case atom.Meta:
				if attr["http-equiv"] == "Content-Security-Policy" {
					p.csp = attr["content"]
				}
			case atom.Link:
				if !strings.HasPrefix(attr["href"], "data:") {
					p.links++
				}
			case atom.Style:
				p.styles++
			case atom.Details:
				if attr["id"] == "notices" {
					p.notices = textOf(n)
				}
			}
			if _, ok := attr["src"]; ok {
				p.links++
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return p
}

func textOf(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return sb.String()
}

func assertSameJSON(t *testing.T, got string, want []byte) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal([]byte(got), &g); err != nil {
		t.Fatalf("inline graph data is not JSON: %v", err)
	}
	if err := json.Unmarshal(want, &w); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Fatal("inline graph data does not round-trip to the input document")
	}
}

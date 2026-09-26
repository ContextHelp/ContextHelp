package cmd

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/searchgraph"
	"github.com/ideacrafterslabs/ctxt/internal/searchgraph/viewer"
)

// xmlRoot returns the local name of an XML document's root element.
func xmlRoot(t *testing.T, data []byte) string {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	for {
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("no root element: %v", err)
		}
		if se, ok := tok.(xml.StartElement); ok {
			return se.Name.Local
		}
	}
}

// decodeJSON decodes a graph document into a generic value, for
// comparing two encodings of one document. generated_at is dropped: two
// runs a second apart differ there and nowhere else.
func decodeJSON(t *testing.T, data []byte) any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, data)
	}
	if g, ok := v["graph"].(map[string]any); ok {
		if md, ok := g["metadata"].(map[string]any); ok {
			delete(md, "generated_at")
		}
	}
	return v
}

// stripSemanticNotice returns stderr without its leading semantic-leg
// notice line, failing when the notice is missing: graph test databases
// have no working embedding provider, so every search prints it.
func stripSemanticNotice(t *testing.T, stderr string) string {
	t.Helper()
	const prefix = "notice: semantic search unavailable ("
	line, rest, ok := strings.Cut(stderr, "\n")
	if !ok || !strings.HasPrefix(line, prefix) {
		t.Errorf("stderr does not start with the semantic notice:\n%s", stderr)
		return stderr
	}
	return rest
}

var graphDataPattern = regexp.MustCompile(`(?s)<script type="application/json" id="graph-data">(.*?)</script>`)

// TestFindGraph_ExportByExtension asserts -o without --format writes the
// format its extension names, 0600, prints the path, and writes
// nothing to stdout.
func TestFindGraph_ExportByExtension(t *testing.T) {
	db := setupGraphTestDB(t)
	seedGraphObjects(t, db, map[string]string{"obj_graph_a": "deployment <runbook> & notes"}, nil)

	jgf, _, err := execFind(t, db, "--format", "json", "find", "deployment", "--graph")
	if err != nil {
		t.Fatalf("reference --format json: %v", err)
	}
	want := decodeJSON(t, []byte(jgf))

	cases := []struct {
		file, format string
		check        func(t *testing.T, data []byte)
	}{
		{"g.json", "JSON Graph Format", func(t *testing.T, data []byte) {
			graphDoc(t, string(data))
			if !reflect.DeepEqual(decodeJSON(t, data), want) {
				t.Error("JGF file differs from --format json output")
			}
		}},
		{"G.JSON", "JSON Graph Format", func(t *testing.T, data []byte) { graphDoc(t, string(data)) }},
		{"g.yaml", "YAML", func(t *testing.T, data []byte) {
			if !strings.HasPrefix(string(data), "graph:") || !strings.Contains(string(data), "vocabulary: "+searchgraph.Vocabulary) {
				t.Errorf("not the YAML graph document:\n%s", data)
			}
		}},
		{"g.yml", "YAML", func(t *testing.T, data []byte) {
			if !strings.HasPrefix(string(data), "graph:") {
				t.Errorf("not the YAML graph document:\n%s", data)
			}
		}},
		{"g.graphml", "GraphML", func(t *testing.T, data []byte) {
			if root := xmlRoot(t, data); root != "graphml" {
				t.Errorf("root element = %q, want graphml", root)
			}
		}},
		{"g.gexf", "GEXF", func(t *testing.T, data []byte) {
			if root := xmlRoot(t, data); root != "gexf" {
				t.Errorf("root element = %q, want gexf", root)
			}
		}},
		{"g.html", "standalone HTML viewer", func(t *testing.T, data []byte) {
			m := graphDataPattern.FindSubmatch(data)
			if m == nil {
				t.Fatalf("no graph-data element in page")
			}
			if !reflect.DeepEqual(decodeJSON(t, m[1]), want) {
				t.Error("inlined document differs from --format json output")
			}
			if strings.Contains(string(m[1]), "<runbook>") {
				t.Error("inlined document is not HTML-escaped")
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.file)
			stdout, stderr, err := execFind(t, db, "find", "deployment", "--graph", "-o", path)
			if err != nil {
				t.Fatalf("find --graph -o %s: %v\n%s", tc.file, err, stderr)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want nothing", stdout)
			}
			if want := "Wrote search graph (" + tc.format + ") to " + path + "\n"; stripSemanticNotice(t, stderr) != want {
				t.Errorf("stderr = %q, want the semantic notice then only %q", stderr, want)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat: %v", err)
			}
			if perm := info.Mode().Perm(); perm != 0o600 {
				t.Errorf("mode = %o, want 600", perm)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			tc.check(t, data)
		})
	}
}

// TestFindGraph_ExportRefusalWritesNothing asserts a refused -o path is
// never created.
func TestFindGraph_ExportRefusalWritesNothing(t *testing.T) {
	db := setupGraphTestDB(t)
	for _, name := range []string{"g.txt", "graph", "g.svg"} {
		path := filepath.Join(t.TempDir(), name)
		if _, err := db.exec("find", "deployment", "--graph", "-o", path); err == nil {
			t.Errorf("-o %s: want usage error", name)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("-o %s: file exists after refusal (%v)", name, err)
		}
	}
}

// TestFindGraph_StructuredOutputFile keeps the existing contract: an
// explicit --format writes that format to -o, whatever the extension,
// unless the extension names a different graph format.
func TestFindGraph_StructuredOutputFile(t *testing.T) {
	db := setupGraphTestDB(t)
	seedGraphObjects(t, db, map[string]string{"obj_graph_a": "deployment notes"}, nil)

	for _, name := range []string{"g.json", "g.out"} {
		path := filepath.Join(t.TempDir(), name)
		stdout, stderr, err := execFind(t, db, "--format", "json", "find", "deployment", "--graph", "-o", path)
		if err != nil {
			t.Fatalf("--format json -o %s: %v", name, err)
		}
		if stdout != "" || stripSemanticNotice(t, stderr) != "" {
			t.Errorf("-o %s: stdout/stderr = %q/%q, want nothing beyond the semantic notice", name, stdout, stderr)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		graphDoc(t, string(data))
	}
}

// swapGraphViewerSeams replaces the browser opener and terminal check
// for one test.
func swapGraphViewerSeams(t *testing.T, op urlOpener, terminal func(io.Writer) bool) {
	t.Helper()
	origOpener, origTerminal := graphViewerOpener, graphTerminal
	t.Cleanup(func() { graphViewerOpener, graphTerminal = origOpener, origTerminal })
	graphViewerOpener, graphTerminal = op, terminal
}

var printedViewerURL = regexp.MustCompile(`Search graph viewer: (http://127\.0\.0\.1:\d+/[A-Za-z0-9_-]{43}/)`)

// TestFindGraph_ViewerNonTTY asserts a captured run (as every in-process
// test run is) serves, prints the URL and how to stop, never opens a
// browser, and exits 0 once idle.
func TestFindGraph_ViewerNonTTY(t *testing.T) {
	op := &fakeOpener{}
	swapGraphViewerSeams(t, op, isTerminal)
	db := setupGraphTestDB(t)
	seedGraphObjects(t, db, map[string]string{"obj_graph_a": "deployment notes"}, nil)

	out, err := db.exec("find", "deployment", "--graph", "--graph-idle-timeout", "100ms")
	if err != nil {
		t.Fatalf("find --graph: %v\n%s", err, out)
	}
	if !printedViewerURL.MatchString(out) || !strings.Contains(out, "Ctrl-C") {
		t.Errorf("output lacks the viewer URL or how to stop:\n%s", out)
	}
	if !strings.Contains(out, "stopped (idle for 100ms)") {
		t.Errorf("output lacks the idle stop:\n%s", out)
	}
	if got := op.opened(); len(got) != 0 {
		t.Errorf("browser opened on a non-TTY run: %v", got)
	}
}

// TestFindGraph_ViewerTTY stands in for a terminal and a browser: the
// fake browser gets the printed URL and loads the page and the graph,
// which is the same document --format json emits.
func TestFindGraph_ViewerTTY(t *testing.T) {
	db := setupGraphTestDB(t)
	seedGraphObjects(t, db, map[string]string{"obj_graph_a": "deployment notes"}, nil)
	jgf, _, err := execFind(t, db, "--format", "json", "find", "deployment", "--graph")
	if err != nil {
		t.Fatalf("reference --format json: %v", err)
	}

	var index, graph []byte
	var indexCode int
	op := &fakeOpener{then: func(url string) {
		get := func(u string) (int, []byte) {
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return 0, nil
			}
			defer resp.Body.Close()
			b, _ := io.ReadAll(resp.Body)
			return resp.StatusCode, b
		}
		indexCode, index = get(url)
		_, graph = get(url + viewer.DataFile)
	}}
	swapGraphViewerSeams(t, op, func(io.Writer) bool { return true })

	out, err := db.exec("find", "deployment", "--graph", "--graph-idle-timeout", "100ms")
	if err != nil {
		t.Fatalf("find --graph: %v\n%s", err, out)
	}
	m := printedViewerURL.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no viewer URL in output:\n%s", out)
	}
	if got := op.opened(); len(got) != 1 || got[0] != m[1] {
		t.Fatalf("browser opened %v, want [%s]", got, m[1])
	}
	if indexCode != http.StatusOK || !strings.Contains(string(index), "<title>ctxt search graph</title>") {
		t.Errorf("index = %d:\n%.200s", indexCode, index)
	}
	if !reflect.DeepEqual(decodeJSON(t, graph), decodeJSON(t, []byte(jgf))) {
		t.Error("served graph.json differs from --format json output")
	}
}

// TestFindGraph_ViewerNoBrowserOnTTY asserts --no-browser wins over a
// terminal.
func TestFindGraph_ViewerNoBrowserOnTTY(t *testing.T) {
	op := &fakeOpener{}
	swapGraphViewerSeams(t, op, func(io.Writer) bool { return true })
	db := setupGraphTestDB(t)

	out, err := db.exec("find", "deployment", "--graph", "--no-browser", "--graph-idle-timeout", "100ms")
	if err != nil {
		t.Fatalf("find --graph --no-browser: %v\n%s", err, out)
	}
	if !printedViewerURL.MatchString(out) {
		t.Errorf("no viewer URL in output:\n%s", out)
	}
	if got := op.opened(); len(got) != 0 {
		t.Errorf("browser opened despite --no-browser: %v", got)
	}
}

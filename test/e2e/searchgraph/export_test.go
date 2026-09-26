//go:build e2e && unix

package searchgraph_test

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Every -o extension writes a 0600 file in its own format, carrying the
// same graph --format json prints.
func TestExport_ByExtension(t *testing.T) {
	e := newEnv(t)
	want := normalize(t, []byte(e.mustRun(args(stageSplitArgs, "--format", "json")...).stdout))

	cases := []struct {
		file, name string
		check      func(t *testing.T, body []byte)
	}{
		{"graph.json", "JSON Graph Format", func(t *testing.T, b []byte) { assertSameGraph(t, want, normalize(t, b)) }},
		{"graph.yaml", "YAML", func(t *testing.T, b []byte) { assertSameGraph(t, want, yamlGraph(t, b)) }},
		{"graph.yml", "YAML", func(t *testing.T, b []byte) { assertSameGraph(t, want, yamlGraph(t, b)) }},
		{"GRAPH.JSON", "JSON Graph Format", func(t *testing.T, b []byte) { assertSameGraph(t, want, normalize(t, b)) }},
		{"graph.graphml", "GraphML", func(t *testing.T, b []byte) {
			assertXMLGraph(t, b, "http://graphml.graphdrawing.org/xmlns", "graphml", want)
		}},
		{"graph.gexf", "GEXF", func(t *testing.T, b []byte) {
			assertXMLGraph(t, b, "http://gexf.net/1.3", "gexf", want)
		}},
		{"graph.html", "standalone HTML viewer", func(t *testing.T, b []byte) {
			assertSameGraph(t, want, normalize(t, inlineGraphData(t, b)))
			assertSelfContained(t, b)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.file)
			r := e.mustRun(args(stageSplitArgs, "-o", path)...)
			if r.stdout != "" {
				t.Errorf("stdout not empty with -o:\n%s", r.stdout)
			}
			// The corpus's default model has no reachable provider, so the
			// search prints the same fallback notice as plain find first.
			notice, rest, _ := strings.Cut(r.stderr, "\n")
			if !strings.HasPrefix(notice, "notice: semantic search unavailable (provider_error): model fixture-e2e-3") {
				t.Errorf("stderr lacks the semantic fallback notice first:\n%s", r.stderr)
			}
			if want := "Wrote search graph (" + tc.name + ") to " + path + "\n"; rest != want {
				t.Errorf("stderr after the notice = %q, want %q", rest, want)
			}
			assertMode0600(t, path)
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			tc.check(t, body)
		})
	}
}

// An explicit --format that agrees with the extension writes that format.
func TestExport_ExplicitFormatMatchingExtension(t *testing.T) {
	e := newEnv(t)
	want := normalize(t, []byte(e.mustRun(args(stageSplitArgs, "--format", "json")...).stdout))
	for _, tc := range []struct{ format, file string }{{"json", "g.json"}, {"yaml", "g.yml"}} {
		path := filepath.Join(t.TempDir(), tc.file)
		e.mustRun(args(stageSplitArgs, "--format", tc.format, "-o", path)...)
		assertMode0600(t, path)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		got := yamlGraph(t, body) // JSON is YAML
		assertSameGraph(t, want, got)
	}
}

func assertMode0600(t *testing.T, path string) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("%s mode = %v, want -rw-------", filepath.Base(path), fi.Mode().Perm())
	}
}

func assertSameGraph(t *testing.T, want, got jgf) {
	t.Helper()
	if !sameDoc(want, got) {
		t.Errorf("graph differs from --format json output\n--- want\n%s\n--- got\n%s", canonical(want), canonical(got))
	}
}

// yamlGraph decodes a YAML graph document and runs it through the same
// normalization as JSON output.
func yamlGraph(t *testing.T, b []byte) jgf {
	t.Helper()
	var v any
	if err := yaml.Unmarshal(b, &v); err != nil {
		t.Fatalf("parse YAML: %v\n%s", err, b)
	}
	j, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("YAML document is not JSON-shaped: %v", err)
	}
	return normalize(t, j)
}

// assertXMLGraph checks the file is well-formed XML with the expected
// root element and as many node and edge elements as the JGF document.
func assertXMLGraph(t *testing.T, b []byte, ns, root string, want jgf) {
	t.Helper()
	dec := xml.NewDecoder(bytes.NewReader(b))
	var gotRoot *xml.StartElement
	nodes, edges := 0, 0
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("malformed %s: %v", root, err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if gotRoot == nil {
			c := se.Copy()
			gotRoot = &c
		}
		if se.Name.Space != ns {
			continue
		}
		switch se.Name.Local {
		case "node":
			nodes++
		case "edge":
			edges++
		}
	}
	if gotRoot == nil || gotRoot.Name.Local != root || gotRoot.Name.Space != ns {
		t.Fatalf("root element = %v, want {%s}%s", gotRoot, ns, root)
	}
	if nodes != len(nodesOf(want)) || edges != len(edgesOf(want)) {
		t.Errorf("%s has %d nodes / %d edges, want %d / %d", root, nodes, edges, len(nodesOf(want)), len(edgesOf(want)))
	}
}

var graphDataScript = regexp.MustCompile(`(?s)<script type="application/json" id="graph-data">(.*?)</script>`)

// inlineGraphData returns the JSON inlined in a standalone viewer page.
func inlineGraphData(t *testing.T, page []byte) []byte {
	t.Helper()
	m := graphDataScript.FindAllSubmatch(page, -1)
	if len(m) != 1 {
		t.Fatalf("standalone page has %d graph-data elements, want 1", len(m))
	}
	return m[0][1]
}

var externalRef = regexp.MustCompile(`(?i)(?:src|href)\s*=\s*"(?:https?:)?//`)

// assertSelfContained checks the standalone page loads nothing: no
// script or stylesheet by URL, no network fetch allowed by its CSP.
func assertSelfContained(t *testing.T, page []byte) {
	t.Helper()
	if loc := externalRef.FindIndex(page); loc != nil {
		t.Errorf("standalone page references an external resource: %q", page[loc[0]:min(loc[1]+40, len(page))])
	}
	s := string(page)
	for _, ref := range []string{`src="viewer.js"`, `href="viewer.css"`} {
		if strings.Contains(s, ref) {
			t.Errorf("standalone page still references %s", ref)
		}
	}
	if !strings.Contains(s, "connect-src 'none'") {
		t.Error("standalone page CSP does not forbid network access (connect-src 'none')")
	}
}

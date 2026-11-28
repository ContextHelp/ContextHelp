package service

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	uri "hop.top/cite/scheme"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// invText is prose full of what a second pass of a cleaner or an
// extractor would eat or rewrite: generics, comparisons, escaped
// entities, a script tag named in prose, a hex id, tabs, CRLF, trailing
// blanks, a run of blank lines and a fenced block.
const invText = "# Notes\r\n\r\nWrite Vec<String> and a<b && c>d in prose.\ttabbed\tcolumns   \r\n" +
	"The text &lt;tag&gt; and AT&amp;T stay as written, and <script> is only a word here.   \r\n" +
	"Commit 0123456789abcdef0123456789abcdef01234567 fixed it.\n\n\n\n" +
	"```go\nfmt.Println(\"<b>\" + a<b)\n```\n\n" +
	"## Details\n\nThe numbat project tracks sightings near the dryandra woodland and records each one in a ledger kept weekly by the rangers."

// invPage is invText as a web page would serve it: entities encoded, with
// markup around it that a first fetch must clean.
const invPage = "<html><head><title>Notes</title><style>p{}</style></head><body><nav>menus</nav>" +
	"<h1>Notes</h1><p>Write Vec&lt;String&gt; and a&lt;b &amp;&amp; c&gt;d in prose.\ttabbed\tcolumns   \r\n" +
	"The text &amp;lt;tag&amp;gt; and AT&amp;amp;T stay as written, and &lt;script&gt; is only a word here.   \r\n" +
	"Commit 0123456789abcdef0123456789abcdef01234567 fixed it.</p>\n\n\n\n<pre>fmt.Println(\"&lt;b&gt;\" + a&lt;b)</pre>" +
	"<script>alert(1)</script><h2>Details</h2><p>The numbat project tracks sightings near the dryandra woodland and records each one in a ledger kept weekly by the rangers.</p></body></html>"

const invCode = "package main\n\n// Vec<String> and a<b && c>d, written for the rangers who keep the ledger of numbat sightings.\n" +
	"// The function below is a placeholder until the ledger format settles.\nfunc main() {\n\tprintln(\"a<b\")\n}\n"

// invPNG is a PNG header: binary content for the image pipeline, whose
// OCR is a fake.
var invPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00")

// invFixture is what one pipeline is first ingested from.
type invFixture struct {
	source      string
	payload     string
	binary      []byte // binary content, base64 on the job, original blob stored
	contentType string
}

// invFixtures names the first-ingest input of every re-derived pipeline.
var invFixtures = map[string]func(t *testing.T, e *realEnv) invFixture{
	"text.short": invTextFixture,
	"text.long":  invTextFixture,
	"doc.markdown": func(t *testing.T, _ *realEnv) invFixture {
		p := filepath.Join(t.TempDir(), "notes.md")
		require.NoError(t, os.WriteFile(p, []byte(invText), 0o600))
		return invFixture{source: p}
	},
	"doc.code": func(t *testing.T, _ *realEnv) invFixture {
		p := filepath.Join(t.TempDir(), "main.go")
		require.NoError(t, os.WriteFile(p, []byte(invCode), 0o600))
		return invFixture{source: p}
	},
	"image.ocr": func(t *testing.T, _ *realEnv) invFixture {
		return invFixture{source: filepath.Join(t.TempDir(), "scan.png"), binary: invPNG, contentType: "image/png"}
	},
	"url.generic":        invPageFixture,
	"url.github.issue":   invPageFixture,
	"url.github.pr":      invPageFixture,
	"url.github.profile": invPageFixture,
	"url.github.release": invPageFixture,
	"url.repo":           invPageFixture,
	"url.github.starred": invManifestPageFixture,
}

func invTextFixture(*testing.T, *realEnv) invFixture {
	return invFixture{source: "test", payload: invText}
}

func invPageFixture(_ *testing.T, e *realEnv) invFixture {
	e.page.Store(invPage)
	return invFixture{source: e.srv.URL + "/page"}
}

// invManifestPageFixture serves a page whose text is a pip manifest, so
// dependency_enricher has dependencies to record: the artifact nodes and
// the pending list a reanalysis must rewrite as ingest wrote them.
func invManifestPageFixture(_ *testing.T, e *realEnv) invFixture {
	e.page.Store("<html><body><pre>requests>=2.0.0\nflask==3.0.0\nclick>=8.0.0\n</pre></body></html>")
	return invFixture{source: e.srv.URL + "/page"}
}

func labelSet(tags []storage.Tag) []string {
	var out []string
	for _, t := range tags {
		out = append(out, t.Label+"/"+t.Source)
	}
	slices.Sort(out)
	return out
}

func mentionSet(ms []uri.URI) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.String())
	}
	slices.Sort(out)
	return out
}

func sectionSet(ss []storage.Section) []string {
	var out []string
	for _, s := range ss {
		out = append(out, s.Title+"|"+s.Content)
	}
	return out
}

func graphSet(g *storage.ObjectGraph) []string {
	if g == nil {
		return nil
	}
	var out []string
	for _, n := range g.Nodes {
		out = append(out, fmt.Sprintf("node %s %s %q %q by %q", n.ID, n.NodeType, n.Label, n.Content, n.ProducedBy()))
	}
	for _, e := range g.Edges {
		out = append(out, fmt.Sprintf("edge %s %s %s by %q", e.FromID, e.EdgeType, e.ToID, e.ProducedBy()))
	}
	slices.Sort(out)
	return out
}

// invPDF is a one-page PDF whose text is invText's first line.
func invPDF() []byte {
	stream := "BT /F1 12 Tf 72 720 Td (Write Vec<String> and AT&T in prose.) Tj ET"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var b strings.Builder
	b.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, x)
	return []byte(b.String())
}

// invDOCX is a minimal Word document holding invText's first line.
func invDOCX(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":         `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml":   `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Write Vec&lt;String&gt; and AT&amp;T in prose.</w:t></w:r></w:p></w:body></w:document>`,
	}
	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "word/document.xml"} {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write([]byte(files[name]))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

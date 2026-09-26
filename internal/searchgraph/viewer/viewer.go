// Package viewer embeds the search-graph viewer: a static page that renders
// a ctxt search graph (JSON Graph Format v2.1, vocabulary
// "ctxt.search-graph/v1") as a 3D force-directed graph, falling back to 2D
// when WebGL is unavailable. The page needs no network access beyond its
// own assets.
//
// The assets under dist/ are generated from web/searchgraph by
// `make build-searchgraph-viewer` and committed; do not edit them by hand.
//
// Two ways to ship it:
//   - Served: expose Assets() over HTTP and serve the graph document as
//     DataFile next to IndexFile; the page fetches it on load.
//   - Standalone: Standalone(doc) returns one self-contained HTML file with
//     the script, styles, license notices and the document inlined.
package viewer

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
)

// Asset names inside Assets().
const (
	IndexFile   = "index.html"
	ScriptFile  = "viewer.js"
	StyleFile   = "viewer.css"
	NoticesFile = "viewer.js.LEGAL.txt"
	// DataFile is the graph document the page fetches, relative to
	// IndexFile, when no document is inlined.
	DataFile = "graph.json"
)

//go:embed dist
var dist embed.FS

// Assets returns the viewer's static files: IndexFile, ScriptFile,
// StyleFile and NoticesFile. DataFile is not included; the caller serves it.
func Assets() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // unreachable: "dist" is a valid, embedded path
	}
	return sub
}

// ErrInvalidDocument reports a graph document that is not valid JSON.
var ErrInvalidDocument = errors.New("search graph document is not valid JSON")

// Exact fragments of IndexFile that Standalone rewrites. The build copies
// index.html verbatim, and tests pin each fragment to exactly one match.
const (
	cspServed    = `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self' data: blob:; base-uri 'none'; form-action 'none'">`
	styleLink    = `<link rel="stylesheet" href="viewer.css">`
	noticesLink  = `<a id="notices" href="viewer.js.LEGAL.txt">Third-party notices</a>`
	scriptSrcTag = `<script src="viewer.js"></script>`
)

// Standalone returns a single-file HTML page that renders doc offline. doc
// must be a JSON document; the page itself validates the vocabulary and
// shows an error for anything that is not a ctxt search graph.
//
// The document is embedded in a <script type="application/json"> element
// with <, > and & escaped as JSON unicode escapes, so no string in it
// (e.g. a label of "</script><script>...") can terminate the element. The
// inlined viewer script is allow-listed by hash in the page's Content
// Security Policy, which also forbids network access.
func Standalone(doc []byte) ([]byte, error) {
	if !json.Valid(doc) {
		return nil, ErrInvalidDocument
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, doc); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidDocument, err)
	}
	var data bytes.Buffer
	json.HTMLEscape(&data, compact.Bytes())

	page, script, style, notices, err := readAssets()
	if err != nil {
		return nil, err
	}
	if i := indexFold(script, "</script"); i >= 0 {
		return nil, fmt.Errorf("viewer: %s contains \"</script\" at byte %d; cannot inline", ScriptFile, i)
	}
	if i := indexFold(script, "<!--"); i >= 0 {
		return nil, fmt.Errorf("viewer: %s contains \"<!--\" at byte %d; cannot inline", ScriptFile, i)
	}
	if i := indexFold(style, "</style"); i >= 0 {
		return nil, fmt.Errorf("viewer: %s contains \"</style\" at byte %d; cannot inline", StyleFile, i)
	}

	sum := sha256.Sum256(script)
	csp := `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'sha256-` +
		base64.StdEncoding.EncodeToString(sum[:]) +
		`'; style-src 'unsafe-inline'; connect-src 'none'; img-src data: blob:; base-uri 'none'; form-action 'none'">`

	var inlineScripts bytes.Buffer
	inlineScripts.WriteString(`<script type="application/json" id="graph-data">`)
	inlineScripts.Write(data.Bytes())
	inlineScripts.WriteString("</script>\n<script>")
	inlineScripts.Write(script)
	inlineScripts.WriteString("</script>")

	replacements := []struct {
		name     string
		old, new []byte
	}{
		{"csp", []byte(cspServed), []byte(csp)},
		{"style", []byte(styleLink), concat("<style>", style, "</style>")},
		{"notices", []byte(noticesLink), concat(
			`<details id="notices"><summary>Third-party notices</summary><pre>`,
			[]byte(html.EscapeString(string(notices))), "</pre></details>")},
		{"script", []byte(scriptSrcTag), inlineScripts.Bytes()},
	}
	for _, r := range replacements {
		if n := bytes.Count(page, r.old); n != 1 {
			return nil, fmt.Errorf("viewer: %s: expected one %s placeholder, found %d", IndexFile, r.name, n)
		}
		page = bytes.Replace(page, r.old, r.new, 1)
	}
	return page, nil
}

func readAssets() (page, script, style, notices []byte, err error) {
	assets := Assets()
	read := func(name string) []byte {
		if err != nil {
			return nil
		}
		var b []byte
		b, err = fs.ReadFile(assets, name)
		if err != nil {
			err = fmt.Errorf("viewer: read embedded %s: %w", name, err)
		}
		return b
	}
	page, script, style, notices = read(IndexFile), read(ScriptFile), read(StyleFile), read(NoticesFile)
	return page, script, style, notices, err
}

func concat(prefix string, body []byte, suffix string) []byte {
	out := make([]byte, 0, len(prefix)+len(body)+len(suffix))
	out = append(out, prefix...)
	out = append(out, body...)
	return append(out, suffix...)
}

// indexFold is bytes.Index with ASCII case folding on the needle, which is
// how HTML matches end tags.
func indexFold(s []byte, needle string) int {
	return bytes.Index(bytes.ToLower(s), []byte(needle))
}

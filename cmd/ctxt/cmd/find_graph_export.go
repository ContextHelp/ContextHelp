package cmd

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/ideacrafterslabs/ctxt/internal/searchgraph"
	"github.com/ideacrafterslabs/ctxt/internal/searchgraph/viewer"
	"hop.top/kit/go/console/output"
)

// Graph document formats a --graph run can write.
const (
	graphFormatJGF     = "json"
	graphFormatYAML    = "yaml"
	graphFormatGraphML = "graphml"
	graphFormatGEXF    = "gexf"
	graphFormatHTML    = "html"
)

// graphFileExtensions maps an -o extension to the graph format it
// selects. .json and .yaml/.yml are the extensions kit itself maps to
// its json and yaml formatters, so -o means the same thing here as on
// every other command; the rest are formats only a graph has.
var graphFileExtensions = map[string]string{
	".json":    graphFormatJGF,
	".yaml":    graphFormatYAML,
	".yml":     graphFormatYAML,
	".graphml": graphFormatGraphML,
	".gexf":    graphFormatGEXF,
	".html":    graphFormatHTML,
}

// graphFormatNames names each format in messages.
var graphFormatNames = map[string]string{
	graphFormatJGF:     "JSON Graph Format",
	graphFormatYAML:    "YAML",
	graphFormatGraphML: "GraphML",
	graphFormatGEXF:    "GEXF",
	graphFormatHTML:    "standalone HTML viewer",
}

// graphFileFormat returns the graph format path's extension selects,
// and false for an extension that selects none.
func graphFileFormat(path string) (string, bool) {
	f, ok := graphFileExtensions[strings.ToLower(filepath.Ext(path))]
	return f, ok
}

// supportedGraphExtensions lists graphFileExtensions' keys, sorted.
func supportedGraphExtensions() string {
	exts := make([]string, 0, len(graphFileExtensions))
	for e := range graphFileExtensions {
		exts = append(exts, e)
	}
	sort.Strings(exts)
	return strings.Join(exts, ", ")
}

// errGraphFileExtension refuses an -o path whose extension picks no
// graph format when nothing else picks one either.
func errGraphFileExtension(path string) error {
	problem := fmt.Sprintf("extension %q names no graph format", filepath.Ext(path))
	if filepath.Ext(path) == "" {
		problem = "no file extension to pick a graph format"
	}
	e := output.UsageError(fmt.Sprintf(
		"--%s -o %s: %s (supported: %s)",
		flagGraph, path, problem, supportedGraphExtensions()))
	e.SuggestedFix = "use -o graph.html, or add --format json"
	return e
}

// writeGraph writes doc to w in format.
func writeGraph(w io.Writer, doc *searchgraph.Document, format string) error {
	switch format {
	case graphFormatJGF:
		return doc.Encode(w, true)
	case graphFormatYAML:
		return cliformat.EncodeAs(w, output.YAML, doc)
	case graphFormatGraphML:
		return doc.EncodeGraphML(w)
	case graphFormatGEXF:
		return doc.EncodeGEXF(w)
	case graphFormatHTML:
		var buf bytes.Buffer
		if err := doc.Encode(&buf, false); err != nil {
			return err
		}
		page, err := viewer.Standalone(buf.Bytes())
		if err != nil {
			return fmt.Errorf("find graph: %w", err)
		}
		_, err = w.Write(page)
		return err
	}
	return fmt.Errorf("find graph: unknown graph format %q", format)
}

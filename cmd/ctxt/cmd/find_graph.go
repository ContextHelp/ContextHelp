package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/browser/launch"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/retrieval"
	"github.com/ideacrafterslabs/ctxt/internal/searchgraph"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/spf13/cobra"
	"hop.top/kit/go/console/output"
)

// find --graph flag names.
const (
	flagGraph                 = "graph"
	flagGraphMaxNodes         = "graph-max-nodes"
	flagGraphMaxEdges         = "graph-max-edges"
	flagGraphSimilar          = "graph-similar"
	flagGraphSimilarThreshold = "graph-similar-threshold"
	flagGraphNoBrowser        = "no-browser"
	flagGraphIdleTimeout      = "graph-idle-timeout"
)

// graphTuningFlags only mean something alongside --graph.
var graphTuningFlags = []string{
	flagGraphMaxNodes, flagGraphMaxEdges, flagGraphSimilar, flagGraphSimilarThreshold,
	flagGraphNoBrowser, flagGraphIdleTimeout,
}

// graphViewerFlags only mean something when --graph opens the viewer.
var graphViewerFlags = []string{flagGraphNoBrowser, flagGraphIdleTimeout}

// graphViewerOpener opens the viewer URL. A variable so tests can
// swap in a fake and never launch a real browser.
var graphViewerOpener launch.Opener = launch.System{}

// graphTerminal reports whether a writer is a terminal. A variable so
// tests can stand in for a TTY.
var graphTerminal = isTerminal

// graphConflictFlags select a result shape or search mode the graph
// document cannot carry: it is always built from the hybrid trace.
var graphConflictFlags = []string{"fts", "semantic", "explain", "facets"}

// registerFindGraphFlags adds the --graph flag family to find. Defaults
// are the builder's own, so help text and output agree.
func registerFindGraphFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.Bool(flagGraph, false, "show the hybrid search trace as a graph: an interactive browser viewer, "+
		"a JGF document with --format json|yaml, or a file by -o extension (.html .json .yaml .graphml .gexf)")
	f.Int(flagGraphMaxNodes, searchgraph.DefaultMaxNodes, "graph node cap, query node included (with --graph)")
	f.Int(flagGraphMaxEdges, searchgraph.DefaultMaxEdges, "graph edge cap (with --graph)")
	f.Bool(flagGraphSimilar, false, "add similar edges between candidates from stored embeddings (with --graph)")
	f.Float64(flagGraphSimilarThreshold, searchgraph.DefaultSimilarThreshold,
		"minimum cosine similarity for a similar edge, in (0,1] (with --graph-similar)")
	f.Bool(flagGraphNoBrowser, false, "print the viewer URL without opening a browser (with --graph)")
	f.Duration(flagGraphIdleTimeout, defaultGraphViewerIdle, "stop the viewer after this long without a request (with --graph)")
}

// findGraphOptions reads and validates the --graph flag family. enabled
// is false when --graph is not set; any tuning flag given without it is
// a usage error rather than silently ignored.
func findGraphOptions(cmd *cobra.Command) (opts searchgraph.Options, enabled bool, err error) {
	f := cmd.Flags()
	enabled, _ = f.GetBool(flagGraph)
	if !enabled {
		for _, name := range graphTuningFlags {
			if f.Changed(name) {
				return opts, false, output.UsageError(fmt.Sprintf("--%s has no effect without --%s", name, flagGraph))
			}
		}
		return opts, false, nil
	}

	var conflicts []string
	for _, name := range graphConflictFlags {
		if on, _ := f.GetBool(name); on {
			conflicts = append(conflicts, "--"+name)
		}
	}
	if len(conflicts) > 0 {
		return opts, true, output.UsageError(fmt.Sprintf(
			"--%s cannot be combined with %s: the graph is built from the hybrid search trace",
			flagGraph, strings.Join(conflicts, ", ")))
	}

	opts.MaxNodes, _ = f.GetInt(flagGraphMaxNodes)
	opts.MaxEdges, _ = f.GetInt(flagGraphMaxEdges)
	opts.Similar, _ = f.GetBool(flagGraphSimilar)
	opts.SimilarThreshold, _ = f.GetFloat64(flagGraphSimilarThreshold)

	switch {
	case opts.MaxNodes < 1:
		return opts, true, output.UsageError(fmt.Sprintf("--%s must be at least 1, got %d", flagGraphMaxNodes, opts.MaxNodes))
	case opts.MaxEdges < 1:
		return opts, true, output.UsageError(fmt.Sprintf("--%s must be at least 1, got %d", flagGraphMaxEdges, opts.MaxEdges))
	case f.Changed(flagGraphSimilarThreshold) && !opts.Similar:
		return opts, true, output.UsageError(fmt.Sprintf("--%s has no effect without --%s", flagGraphSimilarThreshold, flagGraphSimilar))
	case opts.SimilarThreshold <= 0 || opts.SimilarThreshold > 1:
		return opts, true, output.UsageError(fmt.Sprintf("--%s must be in (0,1], got %g", flagGraphSimilarThreshold, opts.SimilarThreshold))
	}
	return opts, true, nil
}

// graphDeliveryKind is where a --graph run sends its document.
type graphDeliveryKind int

const (
	// graphToOutput: --format json|yaml, to stdout or the -o file.
	graphToOutput graphDeliveryKind = iota
	// graphToFile: no structured --format, -o names a file whose
	// extension picks the format.
	graphToFile
	// graphToViewer: no structured --format, no -o: the interactive
	// viewer, served locally.
	graphToViewer
)

// graphDelivery is the resolved destination of a --graph run.
type graphDelivery struct {
	kind      graphDeliveryKind
	format    string // graphFormat*, except for graphToViewer
	path      string // graphToFile only
	noBrowser bool
	idle      time.Duration
}

// findGraphDelivery resolves where a --graph run's document goes, from
// --format and -o, following kit's -o contract: an explicit --format
// wins, else the -o extension picks the format, and an explicit format
// that contradicts the extension is an error. The human rendering of a
// graph is the viewer, so with no structured format the document goes
// to the viewer, or to a file when -o names one.
func findGraphDelivery(cmd *cobra.Command) (graphDelivery, error) {
	f := cmd.Flags()
	format := cliformat.Active()
	path, toFile := cliformat.OutputPath(cmd)

	var d graphDelivery
	switch {
	case cliformat.IsStructured(format):
		d.kind, d.format = graphToOutput, format
		if ext, ok := graphFileFormat(path); toFile && ok && ext != format {
			e := output.UsageError(fmt.Sprintf(
				"--format %s does not match -o %s, which selects %s", format, path, graphFormatNames[ext]))
			e.SuggestedFix = "drop --format and let the -o extension choose"
			return d, e
		}
	case format == cliformat.Table || format == output.Human:
		if !toFile {
			d.kind = graphToViewer
			break
		}
		ext, ok := graphFileFormat(path)
		if !ok {
			return d, errGraphFileExtension(path)
		}
		d.kind, d.format, d.path = graphToFile, ext, path
	default:
		e := output.UsageError(fmt.Sprintf(
			"--%s has no %s rendering: use --format json or yaml, -o <file> (%s), or no --format for the viewer",
			flagGraph, format, supportedGraphExtensions()))
		e.SuggestedFix = "add --format json"
		return d, e
	}

	if d.kind != graphToViewer {
		for _, name := range graphViewerFlags {
			if f.Changed(name) {
				return d, output.UsageError(fmt.Sprintf(
					"--%s only applies to the interactive viewer (--%s without --format json|yaml or -o)", name, flagGraph))
			}
		}
		return d, nil
	}
	d.noBrowser, _ = f.GetBool(flagGraphNoBrowser)
	d.idle, _ = f.GetDuration(flagGraphIdleTimeout)
	if d.idle <= 0 {
		return d, output.UsageError(fmt.Sprintf("--%s must be positive, got %s", flagGraphIdleTimeout, d.idle))
	}
	return d, nil
}

// shouldOpenGraphBrowser reports whether the viewer opens a browser:
// only for a person at a terminal. A run whose stdout or stderr is
// captured (a script, an agent, CI) gets the URL and a server, never a
// browser window it did not ask for.
func shouldOpenGraphBrowser(noBrowser bool, stdout, stderr io.Writer) bool {
	return !noBrowser && graphTerminal(stdout) && graphTerminal(stderr)
}

// findGraphQuery is the search a --graph run traces.
type findGraphQuery struct {
	text   string
	filter storage.ObjectFilter
	search config.SearchConfig
	sem    retrieval.SemanticSource
}

// runFindGraph runs the traced hybrid search and delivers its graph
// document. JSON goes through the document's own encoder so the output
// is the bare JGF object with strings verbatim; YAML goes through the
// shared structured encoder. release frees the service once the
// document is built; the viewer calls it before serving. q.sem is the same
// semantic leg plain hybrid search uses; a degraded leg gets the same
// stderr notice.
func runFindGraph(ctx context.Context, cmd *cobra.Command, svc *service.Service, release func(), q findGraphQuery, opts searchgraph.Options, d graphDelivery) error {
	res, err := svc.HybridSearchExplainFilteredWithTrace(ctx, q.text, q.filter, q.sem, q.search)
	if err != nil {
		return fmt.Errorf("find graph: %w", err)
	}
	printSemanticNotice(cmd, res.Diagnostics)
	doc, err := searchgraph.Build(ctx, res.Trace, searchgraph.SourceFrom(svc.Store), opts)
	if err != nil {
		return findGraphBuildError(err)
	}

	switch d.kind {
	case graphToFile:
		if err := cliformat.WriteTo(cmd, os.Stdout, func(w io.Writer) error { return writeGraph(w, doc, d.format) }); err != nil {
			return err
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Wrote search graph (%s) to %s\n", graphFormatNames[d.format], d.path)
		return nil
	case graphToViewer:
		var buf bytes.Buffer
		if err := doc.Encode(&buf, false); err != nil {
			return err
		}
		release()
		ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()
		srv := graphViewerServer{
			Log:         cmd.ErrOrStderr(),
			Opener:      graphViewerOpener,
			OpenBrowser: shouldOpenGraphBrowser(d.noBrowser, cmd.OutOrStdout(), cmd.ErrOrStderr()),
			Idle:        d.idle,
		}
		return srv.Run(ctx, buf.Bytes())
	}
	return cliformat.WriteTo(cmd, os.Stdout, func(w io.Writer) error { return writeGraph(w, doc, d.format) })
}

// findGraphBuildError classifies a graph build failure. A store that
// cannot read embeddings was never configured for similar edges, which
// kit classes as usage; the sentinel stays matchable.
func findGraphBuildError(err error) error {
	if errors.Is(err, searchgraph.ErrSimilarUnsupported) {
		e := output.WrapError(err, output.CodeUsage, output.ExitUsage)
		e.Message = fmt.Sprintf("--%s needs stored embeddings, and this store cannot read them", flagGraphSimilar)
		e.SuggestedFix = "drop --" + flagGraphSimilar
		return e
	}
	return fmt.Errorf("find graph: %w", err)
}

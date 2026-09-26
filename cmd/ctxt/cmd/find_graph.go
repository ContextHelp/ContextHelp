package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

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
)

// graphTuningFlags only mean something alongside --graph.
var graphTuningFlags = []string{flagGraphMaxNodes, flagGraphMaxEdges, flagGraphSimilar, flagGraphSimilarThreshold}

// graphConflictFlags select a result shape or search mode the graph
// document cannot carry: it is always built from the hybrid trace.
var graphConflictFlags = []string{"fts", "semantic", "explain", "facets"}

// registerFindGraphFlags adds the --graph flag family to find. Defaults
// are the builder's own, so help text and output agree.
func registerFindGraphFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.Bool(flagGraph, false, "emit the hybrid search trace as a JGF graph document (needs --format json or yaml)")
	f.Int(flagGraphMaxNodes, searchgraph.DefaultMaxNodes, "graph node cap, query node included (with --graph)")
	f.Int(flagGraphMaxEdges, searchgraph.DefaultMaxEdges, "graph edge cap (with --graph)")
	f.Bool(flagGraphSimilar, false, "add similar edges between candidates from stored embeddings (with --graph)")
	f.Float64(flagGraphSimilarThreshold, searchgraph.DefaultSimilarThreshold,
		"minimum cosine similarity for a similar edge, in (0,1] (with --graph-similar)")
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

// errFindGraphViewerUnavailable refuses --graph for human output. The
// interactive graph viewer replaces this refusal.
func errFindGraphViewerUnavailable() error {
	e := output.UsageError(fmt.Sprintf(
		"--%s needs --format json (or yaml): the interactive graph viewer is not available yet", flagGraph))
	e.SuggestedFix = "add --format json"
	return e
}

// runFindGraph runs the traced hybrid search and writes its graph
// document. JSON goes through the document's own encoder so the output
// is the bare JGF object with strings verbatim; YAML goes through the
// shared structured encoder. sem is the same semantic leg plain hybrid
// search uses; a degraded leg gets the same stderr notice.
func runFindGraph(ctx context.Context, cmd *cobra.Command, svc *service.Service, sem retrieval.SemanticSource, query string, filter storage.ObjectFilter, searchCfg config.SearchConfig, opts searchgraph.Options) error {
	res, err := svc.HybridSearchExplainFilteredWithTrace(ctx, query, filter, sem, searchCfg)
	if err != nil {
		return fmt.Errorf("find graph: %w", err)
	}
	printSemanticNotice(cmd, res.Diagnostics)
	doc, err := searchgraph.Build(ctx, res.Trace, searchgraph.SourceFrom(svc.Store), opts)
	if err != nil {
		return findGraphBuildError(err)
	}
	if cliformat.Active() == output.JSON {
		return cliformat.WriteTo(nil, os.Stdout, func(w io.Writer) error { return doc.Encode(w, true) })
	}
	return outputJSON(os.Stdout, doc)
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

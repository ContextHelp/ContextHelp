package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cli"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/retrieval"
	"github.com/ideacrafterslabs/ctxt/internal/searchgraph"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"hop.top/kit/go/console/output"
)

var findCmd = &cobra.Command{
	Use:   "find [query]",
	Short: "Search knowledge (hybrid FTS+vector by default)",
	Long: `Search knowledge objects using hybrid FTS+vector search with RRF reranking.

The default mode is "hybrid" (FTS + vector via RRF). Use --fts for FTS-only,
--semantic for vector-only. Per-call overrides apply on top of the active
profile's search strategy and global config defaults.

Resolution order: CLI flags → active profile search_strategy → global search config.

find runs on the dpkms instance --server, --instance or the current
instance names (POST /api/v1/find). ctxt sends the resolved mode, knobs and
active profile; the active profile scopes the search to the objects it
owns. dpkms embeds the query with its default embedding model.

Examples:
  # Hybrid search (default)
  ctxt find "authentication best practices"

  # FTS-only search
  ctxt find "checkout flow" --fts

  # Vector-only search
  ctxt find "signup conversion" --semantic

  # Override RRF weights
  ctxt find "database indexing" --fts-weight 0.3 --vector-weight 0.7

  # Search one profile's objects with its search strategy
  ctxt find "signup flow" --profile growth

  # Limit results
  ctxt find "onboarding" --limit 10

  # Show score breakdown per result
  ctxt find "onboarding" --explain

  # Filter by metadata type
  ctxt find "auth" --meta-type task

  # Filter by topic and person
  ctxt find "deployment" --topic kubernetes --person alice-chen

  # Filter by source and date range
  ctxt find "standup" --source-type slack --since 2026-04-01

  # Show facet breakdown with results
  ctxt find "architecture" --facets

  # Explore the search as an interactive 3D graph in the browser
  # (local, loopback-only server; Ctrl-C to stop)
  ctxt find "deployment" --graph

  # Print the viewer URL without opening a browser
  ctxt find "deployment" --graph --no-browser

  # Search trace as a JGF graph document (nodes: query, candidates, entities)
  ctxt find "deployment" --graph --format json

  # Write the graph to a file; the extension picks the format:
  # .html (self-contained viewer), .json (JGF), .yaml, .graphml, .gexf
  ctxt find "deployment" --graph -o deployment.html
  ctxt find "deployment" --graph -o deployment.gexf

  # Smaller graph with similarity edges
  ctxt find "deployment" --graph --graph-max-nodes 50 --graph-similar --format json`,
	RunE: runFind,
}

// findEmbeddingFlags configure the local search --graph runs; every
// other find embeds its query on the dpkms host.
var findEmbeddingFlags = []string{embeddings.FlagProvider, embeddings.FlagModel, embeddings.FlagEndpoint}

func init() {
	rootCmd.AddCommand(findCmd)
	cliconv.WithSideEffect(findCmd, cliconv.SideEffectRead)
	cliconv.WithExamples(findCmd, []cliconv.Example{
		{Title: "Hybrid search (default)", Command: "ctxt find \"authentication best practices\""},
		{Title: "FTS-only search", Command: "ctxt find \"checkout flow\" --fts"},
		{Title: "Vector-only search with limit", Command: "ctxt find \"signup conversion\" --semantic --limit 10"},
	})

	findCmd.Flags().Int("limit", 10, "maximum results")
	findCmd.Flags().Bool("semantic", false, "vector-only search; dpkms embeds the query with its default model")
	findCmd.Flags().Bool("hybrid", false, "hybrid FTS+vector search with RRF (default mode from config)")
	findCmd.Flags().Bool("fts", false, "FTS-only search")

	// Per-call RRF overrides (zero = use config/profile value)
	findCmd.Flags().Int("rrf-k", 0, "RRF k constant override (default: from config)")
	findCmd.Flags().Float64("fts-weight", 0, "RRF FTS leg weight override (default: from config)")
	findCmd.Flags().Float64("vector-weight", 0, "RRF vector leg weight override (default: from config)")
	findCmd.Flags().Int("fts-pool", 0, "FTS candidate pool size override")
	findCmd.Flags().Int("vector-pool", 0, "vector candidate pool size override")
	findCmd.Flags().Float64("min-score", -1, "minimum RRF score threshold override (-1 = use config)")
	findCmd.Flags().Bool("explain", false, "show per-signal score breakdown for each result")

	// Metadata facet filters (US-0407)
	findCmd.Flags().String("meta-type", "", "filter by metadata type (e.g. observation, task)")
	findCmd.Flags().String("topic", "", "filter by topic in metadata")
	findCmd.Flags().String("person", "", "filter by person in metadata")
	findCmd.Flags().String("since", "", "filter by dates_mentioned >= (ISO date)")
	findCmd.Flags().String("until", "", "filter by dates_mentioned <= (ISO date)")
	findCmd.Flags().String("source-type", "", "filter by source_type")
	findCmd.Flags().Bool("facets", false, "show metadata type count breakdown")
	findCmd.Flags().String("server", "", serverFlagUsage)

	// Embedding provider overrides for --graph's local search.
	embeddings.AddFlags(findCmd.Flags(), &findEmbedding)
	for _, name := range findEmbeddingFlags {
		f := findCmd.Flags().Lookup(name)
		f.Usage += " (with --graph only)"
	}

	registerFindGraphFlags(findCmd)

	viper.BindPFlag("find.limit", findCmd.Flags().Lookup("limit"))
	viper.BindPFlag("find.semantic", findCmd.Flags().Lookup("semantic"))
	viper.BindPFlag("find.hybrid", findCmd.Flags().Lookup("hybrid"))
	viper.BindPFlag("find.fts", findCmd.Flags().Lookup("fts"))
	viper.BindPFlag("find.rrf_k", findCmd.Flags().Lookup("rrf-k"))
	viper.BindPFlag("find.fts_weight", findCmd.Flags().Lookup("fts-weight"))
	viper.BindPFlag("find.vector_weight", findCmd.Flags().Lookup("vector-weight"))
	viper.BindPFlag("find.fts_pool", findCmd.Flags().Lookup("fts-pool"))
	viper.BindPFlag("find.vector_pool", findCmd.Flags().Lookup("vector-pool"))
	viper.BindPFlag("find.min_score", findCmd.Flags().Lookup("min-score"))
}

// findEmbedding holds find's --embedding-* flag values (--graph only).
var findEmbedding embeddings.Overrides

// findEmbeddingHTTPClient, when set, carries --graph's embedding requests
// (tests replay recorded provider traffic through it).
var findEmbeddingHTTPClient *http.Client

func runFind(cmd *cobra.Command, args []string) error {
	graphOpts, graph, err := findGraphOptions(cmd)
	if err != nil {
		return err
	}
	// Resolve where the graph goes before searching, so a bad
	// --format/-o combination fails fast as a usage error.
	var graphTo graphDelivery
	if graph {
		if graphTo, err = findGraphDelivery(cmd); err != nil {
			return err
		}
	} else if err := rejectFindEmbeddingFlags(cmd); err != nil {
		return err
	}

	query, source, err := cli.GetInput(args)
	if err != nil {
		return err
	}
	if source == cli.SourceClipboard {
		fmt.Fprintf(os.Stderr, "Searching clipboard content: %q\n", query)
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	limit := viper.GetInt("find.limit")
	profile := activeProfileName(cmd)
	searchCfg := resolveFindSearch(profile)

	// --graph always runs the traced hybrid search, whatever the mode.
	if graph {
		return runFindGraphLocal(ctx, cmd, findGraphQuery{text: query, filter: buildFindFilter(cmd, limit), search: searchCfg}, graphOpts, graphTo)
	}

	mode := findMode(searchCfg)
	explain, _ := cmd.Flags().GetBool("explain")
	facets, _ := cmd.Flags().GetBool("facets")
	// --explain only applies to hybrid mode, and replaces the facet
	// breakdown there.
	explain = explain && mode == service.FindModeHybrid
	facets = facets && !explain

	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	res, err := client.Find(ctx, dpkmsclient.FindRequest{
		Query:   query,
		Mode:    mode,
		Limit:   limit,
		Profile: profile,
		Filter:  findFilter(cmd),
		Search:  findSearchKnobs(searchCfg),
		Explain: explain,
		Facets:  facets,
	})
	if err != nil {
		return err
	}
	printFindNotice(cmd, res.Diagnostics.Semantic)
	if res.Objects == nil {
		res.Objects = []*storage.KnowledgeObject{}
	}
	if explain {
		return renderFindExplain(query, res)
	}
	return renderFind(query, res, facets)
}

// rejectFindEmbeddingFlags refuses the --embedding-* flags outside
// --graph: dpkms embeds the query with the model registered there, so
// they would change nothing.
func rejectFindEmbeddingFlags(cmd *cobra.Command) error {
	for _, name := range findEmbeddingFlags {
		if cmd.Flags().Changed(name) {
			e := output.UsageError(fmt.Sprintf(
				"--%s only applies to --%s: find embeds the query on the dpkms host with its default embedding model",
				name, flagGraph))
			e.SuggestedFix = "drop --" + name
			return e
		}
	}
	return nil
}

// resolveFindSearch resolves the search strategy: global config, then the
// active profile's search_strategy, then find's own flags.
func resolveFindSearch(profile string) config.SearchConfig {
	searchCfg := config.DefaultSearchConfig()
	if cfg != nil {
		searchCfg = cfg.Search
		if p, ok := cfg.Profile.Profiles[profile]; ok {
			searchCfg = config.ResolveSearchConfig(searchCfg, p.SearchStrategy)
		}
	}
	if k := viper.GetInt("find.rrf_k"); k > 0 {
		searchCfg.RRF.K = k
	}
	if w := viper.GetFloat64("find.fts_weight"); w > 0 {
		searchCfg.RRF.FTSWeight = w
	}
	if w := viper.GetFloat64("find.vector_weight"); w > 0 {
		searchCfg.RRF.VectorWeight = w
	}
	if p := viper.GetInt("find.fts_pool"); p > 0 {
		searchCfg.CandidatePool.FTS = p
	}
	if p := viper.GetInt("find.vector_pool"); p > 0 {
		searchCfg.CandidatePool.Vector = p
	}
	if s := viper.GetFloat64("find.min_score"); s >= 0 {
		searchCfg.MinScore = s
	}
	return searchCfg
}

// findMode is the resolved search mode: a mode flag, else the resolved
// default, else hybrid.
func findMode(searchCfg config.SearchConfig) string {
	switch {
	case viper.GetBool("find.hybrid"):
		return service.FindModeHybrid
	case viper.GetBool("find.semantic"):
		return service.FindModeVector
	case viper.GetBool("find.fts"):
		return service.FindModeFTS
	case searchCfg.DefaultMode != "":
		return searchCfg.DefaultMode
	}
	return service.FindModeHybrid
}

// findSearchKnobs is the resolved strategy as the request's search block.
// Every knob travels, so dpkms applies what ctxt resolved (ADR-077 §6);
// a count left at zero means unset and takes dpkms's default.
func findSearchKnobs(sc config.SearchConfig) dpkmsclient.FindSearch {
	positive := func(n int) *int {
		if n <= 0 {
			return nil
		}
		return &n
	}
	f := func(v float64) *float64 { return &v }
	return dpkmsclient.FindSearch{
		RRFK:                   positive(sc.RRF.K),
		FTSWeight:              f(sc.RRF.FTSWeight),
		VectorWeight:           f(sc.RRF.VectorWeight),
		FTSPool:                positive(sc.CandidatePool.FTS),
		VectorPool:             positive(sc.CandidatePool.Vector),
		MinScore:               f(sc.MinScore),
		FallbackToFTS:          &sc.FallbackToFTS,
		MentionBoostPerMention: f(sc.Reranker.MentionBoostPerMention),
		MaxMentionBoost:        f(sc.Reranker.MaxMentionBoost),
		DirectBacklinkBoost:    f(sc.Reranker.DirectBacklinkBoost),
		HopBacklinkBoost:       f(sc.Reranker.HopBacklinkBoost),
	}
}

// findFilter is the request's metadata facet filter from find's flags.
// dpkms validates the dates.
func findFilter(cmd *cobra.Command) dpkmsclient.FindFilter {
	var f dpkmsclient.FindFilter
	f.MetaType, _ = cmd.Flags().GetString("meta-type")
	f.Topic, _ = cmd.Flags().GetString("topic")
	f.Person, _ = cmd.Flags().GetString("person")
	f.SourceType, _ = cmd.Flags().GetString("source-type")
	f.Since, _ = cmd.Flags().GetString("since")
	f.Until, _ = cmd.Flags().GetString("until")
	return f
}

// renderFind prints a find response: the objects, and the facet
// breakdown when asked for.
func renderFind(query string, res *dpkmsclient.FindResponse, facets bool) error {
	d := res.Diagnostics
	if isJSONOutput() {
		doc := map[string]any{
			"objects":     res.Objects,
			"total":       len(res.Objects),
			"query":       query,
			"mode":        res.Mode,
			"diagnostics": d,
		}
		if facets {
			counts := res.Facets
			if counts == nil {
				counts = map[string]int{}
			}
			doc["facets"] = counts
		}
		return outputJSON(os.Stdout, doc)
	}

	if facets {
		types := make([]string, 0, len(res.Facets))
		for t := range res.Facets {
			types = append(types, t)
		}
		sort.Strings(types)
		fmt.Printf("Facets (metadata type):\n")
		for _, t := range types {
			fmt.Printf("  %-20s %d\n", t, res.Facets[t])
		}
		fmt.Println()
	}

	fmt.Printf("Search [%s]: %q (%d results)\n\n", res.Mode, query, len(res.Objects))
	if len(res.Objects) == 0 {
		printBelowThreshold(d)
		if d.CandidateCount > 0 && d.BelowThresholdCount > 0 {
			fmt.Println("Run with --explain to see which docs were dropped.")
		}
		return nil
	}

	headers := []string{"ID", "Type", "Created"}
	rows := make([][]string, 0, len(res.Objects))
	for _, obj := range res.Objects {
		rows = append(rows, []string{obj.ID, obj.Type, obj.CreatedAt.Format("2006-01-02 15:04")})
	}
	printTable(os.Stdout, headers, rows)
	printStaleness(d)
	return nil
}

// findExplainResult is one --explain result: the object and why it
// ranked where it did.
type findExplainResult struct {
	Object       *storage.KnowledgeObject       `json:"object"`
	Breakdown    dpkmsclient.FindScoreBreakdown `json:"score_breakdown"`
	DocumentView storage.DocumentProjection     `json:"document_view"`
}

// renderFindExplain prints a hybrid find response with its per-result
// score breakdowns.
func renderFindExplain(query string, res *dpkmsclient.FindResponse) error {
	if len(res.Explain) != len(res.Objects) {
		return fmt.Errorf("find explain: dpkms answered %d objects and %d score breakdowns", len(res.Objects), len(res.Explain))
	}
	results := make([]findExplainResult, len(res.Objects))
	for i, obj := range res.Objects {
		x := res.Explain[i]
		if x.ID != obj.ID {
			return fmt.Errorf("find explain: breakdown %d is for %s, not %s", i, x.ID, obj.ID)
		}
		results[i] = findExplainResult{Object: obj, Breakdown: x.Breakdown, DocumentView: x.DocumentView}
	}
	d := res.Diagnostics

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"results":     results,
			"total":       len(results),
			"query":       query,
			"mode":        res.Mode,
			"diagnostics": d,
		})
	}

	fmt.Printf("Search [%s] --explain: %q (%d results)\n\n", res.Mode, query, len(results))
	if len(results) == 0 {
		printBelowThreshold(d)
		return nil
	}
	for i, r := range results {
		b := r.Breakdown
		fmt.Printf("%d. %s\n", i+1, koLabel(r.Object))
		fmt.Printf("   total=%.4f  fts=%.4f  vector=%.4f  mention=%.4f  graph=%.4f\n",
			b.Total, b.FTS, b.Vector, b.MentionBoost, b.GraphRelevance)
		fmt.Println()
	}
	if d.BelowThresholdCount > 0 {
		fmt.Printf("(%d additional candidate%s dropped below threshold %.2f; top below-threshold score: %.2f)\n",
			d.BelowThresholdCount, pluralS(d.BelowThresholdCount), d.Threshold, d.TopBelowThresholdScore)
	}
	printStaleness(d)
	return nil
}

// printBelowThreshold explains an empty result whose candidates all
// scored under the threshold.
func printBelowThreshold(d dpkmsclient.FindDiagnostics) {
	if d.CandidateCount == 0 || d.BelowThresholdCount == 0 {
		return
	}
	fmt.Printf("No results above threshold (%.2f).\n", d.Threshold)
	fmt.Printf("Matched %d candidate%s, all below threshold. Top below-threshold score: %.2f.\n",
		d.CandidateCount, pluralS(d.CandidateCount), d.TopBelowThresholdScore)
}

// printStaleness notes results that are pending a pipeline upgrade. The
// results above it still stand.
func printStaleness(d dpkmsclient.FindDiagnostics) {
	if w := d.StalenessWarning; w != nil && w.Count > 0 {
		fmt.Printf("\nNote: %s\n", w.Reason)
	}
}

// pluralS returns "s" for n != 1, otherwise "". Helper for grammatical
// pluralisation in find diagnostics output.
func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// printFindNotice tells the operator, on stderr, when the semantic leg
// did not run and the results are full-text only. JSON output carries the
// same report under diagnostics.semantic.
func printFindNotice(cmd *cobra.Command, sem *dpkmsclient.FindSemanticReport) {
	if sem == nil || sem.OK() {
		return
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "notice: %s\n", sem.Notice)
}

// runFindGraphLocal runs find --graph in process against the local
// store, with the default embedding model's provider resolved here and
// find's --embedding-* flags applied.
func runFindGraphLocal(ctx context.Context, cmd *cobra.Command, q findGraphQuery, opts searchgraph.Options, d graphDelivery) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	// release is idempotent so the graph viewer can let go of the
	// service before it starts serving, which may last minutes.
	release := sync.OnceFunc(cleanup)
	defer release()
	if q.sem, err = findSemanticSource(svc.Store); err != nil {
		return fmt.Errorf("find: %w", err)
	}
	return runFindGraph(ctx, cmd, svc, release, q, opts, d)
}

// findSemanticSource builds --graph's semantic leg: the model registry on
// the command's store and the embedding resolver with find's
// --embedding-* flags (transport-only for a registered model).
func findSemanticSource(store storage.StorageDriver) (retrieval.SemanticSource, error) {
	models, err := registry.ForDriver(store)
	if err != nil {
		return retrieval.SemanticSource{}, err
	}
	r := newEmbeddingResolver()
	r.Registry = models
	r.Flags = findEmbedding
	r.HTTPClient = findEmbeddingHTTPClient
	return retrieval.SemanticSource{Models: models, Resolver: embeddings.NewProviderResolver(r)}, nil
}

// printSemanticNotice is printFindNotice for --graph's in-process search.
func printSemanticNotice(cmd *cobra.Command, d service.SearchDiagnostics) {
	if d.Semantic == nil || d.Semantic.OK() {
		return
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "notice: %s\n", d.Semantic.Notice)
}

// buildFindFilter constructs --graph's ObjectFilter from find's metadata
// facet flags.
func buildFindFilter(cmd *cobra.Command, limit int) storage.ObjectFilter {
	f := storage.ObjectFilter{Limit: limit}

	f.MetadataType, _ = cmd.Flags().GetString("meta-type")
	f.MetadataTopic, _ = cmd.Flags().GetString("topic")
	f.MetadataPerson, _ = cmd.Flags().GetString("person")
	f.SourceType, _ = cmd.Flags().GetString("source-type")

	if since, _ := cmd.Flags().GetString("since"); since != "" {
		if t, err := time.Parse("2006-01-02", since); err == nil {
			f.MetadataSince = &t
		}
	}
	if until, _ := cmd.Flags().GetString("until"); until != "" {
		if t, err := time.Parse("2006-01-02", until); err == nil {
			f.MetadataUntil = &t
		}
	}
	return f
}

package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cli"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/repl"
	"github.com/ideacrafterslabs/ctxt/internal/retrieval"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var findCmd = &cobra.Command{
	Use:   "find [query]",
	Short: "Search knowledge (hybrid FTS+vector by default)",
	Long: `Search knowledge objects using hybrid FTS+vector search with RRF reranking.

The default mode is "hybrid" (FTS + vector via RRF). Use --fts for FTS-only,
--semantic for vector-only. Per-call overrides apply on top of the active
profile's search strategy and global config defaults.

Resolution order: CLI flags → active profile search_strategy → global search config.

Examples:
  # Hybrid search (default)
  ctxt find "authentication best practices"

  # FTS-only search
  ctxt find "checkout flow" --fts

  # Vector-only search
  ctxt find "signup conversion" --semantic

  # Override RRF weights
  ctxt find "database indexing" --fts-weight 0.3 --vector-weight 0.7

  # Search with profile context
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
  ctxt find "architecture" --facets`,
	RunE: runFind,
}

func init() {
	rootCmd.AddCommand(findCmd)
	cliconv.WithSideEffect(findCmd, cliconv.SideEffectRead)
	cliconv.WithExamples(findCmd, []cliconv.Example{
		{Title: "Hybrid search (default)", Command: "ctxt find \"authentication best practices\""},
		{Title: "FTS-only search", Command: "ctxt find \"checkout flow\" --fts"},
		{Title: "Vector-only search with limit", Command: "ctxt find \"signup conversion\" --semantic --limit 10"},
	})

	findCmd.Flags().Int("limit", 10, "maximum results")
	findCmd.Flags().Bool("semantic", false, "vector-only search via embedding provider")
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

	// Per-run embedding provider overrides (vector and hybrid modes).
	embeddings.AddFlags(findCmd.Flags(), &findEmbedding)

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

// findEmbedding holds find's --embedding-* flag values.
var findEmbedding embeddings.Overrides

// findEmbeddingHTTPClient, when set, carries find's embedding requests
// (tests replay recorded provider traffic through it).
var findEmbeddingHTTPClient *http.Client

func runFind(cmd *cobra.Command, args []string) error {
	query, source, err := cli.GetInput(args)
	if err != nil {
		return err
	}
	if source == "clipboard" {
		fmt.Fprintf(os.Stderr, "Searching clipboard content: %q\n", query)
	}

	limit := viper.GetInt("find.limit")

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	// Use the command context so session state (injected by REPL) is visible.
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	// Extract session state when running inside a REPL session.
	var sessionState *repl.SessionState
	if v := ctx.Value(repl.SessionContextKey{}); v != nil {
		sessionState, _ = v.(*repl.SessionState)
	}

	// Resolve search config: global → active profile → CLI flags.
	searchCfg := cfg.Search
	activeProfile := viper.GetString("profile.default")
	if p, ok := cfg.Profile.Profiles[activeProfile]; ok {
		searchCfg = config.ResolveSearchConfig(searchCfg, p.SearchStrategy)
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

	// Determine mode: explicit flags override config.
	mode := searchCfg.DefaultMode
	if viper.GetBool("find.fts") {
		mode = "fts"
	}
	if viper.GetBool("find.semantic") {
		mode = "vector"
	}
	if viper.GetBool("find.hybrid") {
		mode = "hybrid"
	}

	explain, _ := cmd.Flags().GetBool("explain")
	facets, _ := cmd.Flags().GetBool("facets")

	// vector and hybrid (incl. --explain) search the default embedding
	// model's index; the default is read per query, never cached.
	var sem retrieval.SemanticSource
	if mode != "fts" {
		sem, err = findSemanticSource(svc.Store, sessionState)
		if err != nil {
			return fmt.Errorf("find: %w", err)
		}
	}

	// Build metadata facet filter from CLI flags.
	filter := buildFindFilter(cmd, limit)

	// --explain only applies to hybrid mode; it prints per-signal score breakdowns.
	if explain && mode == "hybrid" {
		return runFindExplain(ctx, cmd, svc, sem, query, limit, mode, searchCfg)
	}

	var results []*storage.KnowledgeObject
	// fts mode leaves diagnostics at zero values (CandidateCount = 0, no
	// semantic report) so JSON output stays uniform.
	var diagnostics service.SearchDiagnostics

	switch mode {
	case "vector":
		results, diagnostics, err = svc.SemanticSearchFiltered(ctx, query, filter, sem, searchCfg)
	case "fts":
		results, err = svc.FindByTextFiltered(ctx, query, filter)
	default: // "hybrid"
		results, diagnostics, err = svc.HybridSearchFilteredWithDiagnostics(ctx, query, filter, sem, searchCfg)
	}

	if err != nil {
		return fmt.Errorf("find (%s): %w", mode, err)
	}
	printSemanticNotice(cmd, diagnostics)

	// --facets: show metadata type count breakdown before results.
	if facets {
		counts, fErr := svc.FacetCounts(ctx, filter)
		if fErr == nil {
			if isJSONOutput() {
				return outputJSON(os.Stdout, map[string]any{
					"objects":     results,
					"total":       len(results),
					"query":       query,
					"mode":        mode,
					"facets":      counts,
					"diagnostics": diagnostics,
				})
			}
			fmt.Printf("Facets (metadata type):\n")
			for t, c := range counts {
				fmt.Printf("  %-20s %d\n", t, c)
			}
			fmt.Println()
		}
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"objects":     results,
			"total":       len(results),
			"query":       query,
			"mode":        mode,
			"diagnostics": diagnostics,
		})
	}

	fmt.Printf("Search [%s]: %q (%d results)\n\n", mode, query, len(results))

	if len(results) == 0 {
		// T-0574: distinguish "nothing matched any token" from "matches existed
		// but the reranker dropped them all under MinScore". The first surfaces
		// suggestions; the second surfaces threshold context.
		if diagnostics.CandidateCount > 0 && diagnostics.BelowThresholdCount > 0 {
			fmt.Printf("No results above threshold (%.2f).\n", diagnostics.Threshold)
			fmt.Printf("Matched %d candidate%s, all below threshold. Top below-threshold score: %.2f.\n",
				diagnostics.CandidateCount,
				pluralS(diagnostics.CandidateCount),
				diagnostics.TopBelowThresholdScore)
			fmt.Println("Run with --explain to see which docs were dropped.")
			return nil
		}
		printFindSuggestions(cmd, ctx, svc, query)
		return nil
	}

	headers := []string{"ID", "Type", "Created"}
	var rows [][]string
	for _, obj := range results {
		rows = append(rows, []string{
			obj.ID,
			obj.Type,
			obj.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	printTable(os.Stdout, headers, rows)

	// T-0581: surface staleness so operators see when their hits include
	// objects pending pipeline upgrade. Soft signal — search still
	// returned the (possibly stale) results above.
	if diagnostics.StalenessWarning != nil && diagnostics.StalenessWarning.Count > 0 {
		fmt.Printf("\nNote: %s\n", diagnostics.StalenessWarning.Reason)
	}
	return nil
}

// runFindExplain executes a hybrid search and prints per-result score breakdowns.
func runFindExplain(ctx context.Context, cmd *cobra.Command, svc *service.Service, sem retrieval.SemanticSource, query string, limit int, mode string, searchCfg config.SearchConfig) error {
	envelope, err := svc.HybridSearchExplainFilteredWithDiagnostics(ctx, query, storage.ObjectFilter{Limit: limit}, sem, searchCfg)
	if err != nil {
		return fmt.Errorf("find explain (%s): %w", mode, err)
	}
	explainResults := envelope.Results
	diagnostics := envelope.Diagnostics
	printSemanticNotice(cmd, diagnostics)

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"results":     explainResults,
			"total":       len(explainResults),
			"query":       query,
			"mode":        mode,
			"diagnostics": diagnostics,
		})
	}

	fmt.Printf("Search [%s] --explain: %q (%d results)\n\n", mode, query, len(explainResults))

	if len(explainResults) == 0 {
		// T-0574: surface threshold context when candidates surfaced but were
		// all dropped below MinScore.
		if diagnostics.CandidateCount > 0 && diagnostics.BelowThresholdCount > 0 {
			fmt.Printf("No results above threshold (%.2f).\n", diagnostics.Threshold)
			fmt.Printf("Matched %d candidate%s, all below threshold. Top below-threshold score: %.2f.\n",
				diagnostics.CandidateCount,
				pluralS(diagnostics.CandidateCount),
				diagnostics.TopBelowThresholdScore)
		}
		return nil
	}

	for i, r := range explainResults {
		obj := r.Object
		b := r.Breakdown
		label := koLabel(obj)
		fmt.Printf("%d. %s\n", i+1, label)
		fmt.Printf("   total=%.4f  fts=%.4f  vector=%.4f  mention=%.4f  graph=%.4f\n",
			b.Total, b.FTS, b.Vector, b.MentionBoost, b.GraphRelevance)
		fmt.Println()
	}

	if diagnostics.BelowThresholdCount > 0 {
		// T-0574: we surface the count + top dropped score here. Listing each
		// dropped candidate's per-leg breakdown is a follow-up (would need
		// HybridSearchExplainFilteredWithDiagnostics to also return the
		// dropped ranking.Result slice; out of scope for this task).
		fmt.Printf("(%d additional candidate%s dropped below threshold %.2f; top below-threshold score: %.2f)\n",
			diagnostics.BelowThresholdCount,
			pluralS(diagnostics.BelowThresholdCount),
			diagnostics.Threshold,
			diagnostics.TopBelowThresholdScore)
	}
	if diagnostics.StalenessWarning != nil && diagnostics.StalenessWarning.Count > 0 {
		fmt.Printf("\nNote: %s\n", diagnostics.StalenessWarning.Reason)
	}
	return nil
}

// pluralS returns "s" for n != 1, otherwise "". Helper for grammatical
// pluralisation in find diagnostics output.
func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// findSemanticSource builds the semantic leg for one find run: the model
// registry on the command's store, the embedding resolver with find's
// --embedding-* flags (transport-only for a registered model), and, inside a
// REPL session, blending with the session's earlier query vectors of the
// same model.
func findSemanticSource(store storage.StorageDriver, state *repl.SessionState) (retrieval.SemanticSource, error) {
	models, err := registry.ForDriver(store)
	if err != nil {
		return retrieval.SemanticSource{}, err
	}
	r := newEmbeddingResolver()
	r.Registry = models
	r.Flags = findEmbedding
	r.HTTPClient = findEmbeddingHTTPClient
	src := retrieval.SemanticSource{Models: models, Resolver: embeddings.NewProviderResolver(r)}
	if state != nil {
		src.Blend = func(modelID string, vec []float32) []float32 {
			sessionVec := state.SessionContextVector(modelID)
			state.PushQueryVector(modelID, vec)
			if sessionVec == nil {
				return vec
			}
			return search.BlendVectors(vec, sessionVec, 0.85)
		}
	}
	return src, nil
}

// printSemanticNotice tells the operator, on stderr, when the semantic leg
// did not run and the results are full-text only. JSON output carries the
// same report under diagnostics.semantic.
func printSemanticNotice(cmd *cobra.Command, d service.SearchDiagnostics) {
	if d.Semantic == nil || d.Semantic.OK() {
		return
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "notice: %s\n", d.Semantic.Notice)
}

// printFindSuggestions runs a prefix FTS query and prints "Did you mean?" hints.
// Uses the first word of the original query as the prefix stem.
func printFindSuggestions(cmd *cobra.Command, ctx context.Context, svc interface {
	FindByText(context.Context, string, int) ([]*storage.KnowledgeObject, error)
}, query string) {
	// Build prefix query from first word.
	firstWord := query
	if idx := strings.IndexByte(query, ' '); idx > 0 {
		firstWord = query[:idx]
	}
	if firstWord == "" {
		return
	}
	prefixQuery := firstWord + " *"
	suggestions, err := svc.FindByText(ctx, prefixQuery, 3)
	if err != nil || len(suggestions) == 0 {
		return
	}

	// Collect unique labels to surface as hints (max 3).
	var hints []string
	seen := make(map[string]bool)
	for _, obj := range suggestions {
		label := koLabel(obj)
		if !seen[label] {
			hints = append(hints, label)
			seen[label] = true
		}
	}
	if len(hints) == 0 {
		return
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Did you mean:\n")
	for _, h := range hints {
		fmt.Fprintf(cmd.OutOrStdout(), "  • %s\n", h)
	}
}

// buildFindFilter constructs an ObjectFilter from find-command metadata facet flags.
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

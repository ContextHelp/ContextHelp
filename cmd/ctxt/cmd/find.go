package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/cli"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
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
  ctxt find "onboarding" --explain`,
	RunE: runFind,
}

func init() {
	rootCmd.AddCommand(findCmd)

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

	ctx := context.Background()

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

	// --explain only applies to hybrid mode; it prints per-signal score breakdowns.
	if explain && mode == "hybrid" {
		return runFindExplain(cmd, ctx, svc, query, limit, mode, searchCfg)
	}

	var results []*storage.KnowledgeObject

	switch mode {
	case "vector":
		factory := providers.NewFactory(cfg.Providers, nil)
		ep := factory.Embedding()
		results, err = svc.SemanticSearch(ctx, query, limit, ep)
	case "fts":
		results, err = svc.FindByText(ctx, query, limit)
	default: // "hybrid"
		factory := providers.NewFactory(cfg.Providers, nil)
		ep := factory.Embedding()
		results, err = svc.HybridSearch(ctx, query, limit, ep, searchCfg)
	}

	if err != nil {
		return fmt.Errorf("find (%s): %w", mode, err)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"objects": results,
			"total":   len(results),
			"query":   query,
			"mode":    mode,
		})
	}

	fmt.Printf("Search [%s]: %q (%d results)\n\n", mode, query, len(results))

	if len(results) == 0 {
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
	return nil
}

// runFindExplain executes a hybrid search and prints per-result score breakdowns.
func runFindExplain(cmd *cobra.Command, ctx context.Context, svc *service.Service, query string, limit int, mode string, searchCfg config.SearchConfig) error {
	factory := providers.NewFactory(cfg.Providers, nil)
	ep := factory.Embedding()

	explainResults, err := svc.HybridSearchExplain(ctx, query, limit, ep, searchCfg)
	if err != nil {
		return fmt.Errorf("find explain (%s): %w", mode, err)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"results": explainResults,
			"total":   len(explainResults),
			"query":   query,
			"mode":    mode,
		})
	}

	fmt.Printf("Search [%s] --explain: %q (%d results)\n\n", mode, query, len(explainResults))

	if len(explainResults) == 0 {
		return nil
	}

	for i, r := range explainResults {
		obj := r.Object
		b := r.Breakdown
		label := obj.ID
		if len(obj.Summaries) > 0 && obj.Summaries[0] != "" {
			label = obj.Summaries[0]
			if len(label) > 60 {
				label = label[:57] + "..."
			}
		}
		fmt.Printf("%d. %s (%s)\n", i+1, label, obj.ID)
		fmt.Printf("   total=%.4f  fts=%.4f  vector=%.4f  mention=%.4f  graph=%.4f\n",
			b.Total, b.FTS, b.Vector, b.MentionBoost, b.GraphRelevance)
		fmt.Println()
	}
	return nil
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

	// Collect unique summaries/IDs to surface as hints (max 3).
	var hints []string
	seen := make(map[string]bool)
	for _, obj := range suggestions {
		label := obj.ID
		if len(obj.Summaries) > 0 && obj.Summaries[0] != "" {
			label = obj.Summaries[0]
			if len(label) > 60 {
				label = label[:57] + "..."
			}
		}
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

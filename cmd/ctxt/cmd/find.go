package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var findCmd = &cobra.Command{
	Use:   "find <query>",
	Short: "Semantic search across knowledge",
	Long: `Perform semantic search across local and federated knowledge.

Uses embeddings and AI-powered reranking to find relevant content.

Examples:
  # Basic search
  ctxt find "authentication best practices"

  # Search with profile context
  ctxt find "signup flow" --profile growth

  # Limit results
  ctxt find "onboarding" --limit 10`,
	Args: cobra.MinimumNArgs(1),
	RunE: runFind,
}

func init() {
	rootCmd.AddCommand(findCmd)

	// Search flags
	findCmd.Flags().Int("limit", 10, "maximum results")
	findCmd.Flags().Bool("semantic", false, "use semantic vector search via embedding provider")

	// Bind flags to viper
	viper.BindPFlag("find.limit", findCmd.Flags().Lookup("limit"))
	viper.BindPFlag("find.semantic", findCmd.Flags().Lookup("semantic"))
}

func runFind(cmd *cobra.Command, args []string) error {
	query := strings.Join(args, " ")
	limit := viper.GetInt("find.limit")

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()

	if viper.GetBool("find.semantic") {
		factory := providers.NewFactory(cfg.Providers)
		ep := factory.Embedding()
		results, serr := svc.SemanticSearch(ctx, query, limit, ep)
		if serr != nil {
			return fmt.Errorf("semantic find: %w", serr)
		}
		if isJSONOutput() {
			return outputJSON(os.Stdout, map[string]any{
				"objects": results,
				"total":   len(results),
				"query":   query,
				"mode":    "semantic",
			})
		}
		fmt.Printf("Semantic search: %q (%d results)\n\n", query, len(results))
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

	objs, err := svc.FindByText(ctx, query, limit)
	if err != nil {
		return fmt.Errorf("find: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"objects": objs,
			"total":   len(objs),
			"query":   query,
		})
	}

	fmt.Printf("Search: %q (%d results)\n\n", query, len(objs))
	headers := []string{"ID", "Type", "Created"}
	var rows [][]string
	for _, obj := range objs {
		rows = append(rows, []string{
			obj.ID,
			obj.Type,
			obj.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	printTable(os.Stdout, headers, rows)
	return nil
}

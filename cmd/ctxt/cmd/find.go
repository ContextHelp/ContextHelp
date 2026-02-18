package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

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

	// Bind flags to viper
	viper.BindPFlag("find.limit", findCmd.Flags().Lookup("limit"))
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
	objects, err := svc.FindByText(ctx, query, limit)
	if err != nil {
		return fmt.Errorf("find: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"objects": objects,
			"total":   len(objects),
			"query":   query,
		})
	}

	fmt.Printf("Search: %q (%d results)\n\n", query, len(objects))
	headers := []string{"ID", "Type", "Created"}
	var rows [][]string
	for _, obj := range objects {
		rows = append(rows, []string{
			obj.ID,
			obj.Type,
			obj.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	printTable(os.Stdout, headers, rows)
	return nil
}

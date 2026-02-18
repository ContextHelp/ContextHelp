package cmd

import (
	"fmt"

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
	query := args[0]

	// TODO: Implement actual find logic
	fmt.Printf("Searching for: %s\n", query)
	fmt.Printf("Profile: %s\n", viper.GetString("profile.default"))
	fmt.Printf("Limit: %d\n\n", viper.GetInt("find.limit"))

	fmt.Println("Search Results:")
	fmt.Println()
	fmt.Println("Score | Title                                    | Type")
	fmt.Println("------|------------------------------------------|---------")
	fmt.Println("0.95  | Authentication patterns and best...      | url")
	fmt.Println("0.87  | Implementing secure login flows          | text")
	fmt.Println("0.82  | JWT vs Session-based auth comparison     | url")

	return nil
}
